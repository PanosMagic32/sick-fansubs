package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RestoreOfflineBackup replaces the live database files with a validated
// backup during the migration command's offline window
// (docs/patterns/go/sqlite.md; the safety properties are in internal/database/AGENTS.md).
func RestoreOfflineBackup(dataDir, backupPath string) (retErr error) {
	// Validate before touching anything: a restore never starts from an
	// unverified artifact.
	if err := verifyBackupForRestore(backupPath); err != nil {
		return err
	}

	live := filepath.Join(dataDir, FileName)
	quarantine := func(name string) string {
		return filepath.Join(dataDir, fmt.Sprintf("%s.broken-%d", name, time.Now().UnixMilli()))
	}

	// 1. Quarantine the (possibly half-migrated) live files. Deletion is
	//    never the first step — the rollback set stays recoverable.
	type quarantined struct{ original, movedTo string }
	var moved []quarantined
	for _, name := range []string{FileName, FileName + "-wal", FileName + "-shm"} {
		p := filepath.Join(dataDir, name)
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("database: stat live %s: %w", name, err)
		}
		q := quarantine(name)
		if err := os.Rename(p, q); err != nil {
			return fmt.Errorf("database: quarantine live %s: %w", name, err)
		}
		moved = append(moved, quarantined{original: p, movedTo: q})
	}

	// Any failure from here on must put the quarantined files back. The
	// rollback is best-effort at the per-file level: a rename failure leaves
	// the `.broken-<millis>` remnant behind; the next open refuses to create
	// an empty database while it remains (`ensureNoInterruptedRollback`).
	defer func() {
		if retErr != nil {
			for _, q := range moved {
				_ = os.Rename(q.movedTo, q.original)
			}
		}
	}()

	// 2. Write the replacement under a temp name and sync it.
	tmp := live + ".tmp"
	if err := copyFile(backupPath, tmp); err != nil {
		return err
	}

	// 3. Activate atomically (same filesystem) and sync the directory.
	if err := os.Rename(tmp, live); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("database: activate restored database: %w", err)
	}
	if err := syncDir(dataDir); err != nil {
		return fmt.Errorf("database: sync data directory: %w", err)
	}

	// 4. Success — the activated file was validated pre-copy. Drop the
	//    quarantined broken set (best-effort: a leftover quarantine is
	//    diagnosable, and the retained backup remains untouched).
	for _, q := range moved {
		_ = os.Remove(q.movedTo)
	}
	moved = nil
	return nil
}

// ensureNoInterruptedRollback refuses to open a data directory whose main
// database file is missing while RestoreOfflineBackup remnants remain — the
// next connection would otherwise create an empty database in place of the
// file the remnant retains. Called by openDB.
func ensureNoInterruptedRollback(dataDir string) error {
	if _, err := os.Stat(filepath.Join(dataDir, FileName)); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("database: stat %s: %w", FileName, err)
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("database: read data directory: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, FileName+".broken-") || name == FileName+".tmp" {
			return fmt.Errorf("database: the database file is missing while the interrupted-rollback artifact %q remains — refusing to create an empty database; recover the live file from that artifact or the retained backup by hand", name)
		}
	}
	return nil
}

// verifyBackupForRestore runs the restore-activation validation: the same
// guarantees as verifyBackupArtifact plus full integrity_check with exactly
// one "ok" row (docs/patterns/go/sqlite.md: quick-check-only files never
// qualify for activation). Read-write open on a writable copy — see
// verifyBackupArtifactWith for why.
func verifyBackupForRestore(path string) error {
	if err := verifyBackupArtifact(path); err != nil {
		return err
	}

	db, err := openDSN(buildDSNForFile(path))
	if err != nil {
		return fmt.Errorf("database: reopen backup for restore: %w", err)
	}
	defer db.Close()

	if err := fullIntegrityOK(db); err != nil {
		return fmt.Errorf("database: backup integrity_check: %w", err)
	}
	return nil
}

// fullIntegrityOK requires PRAGMA integrity_check to return exactly one
// "ok" row (docs/patterns/go/sqlite.md: restore activation requires the full
// check — quick_check is a screening check only). It runs against an open
// *sql.DB, shared by backup validation and the staged-restore
// preparation/finalization (restore.go).
func fullIntegrityOK(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("integrity_check: %w", err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return fmt.Errorf("integrity_check scan: %w", err)
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("integrity_check iterate: %w", err)
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("integrity_check: got %v, want exactly [ok]", results)
	}
	return nil
}

// copyFile copies src to dst with 0600 permissions and fsyncs the result.
// dst must not exist.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("database: open backup for restore: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("database: create restored database: %w", err)
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("database: copy backup into place: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("database: sync restored database: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("database: close restored database: %w", err)
	}
	return nil
}

// buildDSNForFile builds the pragma URI docs/patterns/go/sqlite.md defines for
// an arbitrary database file path. buildDSN delegates to it with the fixed live
// path.
func buildDSNForFile(absPath string) string {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_pragma", "trusted_schema(OFF)")
	q.Set("_txlock", "immediate")
	q.Set("_dqs", "0")

	u := &url.URL{
		Scheme:   "file",
		Path:     absPath,
		RawQuery: q.Encode(),
	}
	return u.String()
}

// randomHex returns n random bytes as lowercase hex (unpredictable temp
// filenames — docs/patterns/go/sqlite.md).
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// rand.Read fails only when the system entropy source is broken;
		// a timestamp fallback keeps the name unique enough to continue
		// while the copy-phase failures would be reported loudly anyway.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// syncFile fsyncs the named file's contents so a crash after publication
// cannot leave an empty or truncated artifact (docs/patterns/go/sqlite.md).
func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// syncDir fsyncs a directory so a rename inside it survives a crash.
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
