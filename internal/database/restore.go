package database

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Operation subdirectory prefixes beneath the data directory. The millis
// suffix keeps them unique per attempt; recovery picks the newest one.
const (
	restoreStagedPrefix     = ".restore-"
	restoreQuarantinePrefix = ".quarantine-"
)

// StagedRestoreOptions configures one staged restore run.
type StagedRestoreOptions struct {
	// DataDir is the absolute data directory (the live database's home).
	DataDir string
	// BackupPath is the path to a validated backup artifact — typically a
	// file under data/backups/ from BackupForMigration.
	BackupPath string
	// AcknowledgeLoss accepts post-backup account-security changes and lets
	// activation proceed despite them (security reconciliation v1).
	AcknowledgeLoss bool
	// PostActivation runs after the staged database is activated and before
	// the post-activation check and restore-marker removal — the
	// media-extraction slot. The caller (cmd/restore) wires the media half
	// here so this package stays media-free. A non-nil error aborts the run:
	// the activated database remains in place and the marker is retained, so
	// ordinary startup fails closed and a re-run resumes (the hook must be
	// idempotent — ExtractMediaTar is). The resume paths invoke it too.
	PostActivation func() error
}

// StagedRestoreResult carries the durable outcomes of a successful restore.
type StagedRestoreResult struct {
	// QuarantineDir is the retained rollback set holding the previous live
	// database files. The operator removes it after verification.
	QuarantineDir string
	// BackupPath echoes the activated backup artifact.
	BackupPath string
	// AcknowledgedLoss is true when activation proceeded over newer
	// account-security events, with LiveNewestMS/BackupNewestMS for logs.
	AcknowledgedLoss bool
	// LiveNewestMS is the live database's newest security-event millis.
	LiveNewestMS int64
	// BackupNewestMS is the staged copy's newest security-event millis.
	BackupNewestMS int64
}

// operationMillis returns an operation-directory suffix floored at the
// marker's attempt millis, so a backward clock step can never hide the
// current attempt's directories from resume.
func operationMillis(attemptMS int64) int64 {
	if now := time.Now().UnixMilli(); now > attemptMS {
		return now
	}
	return attemptMS
}

// verifyBackupCopy copies the artifact into a private temp file in dataDir
// and validates the COPY, so the artifact is never SQL-opened; the same bytes
// are the staging source (internal/database/AGENTS.md).
func verifyBackupCopy(dataDir, backupPath string) (string, error) {
	src, err := os.Open(backupPath)
	if err != nil {
		return "", fmt.Errorf("database: open backup for validation: %w", err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp(dataDir, ".verify-backup-*.db")
	if err != nil {
		return "", fmt.Errorf("database: create validation copy: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup: a discard here never masks the error being
	// returned — the failed copy, close, or validation is the real failure.
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := io.Copy(tmp, src); err != nil {
		cleanup()
		return "", fmt.Errorf("database: copy backup for validation: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("database: close validation copy: %w", err)
	}
	if err := verifyBackupForRestore(tmpPath); err != nil {
		cleanup()
		return "", err
	}
	return tmpPath, nil
}

// StagedRestore runs the complete fail-closed staged restore. The caller
// (cmd/restore) owns the offline window: the application must be stopped or
// unready for the whole run, exactly like cmd/migrate (docs/patterns/go/sqlite.md).
func StagedRestore(opts StagedRestoreOptions) (StagedRestoreResult, error) {
	// Validate the backup before touching anything (step 2): a restore never
	// starts from an unverified artifact, and a rejected one leaves the live
	// database untouched. The artifact is validated from a private COPY in
	// the data directory — it may be WAL-mode (whose validation open needs
	// sidecar files) and may sit in a READ-ONLY unit directory (the drill
	// shape), so the original is only ever read, never opened by SQLite.
	// The same verified copy is the staging source, so the bytes verified
	// here are exactly the bytes staged (no re-read TOCTOU).
	verifiedPath, err := verifyBackupCopy(opts.DataDir, opts.BackupPath)
	if err != nil {
		return StagedRestoreResult{}, fmt.Errorf("database: validate backup %q: %w", opts.BackupPath, err)
	}
	// The verified copy is scratch: best-effort removal at return; a leftover
	// temp file is never promoted and never read as a unit.
	defer func() {
		_ = os.Remove(verifiedPath)
	}()

	present, err := restoreMarkerExists(opts.DataDir)
	if err != nil {
		return StagedRestoreResult{}, err
	}
	if present {
		// A previous run crashed or failed after creating the marker. Resume
		// or abort based on the on-disk state (crash recovery).
		return resumeRestore(opts)
	}

	// Normal path: acquire the exclusive marker first (step 1). From here
	// the application opener and readiness fail closed.
	if err := acquireRestoreMarker(opts.DataDir); err != nil {
		return StagedRestoreResult{}, err
	}

	return runStagedRestore(opts, verifiedPath)
}

// discardFailedStage cleans a failed attempt that provably left the live
// database untouched: the staged copy and the marker. The cleanup errors are
// deliberately dropped — the primary error is the operator's signal, and a
// leftover directory is diagnosable without poisoned state.
func discardFailedStage(dataDir, stagedDir string) {
	_ = os.RemoveAll(stagedDir)
	_ = removeRestoreMarker(dataDir)
}

// runStagedRestore executes the prepared workflow after the marker exists.
// stageSource is the already-verified copy (verifyBackupCopy) — staging
// copies from it, not from the original artifact, so verification and
// staging never diverge. Its error handling encodes the phase boundary:
// before the quarantine step the live database is untouched, so failures
// clean the staged copy and remove the marker; after it, failures leave the
// marker in place.
func runStagedRestore(opts StagedRestoreOptions, stageSource string) (StagedRestoreResult, error) {
	dataDir := opts.DataDir

	// Phase A (steps 3–4): stage + prepare. Live database untouched.
	attemptMS, err := restoreMarkerAttempt(dataDir)
	if err != nil {
		return StagedRestoreResult{}, err
	}
	stagedDir, err := stageAndPrepare(dataDir, stageSource, attemptMS)
	if err != nil {
		discardFailedStage(dataDir, stagedDir)
		return StagedRestoreResult{}, err
	}

	// Phase B (step 5): security reconciliation before any mutation.
	rec, err := reconcileSecurity(
		filepath.Join(dataDir, FileName), stagedDir, opts.AcknowledgeLoss)
	if err != nil {
		discardFailedStage(dataDir, stagedDir)
		return StagedRestoreResult{}, err
	}

	// Phase C (step 6): re-verify, checkpoint, leave self-contained.
	if err := finalizeStaged(stagedDir); err != nil {
		discardFailedStage(dataDir, stagedDir)
		return StagedRestoreResult{}, err
	}

	// Phase D (step 7): quarantine + atomic activation. Failures from here
	// leave the marker so ordinary startup fails closed (step 8).
	quarantineDir, err := activate(dataDir, stagedDir, attemptMS)
	if err != nil {
		if quarantineDir != "" {
			return StagedRestoreResult{}, fmt.Errorf(
				"database: activation failed and the rollback did not complete (restore marker retained at %q — re-run the restore command to recover): %w",
				restoreMarkerPath(dataDir), err)
		}
		// activate rolled the quarantine back: live is intact again, so the
		// marker can go and the app can start.
		discardFailedStage(dataDir, stagedDir)
		return StagedRestoreResult{}, err
	}

	// Phase D½: the media half runs after the database is
	// activated and before the post-activation check/marker removal.
	if err := postActivation(opts); err != nil {
		return StagedRestoreResult{}, err
	}

	// Phases E–F (step 8): marker-exempt strict/schema check on the
	// activated file, then marker removal.
	return completeActivation(opts.BackupPath, dataDir, quarantineDir, rec)
}

// postActivation invokes the caller's hook when configured. A failure
// retains the activated database and the marker — no rollback — because the
// hook must be idempotent and a re-run resumes where it stopped —
// extraction failure after DB activation keeps the marker; the operator
// re-runs.
func postActivation(opts StagedRestoreOptions) error {
	if opts.PostActivation == nil {
		return nil
	}
	if err := opts.PostActivation(); err != nil {
		return fmt.Errorf("database: post-activation step failed: %w (the activated database remains in place and the restore marker is retained — re-run the restore command to resume)", err)
	}
	return nil
}

// stageAndPrepare copies the backup into a new operation subdirectory, opens
// it through the maintenance opener (upgrading a supported older history),
// verifies integrity and foreign keys, deletes every session and pending
// token in one transaction, and verifies the counts are zero. attemptMS
// floors the directory suffix (internal/database/AGENTS.md).
func stageAndPrepare(dataDir, backupPath string, attemptMS int64) (string, error) {
	stagedDir := filepath.Join(dataDir, fmt.Sprintf("%s%d", restoreStagedPrefix, operationMillis(attemptMS)))
	if err := os.Mkdir(stagedDir, 0o700); err != nil {
		return "", fmt.Errorf("database: create staged directory: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(stagedDir) } // best effort; the returned error is the failure

	// The fixed-name 0600 main file inside the operation subdirectory —
	// never restored directly over the live files (step 3).
	if err := copyFile(backupPath, filepath.Join(stagedDir, FileName)); err != nil {
		cleanup()
		return "", err
	}

	// Maintenance open applies pending migrations (older-prefix backups are
	// upgraded), verifies exact history + the Go-declared schema (step 4). The
	// marker-exempt opener is correct here: the restore owns the live marker.
	db, err := openMaintenance(Config{DataDir: stagedDir})
	if err != nil {
		cleanup()
		return "", fmt.Errorf("database: prepare staged copy: %w", err)
	}
	fail := func(tx *sql.Tx, err error) (string, error) {
		// Rollback is best-effort: the transaction is being abandoned and the
		// failure below is the error the operator needs.
		if tx != nil {
			_ = tx.Rollback()
		}
		_ = db.Close() // best effort; the returned error is the failure
		cleanup()
		return "", err
	}

	if err := fullIntegrityOK(db); err != nil {
		return fail(nil, fmt.Errorf("database: staged integrity: %w", err))
	}
	if err := VerifyForeignKeys(db); err != nil {
		return fail(nil, fmt.Errorf("database: staged foreign keys: %w", err))
	}

	// Every session and every pending token die in ONE transaction, then
	// the counts are verified independently (step 4) — no restored session
	// can authenticate and no consumed reset/verification link comes back
	// to life (the sessions-purge rationale applied to the token tables —
	// one uniform invariant across every token table).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail(nil, fmt.Errorf("database: begin session deletion: %w", err))
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions"); err != nil {
		return fail(tx, fmt.Errorf("database: delete sessions: %w", err))
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM password_reset_tokens"); err != nil {
		return fail(tx, fmt.Errorf("database: delete reset tokens: %w", err))
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM email_verification_tokens"); err != nil {
		return fail(tx, fmt.Errorf("database: delete verification tokens: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fail(tx, fmt.Errorf("database: commit session deletion: %w", err))
	}
	if err := sessionsEmpty(db); err != nil {
		return fail(nil, fmt.Errorf("database: staged sessions: %w", err))
	}
	if err := resetTokensEmpty(db); err != nil {
		return fail(nil, fmt.Errorf("database: staged reset tokens: %w", err))
	}
	if err := verificationTokensEmpty(db); err != nil {
		return fail(nil, fmt.Errorf("database: staged verification tokens: %w", err))
	}

	if err := db.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("database: close staged copy: %w", err)
	}
	return stagedDir, nil
}

// finalizeStaged re-verifies the prepared staged copy (strict history, full
// integrity, foreign keys, zero sessions), checkpoints the WAL with
// TRUNCATE, closes the pool, removes the sidecars, and requires the
// operation directory to hold exactly the self-contained main file.
func finalizeStaged(stagedDir string) error {
	db, err := Open(Config{DataDir: stagedDir})
	if err != nil {
		return fmt.Errorf("database: reopen staged copy: %w", err)
	}
	fail := func(err error) error {
		_ = db.Close() // best effort; the returned error is the failure
		return err
	}

	if err := migrationHistoryOK(db); err != nil {
		return fail(fmt.Errorf("database: staged history: %w", err))
	}
	if err := VerifySchema(db); err != nil {
		return fail(fmt.Errorf("database: staged schema: %w", err))
	}
	if err := fullIntegrityOK(db); err != nil {
		return fail(fmt.Errorf("database: staged integrity: %w", err))
	}
	if err := VerifyForeignKeys(db); err != nil {
		return fail(fmt.Errorf("database: staged foreign keys: %w", err))
	}
	if err := sessionsEmpty(db); err != nil {
		return fail(fmt.Errorf("database: staged sessions: %w", err))
	}

	// wal_checkpoint(TRUNCATE) returns (busy, log, checkpointed); the last
	// column must be 0 (success). After it, the main file holds every page.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var busy, logN, checkpointed int
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logN, &checkpointed); err != nil {
		return fail(fmt.Errorf("database: staged checkpoint: %w", err))
	}
	if checkpointed != 0 {
		return fail(fmt.Errorf("database: staged checkpoint failed: (%d, %d, %d)", busy, logN, checkpointed))
	}

	if err := db.Close(); err != nil {
		return fmt.Errorf("database: close staged copy: %w", err)
	}

	// Self-contained: no sidecars, exactly the main file. Removal is
	// best-effort; the self-containment check below is the authority.
	for _, name := range []string{FileName + "-wal", FileName + "-shm"} {
		_ = os.Remove(filepath.Join(stagedDir, name))
	}
	entries, err := os.ReadDir(stagedDir)
	if err != nil {
		return fmt.Errorf("database: read staged directory: %w", err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		return fmt.Errorf("database: staged directory is not self-contained (%d entries)", len(entries))
	}
	return nil
}
