package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// restoreMarkerName is the exclusive owner-only marker (0600) the staged
// restore acquires first; its existence gates every live opener and its
// attempt millis scopes crash recovery (docs/patterns/go/sqlite.md rule 12).
const restoreMarkerName = "restore.marker"

// restoreActivationName is the per-attempt activation record. Written just
// before the promotion rename and removed with the marker on success, it is
// resume's only evidence that the current attempt activated the staged copy.
const restoreActivationName = "restore.activated"

func restoreMarkerPath(dataDir string) string {
	return filepath.Join(dataDir, restoreMarkerName)
}

// restoreMarkerAttempt reports the attempt millis recorded in the current
// marker; resume scopes operation-directory lookups to it, and an old-layout
// marker (no field) fails closed with manual recovery.
func restoreMarkerAttempt(dataDir string) (int64, error) {
	raw, err := os.ReadFile(restoreMarkerPath(dataDir))
	if err != nil {
		return 0, fmt.Errorf("database: read restore marker: %w", err)
	}
	const field = "attempt="
	content := string(raw)
	_, after, ok := strings.Cut(content, field)
	if !ok {
		return 0, fmt.Errorf("database: restore marker carries no attempt identifier — manual recovery required")
	}
	attemptMS, err := strconv.ParseInt(strings.TrimSpace(after), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("database: restore marker attempt is malformed — manual recovery required")
	}
	return attemptMS, nil
}

func activationRecordPath(dataDir string) string {
	return filepath.Join(dataDir, restoreActivationName)
}

// writeActivationRecord records the in-flight attempt's quarantine directory
// just before the promotion rename, synced so a crash cannot lose the
// evidence that activation started.
func writeActivationRecord(dataDir, quarantineDir string) error {
	path := activationRecordPath(dataDir)
	if err := os.WriteFile(path, []byte(filepath.Base(quarantineDir)+"\n"), 0o600); err != nil {
		return fmt.Errorf("database: write activation record: %w", err)
	}
	if err := syncFile(path); err != nil {
		// Best-effort removal of the partial record; the sync failure is
		// the error being returned.
		_ = os.Remove(path)
		return fmt.Errorf("database: sync activation record: %w", err)
	}
	if err := syncDir(dataDir); err != nil {
		// Same best-effort removal; the directory sync failure is returned.
		_ = os.Remove(path)
		return fmt.Errorf("database: sync activation record directory: %w", err)
	}
	return nil
}

// readActivationRecord returns the current attempt's quarantine directory, or
// "" when no current record exists: the name must be a current-attempt
// quarantine (at or after minMS) that still exists (internal/database/AGENTS.md).
func readActivationRecord(dataDir string, minMS int64) (string, error) {
	raw, err := os.ReadFile(activationRecordPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("database: read activation record: %w", err)
	}
	name := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(name, restoreQuarantinePrefix) || strings.ContainsAny(name, `/\`) {
		return "", nil
	}
	dirMS, err := strconv.ParseInt(strings.TrimPrefix(name, restoreQuarantinePrefix), 10, 64)
	if err != nil || dirMS < minMS {
		return "", nil
	}
	dir := filepath.Join(dataDir, name)
	if _, err := os.Stat(dir); err != nil {
		return "", nil
	}
	return dir, nil
}

// removeActivationRecord clears the record; a missing file is not an error.
// Rollback paths treat a removal failure as fail-closed (the record must never
// outlive a rollback and be read as evidence of activation).
func removeActivationRecord(dataDir string) error {
	if err := os.Remove(activationRecordPath(dataDir)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("database: remove activation record: %w", err)
	}
	return nil
}

// restoreMarkerExists reports whether the restore marker is present.
func restoreMarkerExists(dataDir string) (bool, error) {
	_, err := os.Stat(restoreMarkerPath(dataDir))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("database: stat restore marker: %w", err)
}

// ensureNoRestoreMarker fails closed when the marker exists — the gate the
// application opener and readiness check both use (docs/patterns/go/sqlite.md).
func ensureNoRestoreMarker(dataDir string) error {
	present, err := restoreMarkerExists(dataDir)
	if err != nil {
		return err
	}
	if present {
		return fmt.Errorf("database: a restore marker is present in the data directory — a staged restore is in progress or a previous one crashed; the application refuses to open until the restore command resolves it")
	}
	return nil
}

// acquireRestoreMarker creates the exclusive owner-only marker (0600). It
// refuses when one already exists — a concurrent or crashed restore.
func acquireRestoreMarker(dataDir string) error {
	f, err := os.OpenFile(restoreMarkerPath(dataDir), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("database: restore marker already exists in %q", dataDir)
		}
		return fmt.Errorf("database: create restore marker: %w", err)
	}
	// The start stamp is diagnostics; the attempt millis is load-bearing —
	// resume scopes operation directories to this attempt with it. Existence
	// remains the gate, and the file is synced so a power loss cannot leave an
	// empty marker that reads as an old-layout marker.
	now := time.Now()
	discardPartialMarker := func() {
		// O_EXCL guarantees this call created the marker; a failure before the
		// marker is complete must leave no partial marker for resume to trip on
		// (a marker without attempt= needs manual recovery).
		_ = os.Remove(restoreMarkerPath(dataDir))
	}
	if _, err := fmt.Fprintf(f, "restore started %s attempt=%d\n", now.UTC().Format(time.RFC3339), now.UnixMilli()); err != nil {
		f.Close() // best effort; the write failure is the returned error
		discardPartialMarker()
		return fmt.Errorf("database: write restore marker: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		discardPartialMarker()
		return fmt.Errorf("database: sync restore marker: %w", err)
	}
	if err := f.Close(); err != nil {
		discardPartialMarker()
		return fmt.Errorf("database: close restore marker: %w", err)
	}
	if err := syncDir(dataDir); err != nil {
		discardPartialMarker()
		return fmt.Errorf("database: sync restore marker: %w", err)
	}
	return nil
}

// removeRestoreMarker removes the marker and syncs the deletion so the
// removal survives a crash (docs/patterns/go/sqlite.md).
func removeRestoreMarker(dataDir string) error {
	if err := os.Remove(restoreMarkerPath(dataDir)); err != nil {
		return fmt.Errorf("database: remove restore marker: %w", err)
	}
	if err := syncDir(dataDir); err != nil {
		return fmt.Errorf("database: sync marker removal: %w", err)
	}
	return nil
}
