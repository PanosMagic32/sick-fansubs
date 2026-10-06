package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sick-fansubs/internal/audit"
)

// sessionsEmpty requires exactly zero session rows.
func sessionsEmpty(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&n); err != nil {
		return fmt.Errorf("count sessions: %w", err)
	}
	if n != 0 {
		return fmt.Errorf("%d session row(s) remain", n)
	}
	return nil
}

// resetTokensEmpty requires exactly zero password-reset-token rows —
// the restore purge's independent verification step.
func resetTokensEmpty(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM password_reset_tokens").Scan(&n); err != nil {
		return fmt.Errorf("count reset tokens: %w", err)
	}
	if n != 0 {
		return fmt.Errorf("%d reset token row(s) remain", n)
	}
	return nil
}

// verificationTokensEmpty requires exactly zero email-verification-token
// rows — the same purge-invariant verification.
func verificationTokensEmpty(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM email_verification_tokens").Scan(&n); err != nil {
		return fmt.Errorf("count verification tokens: %w", err)
	}
	if n != 0 {
		return fmt.Errorf("%d verification token row(s) remain", n)
	}
	return nil
}

// reconciliation is the outcome of the security comparison: whether the
// restore proceeded over newer events and the two newest timestamps.
type reconciliation struct {
	acknowledgedLoss bool
	liveNewestMS     int64
	backupNewestMS   int64
}

// reconcileSecurity refuses activation when the live database has newer
// SUCCESSFUL account-security events than the staged copy, unless acknowledge
// is set (the v1 contract lives in internal/database/AGENTS.md).
func reconcileSecurity(liveDBPath, stagedDir string, acknowledge bool) (reconciliation, error) {
	// A missing live database (fresh-volume disaster recovery) has no
	// security state to reconcile — the backup is the only state, so the
	// live side counts as zero events. Any OTHER stat/read failure still
	// refuses: a hot WAL or corrupt live file must never be reconciled over.
	liveMS := int64(0)
	if _, err := os.Stat(liveDBPath); err == nil {
		liveMS, err = newestSecurityEvent(liveDBPath)
		if err != nil {
			return reconciliation{}, fmt.Errorf("database: read live security state: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return reconciliation{}, fmt.Errorf("database: stat live database: %w", err)
	}

	stagedMS, err := newestSecurityEvent(filepath.Join(stagedDir, FileName))
	if err != nil {
		return reconciliation{}, fmt.Errorf("database: read staged security state: %w", err)
	}

	newer := liveMS > stagedMS
	if newer && !acknowledge {
		return reconciliation{}, fmt.Errorf(
			"database: the live database has account-security events newer than the backup (live newest %d ms, backup newest %d ms) — restoring would revive password, role, status, or account-deletion changes made after the backup; re-run with -acknowledge-loss to accept this explicitly (security reconciliation v1)",
			liveMS, stagedMS)
	}
	return reconciliation{acknowledgedLoss: newer, liveNewestMS: liveMS, backupNewestMS: stagedMS}, nil
}

// newestSecurityEvent opens a closed database file READ-ONLY (mode=ro) and
// reads the newest successful account-security event timestamp. Read-only
// opening means the reconciliation never mutates the live file it inspects.
// A hot (uncheckpointed) WAL cannot be read in read-only mode — the open
// fails and the restore refuses rather than reconciling over unseen events.
func newestSecurityEvent(dbPath string) (int64, error) {
	dsn, err := buildReadOnlyDSN(dbPath)
	if err != nil {
		return 0, err
	}
	db, err := openDSN(dsn)
	if err != nil {
		return 0, fmt.Errorf("open read-only: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events := audit.AccountSecurityEvents()
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(events)), ",")
	query := "SELECT COALESCE(MAX(created_at_ms), 0) FROM audit_events WHERE result = 'success' AND event IN (" + placeholders + ")"
	args := make([]any, len(events))
	for i, e := range events {
		args[i] = e
	}

	var ms int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&ms); err != nil {
		return 0, fmt.Errorf("query security events: %w", err)
	}
	return ms, nil
}

// buildReadOnlyDSN derives a mode=ro variant of the standard DSN. The mode
// parameter is the SQLite file-URI open flag honored by the pinned modernc
// driver — a read-only connection cannot create WAL/SHM sidecars or
// checkpoint, so inspecting the live file leaves it byte-for-byte untouched.
// The function FAILS rather than silently degrading to read-write: this is a
// fail-closed component, and a parse error (impossible in practice — the DSN
// is constructed internally) must never quietly drop the read-only guarantee.
func buildReadOnlyDSN(absPath string) (string, error) {
	u, err := url.Parse(buildDSNForFile(absPath))
	if err != nil {
		return "", fmt.Errorf("database: parse DSN for read-only open: %w", err)
	}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
