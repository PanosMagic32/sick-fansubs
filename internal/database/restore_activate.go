package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// activate quarantines the previous live files and atomically promotes the
// staged main file, writing the activation record between the two so resume
// can trust this attempt's activation (internal/database/AGENTS.md).
func activate(dataDir, stagedDir string, attemptMS int64) (string, error) {
	quarantineDir := filepath.Join(dataDir, fmt.Sprintf("%s%d", restoreQuarantinePrefix, operationMillis(attemptMS)))
	if err := os.Mkdir(quarantineDir, 0o700); err != nil {
		return "", fmt.Errorf("database: create quarantine directory: %w", err)
	}

	quarantine := func(name string) error {
		src := filepath.Join(dataDir, name)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		return os.Rename(src, filepath.Join(quarantineDir, name))
	}

	for _, name := range []string{FileName, FileName + "-wal", FileName + "-shm"} {
		if err := quarantine(name); err != nil {
			if rbErr := moveBackQuarantine(dataDir, quarantineDir); rbErr != nil {
				return quarantineDir, fmt.Errorf("database: quarantine %s: %v (and rollback failed: %w)", name, err, rbErr)
			}
			return "", fmt.Errorf("database: quarantine %s: %w", name, err)
		}
	}

	if err := writeActivationRecord(dataDir, quarantineDir); err != nil {
		if rbErr := moveBackQuarantine(dataDir, quarantineDir); rbErr != nil {
			return quarantineDir, fmt.Errorf("%v (and rollback failed: %w)", err, rbErr)
		}
		return "", err
	}

	if err := promoteStaged(stagedDir, dataDir); err != nil {
		// The record must not outlive a rollback (a later resume would read it
		// as proof of activation against the rolled-back old database), so it
		// is cleared BEFORE the rollback starts.
		if rerr := removeActivationRecord(dataDir); rerr != nil {
			return quarantineDir, fmt.Errorf("database: activate restored database: %v; AND clearing the activation record failed: %w (marker retained — manual recovery required)", err, rerr)
		}
		if rbErr := moveBackQuarantine(dataDir, quarantineDir); rbErr != nil {
			return quarantineDir, fmt.Errorf("database: activate restored database: %v (and rollback failed: %w)", err, rbErr)
		}
		return "", fmt.Errorf("database: activate restored database: %w", err)
	}

	return quarantineDir, nil
}

// promoteStaged renames the staged main file into the live name on the same
// filesystem and syncs the data directory (step 7). The staged directory is
// then consumed: any residual sidecars are dropped (a read-only
// reconciliation open may have recreated an empty -shm — it belongs to the
// already-promoted main file) and the directory is removed.
func promoteStaged(stagedDir, dataDir string) error {
	if err := os.Rename(filepath.Join(stagedDir, FileName), filepath.Join(dataDir, FileName)); err != nil {
		return err
	}
	if err := syncDir(dataDir); err != nil {
		return fmt.Errorf("sync activation: %w", err)
	}
	// The staged directory is consumed: best-effort sweep of any residue
	// (the promoted main file is already live; a leftover directory is
	// diagnosable).
	entries, err := os.ReadDir(stagedDir)
	if err == nil {
		for _, e := range entries {
			_ = os.Remove(filepath.Join(stagedDir, e.Name()))
		}
	}
	_ = os.Remove(stagedDir)
	return nil
}

// moveBackQuarantine renames every file in the quarantine directory back to
// the data directory and removes the quarantine directory. Best-effort at
// the per-file level: a partial rollback leaves the quarantine directory
// behind, which the caller reports as fail-closed.
func moveBackQuarantine(dataDir, quarantineDir string) error {
	entries, err := os.ReadDir(quarantineDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(quarantineDir, e.Name())
		dst := filepath.Join(dataDir, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	return os.Remove(quarantineDir)
}

// undoActivation removes the activated file (and any final-name sidecars
// created by the post-activation check) and restores the quarantined
// rollback set.
func undoActivation(dataDir, quarantineDir string) error {
	// Best-effort removal of the activated set; the rollback outcome is
	// moveBackQuarantine's error.
	for _, name := range []string{FileName, FileName + "-wal", FileName + "-shm"} {
		_ = os.Remove(filepath.Join(dataDir, name))
	}
	if err := moveBackQuarantine(dataDir, quarantineDir); err != nil {
		return err
	}
	return syncDir(dataDir)
}

// verifyActivated runs the marker-exempt strict/schema compatibility check
// against the activated file: the same open path the application opener
// uses, intentionally excluding only the marker gate the restore command
// currently owns.
func verifyActivated(dataDir string) error {
	db, err := openStrict(dataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := VerifySchema(db); err != nil {
		return fmt.Errorf("database: activated schema: %w", err)
	}
	return nil
}

// completeActivation is the shared tail of the workflow (step 8): the
// marker-exempt check, rollback on failure (marker retained), then marker
// removal and the result. Step 9 (start/smoke) is the operator's.
func completeActivation(backupPath, dataDir, quarantineDir string, rec reconciliation) (StagedRestoreResult, error) {
	if err := verifyActivated(dataDir); err != nil {
		// Clear the record BEFORE the rollback: an interrupted rollback must
		// resolve through the no-record fail-closed branch, never as a
		// completed activation against the old database.
		if rerr := removeActivationRecord(dataDir); rerr != nil {
			return StagedRestoreResult{}, fmt.Errorf(
				"database: post-activation check failed: %v; AND clearing the activation record failed: %w (restore marker retained at %q — manual recovery required)",
				err, rerr, restoreMarkerPath(dataDir))
		}
		if rbErr := undoActivation(dataDir, quarantineDir); rbErr != nil {
			return StagedRestoreResult{}, fmt.Errorf(
				"database: post-activation check failed: %v; AND rollback to the quarantine failed: %w (restore marker retained at %q)",
				err, rbErr, restoreMarkerPath(dataDir))
		}
		return StagedRestoreResult{}, fmt.Errorf(
			"database: post-activation check failed: %v; the previous database files were rolled back from %q (restore marker retained — re-run the restore command to clear it)",
			err, quarantineDir)
	}

	// The activated file is verified — remove the marker and sync its
	// deletion. The quarantined rollback set is retained.
	if err := removeRestoreMarker(dataDir); err != nil {
		return StagedRestoreResult{}, err
	}
	_ = removeActivationRecord(dataDir) // best effort: a stale record is attempt-scoped

	return StagedRestoreResult{
		QuarantineDir:    quarantineDir,
		BackupPath:       backupPath,
		AcknowledgedLoss: rec.acknowledgedLoss,
		LiveNewestMS:     rec.liveNewestMS,
		BackupNewestMS:   rec.backupNewestMS,
	}, nil
}

// resumeRestore recovers from a previous run that created the marker and then
// crashed or failed; the activation record scopes the state machine to the
// current attempt, and anything ambiguous fails closed (internal/database/AGENTS.md).
func resumeRestore(opts StagedRestoreOptions) (StagedRestoreResult, error) {
	dataDir := opts.DataDir
	live := filepath.Join(dataDir, FileName)
	attemptMS, err := restoreMarkerAttempt(dataDir)
	if err != nil {
		return StagedRestoreResult{}, err
	}

	_, statErr := os.Stat(live)
	switch {
	case statErr == nil:
		// Live main exists.
		quarantineDir, err := readActivationRecord(dataDir, attemptMS)
		if err != nil {
			return StagedRestoreResult{}, err
		}
		if quarantineDir != "" {
			// Activation happened; the crash came before the run finished. The
			// hook is idempotent, so re-run it, re-verify, and finish. The
			// security reconciliation of the original run already happened, so
			// the result's security fields stay zero here (informational only
			// on this path).
			if err := postActivation(opts); err != nil {
				return StagedRestoreResult{}, err
			}
			return completeActivation(opts.BackupPath, dataDir, quarantineDir, reconciliation{})
		}

		// No current record: this attempt never promoted the staged copy. A
		// current-attempt quarantine may still hold part of the live set; one
		// holding the main file must never overwrite live automatically.
		partial, err := findNewestDir(dataDir, restoreQuarantinePrefix, attemptMS)
		if err != nil {
			return StagedRestoreResult{}, err
		}
		if partial != "" {
			if _, err := os.Stat(filepath.Join(partial, FileName)); err == nil {
				return StagedRestoreResult{}, fmt.Errorf(
					"database: a restore marker and a quarantine holding the live database exist without an activation record — manual recovery required (quarantine at %q)", partial)
			}
			if err := moveBackQuarantine(dataDir, partial); err != nil {
				return StagedRestoreResult{}, fmt.Errorf(
					"database: resume: restoring a partial quarantine set failed: %w (marker retained — manual recovery required)", err)
			}
		}

		// The previous run never reached activation. The live database is
		// untouched: discard leftovers and the marker.
		stagedDir, err := findNewestDir(dataDir, restoreStagedPrefix, attemptMS)
		if err != nil {
			return StagedRestoreResult{}, err
		}
		if stagedDir != "" {
			_ = os.RemoveAll(stagedDir) // best effort; the live database is untouched
		}
		if err := removeRestoreMarker(dataDir); err != nil {
			return StagedRestoreResult{}, err
		}
		_ = removeActivationRecord(dataDir) // defensive: no current record expected
		return StagedRestoreResult{}, fmt.Errorf(
			"database: a previous restore attempt left a marker but never reached activation; the live database is untouched — staged leftovers discarded and the marker removed; re-run the restore command to retry")

	case os.IsNotExist(statErr):
		// Live main missing — activation must be finished or the state is
		// unrecoverable automatically.
		quarantineDir, err := findNewestDir(dataDir, restoreQuarantinePrefix, attemptMS)
		if err != nil {
			return StagedRestoreResult{}, err
		}
		stagedDir, err := findNewestDir(dataDir, restoreStagedPrefix, attemptMS)
		if err != nil {
			return StagedRestoreResult{}, err
		}
		if quarantineDir == "" || stagedDir == "" {
			return StagedRestoreResult{}, fmt.Errorf(
				"database: the live database file is missing and no complete in-flight restore set exists (quarantine %q, staged %q) — manual recovery required",
				quarantineDir, stagedDir)
		}
		if _, err := os.Stat(filepath.Join(stagedDir, FileName)); err != nil {
			return StagedRestoreResult{}, fmt.Errorf(
				"database: the live database file is missing and the staged copy is incomplete — manual recovery required: %w", err)
		}
		return resumeActivation(opts, dataDir, quarantineDir, stagedDir)

	default:
		return StagedRestoreResult{}, fmt.Errorf("database: stat live database: %w", statErr)
	}
}

// resumeActivation finishes an activation that crashed between quarantine
// and promotion: re-verify the prepared staged copy, complete the
// quarantining of the sidecars (so the reconciliation reads the whole live
// set — main + WAL together), re-run the security reconciliation against
// that complete quarantined set, and promote.
func resumeActivation(opts StagedRestoreOptions, dataDir, quarantineDir, stagedDir string) (StagedRestoreResult, error) {
	if err := finalizeStaged(stagedDir); err != nil {
		return StagedRestoreResult{}, fmt.Errorf(
			"database: resume: staged copy failed re-verification: %w (marker retained — roll the quarantine back manually if needed)", err)
	}

	// Complete the quarantine of sidecars that were not yet moved when the
	// crash happened (the main file is already inside). Order matters: the
	// reconciliation must see the WAL that belongs to the quarantined main —
	// comparing against a WAL-stripped live file could hide uncheckpointed
	// security events.
	for _, name := range []string{FileName + "-wal", FileName + "-shm"} {
		src := filepath.Join(dataDir, name)
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, filepath.Join(quarantineDir, name)); err != nil {
				return StagedRestoreResult{}, fmt.Errorf(
					"database: resume: complete quarantine of %s: %w (marker retained)", name, err)
			}
		}
	}

	rec, err := reconcileSecurity(
		filepath.Join(quarantineDir, FileName), stagedDir, opts.AcknowledgeLoss)
	if err != nil {
		return StagedRestoreResult{}, fmt.Errorf(
			"database: resume: %w (marker retained; the quarantined files remain at %q)", err, quarantineDir)
	}

	if err := writeActivationRecord(dataDir, quarantineDir); err != nil {
		return StagedRestoreResult{}, fmt.Errorf("database: resume: %w (marker retained)", err)
	}

	if err := promoteStaged(stagedDir, dataDir); err != nil {
		// Clear the record BEFORE the rollback (an interrupted rollback must
		// never be read as a completed activation on the next run).
		if rerr := removeActivationRecord(dataDir); rerr != nil {
			return StagedRestoreResult{}, fmt.Errorf(
				"database: resume: activation failed: %v; AND clearing the activation record failed: %w (marker retained — manual recovery required)", err, rerr)
		}
		if rbErr := moveBackQuarantine(dataDir, quarantineDir); rbErr != nil {
			return StagedRestoreResult{}, fmt.Errorf(
				"database: resume: activation failed: %v (and rollback failed: %w — marker retained)", err, rbErr)
		}
		return StagedRestoreResult{}, fmt.Errorf("database: resume: activation failed: %w", err)
	}

	// The resumed activation reaches the same post-activation step as the
	// normal path (media extraction before marker removal).
	if err := postActivation(opts); err != nil {
		return StagedRestoreResult{}, err
	}

	return completeActivation(opts.BackupPath, dataDir, quarantineDir, rec)
}

// findNewestDir returns the newest directory beneath parent whose name has
// the given prefix and a numeric millis suffix at or after minMS, or "" when
// none exists — the attempt scoping for crash recovery.
func findNewestDir(parent, prefix string, minMS int64) (string, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", fmt.Errorf("database: read %q: %w", parent, err)
	}
	var newestPath string
	var newestMS int64
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		dirMS, err := strconv.ParseInt(strings.TrimPrefix(e.Name(), prefix), 10, 64)
		if err != nil || dirMS < minMS {
			continue
		}
		if newestPath == "" || dirMS > newestMS {
			newestPath = filepath.Join(parent, e.Name())
			newestMS = dirMS
		}
	}
	return newestPath, nil
}
