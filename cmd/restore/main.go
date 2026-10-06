// Command restore restores an older validated backup UNIT through the fail-closed
// staged restore (docs/patterns/go/sqlite.md, with coordinated media
// recovery).
//
// The command owns the whole offline workflow: an exclusive 0600 restore
// marker (the strict application opener and readiness fail closed while it
// exists), staged preparation under an operation subdirectory (maintenance
// upgrade, full integrity, foreign keys, transactional session deletion with
// a zero-count re-verification), security reconciliation against the live
// audit trail (SUCCESSFUL account-security events newer than the backup
// refuse activation unless -acknowledge-loss), quarantine-then-atomic
// activation with the previous files retained as the rollback set, and a
// marker-exempt post-activation check before the marker is removed.
//
// The media half: the -backup path selects a complete backup unit
// — the database artifact whose directory also holds the
// .media-manifest.json sidecar and the media tarball it names. The manifest
// is read, validated, and its tarball verified BEFORE the database workflow
// starts (a verification failure never touches the live database or the
// marker). After database activation, the tarball is extracted into the live
// media directory, and only then is the restore marker removed. A restore
// without a valid matching manifest is rejected — pre-migrate DB-only
// artifacts are cmd/migrate's own rollback path, not this command's.
// Production units are age ciphertext; decrypt them on
// the owner's machine first and restore the plaintext unit.
//
// Step 9 of the workflow — starting through the real application opener and
// running readiness/authentication smoke checks before reopening traffic —
// is the OPERATOR's sequence after this command exits successfully (the
// command cannot start the application; the strict-open check it does run is
// the marker-exempt step 8 verification).
//
// Run it while the application is stopped — the same single-owner offline
// window as cmd/migrate (docs/patterns/go/sqlite.md). A crash leaves the
// marker in place, so the application cannot open a half-restored database; re-running
// the command resumes or aborts based on the on-disk state. A failed media
// extraction after activation also leaves the marker and the activated
// database in place — re-run to resume (extraction is idempotent).
//
// Usage:
//
//	DATA_DIR=/app/data cmd/restore -backup data/backups/2026-09-02/sick-fansubs.db [-acknowledge-loss]
//
// Exit codes: 0 success, 1 on any failure. Logs go to stderr; the retained
// quarantine directory prints to stdout.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"sick-fansubs/internal/config"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	backup := flag.String("backup", "", "path to a validated backup unit's database artifact (the .media-manifest.json sidecar must sit next to it)")
	acknowledge := flag.Bool("acknowledge-loss", false, "accept post-backup account-security changes (security reconciliation v1)")
	flag.Parse()

	if *backup == "" {
		logger.Error("staged restore failed", "error", errors.New("backup is required (-backup <path>)"))
		os.Exit(1)
	}

	if err := run(logger, os.Stdout, *backup, *acknowledge); err != nil {
		logger.Error("staged restore failed", "error", err)
		os.Exit(1)
	}
}

// run is the testable seam — main() only wires the environment. The result
// summary prints to out exactly once.
func run(logger *slog.Logger, out io.Writer, backupPath string, acknowledge bool) error {
	// Validate at the seam too — main() checks the flag before calling, but
	// run() owns the contract so the tests pin the real validation.
	if strings.TrimSpace(backupPath) == "" {
		return fmt.Errorf("backup path is required (-backup <path>)")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// The database package builds file: URIs from the backup path
	// (docs/patterns/go/sqlite.md), which requires an absolute path — a
	// relative one errors with a cryptic "invalid uri authority". Absolutize
	// here so `make restore-local` works from anywhere.
	abs, err := filepath.Abs(backupPath)
	if err != nil {
		return fmt.Errorf("resolve backup path: %w", err)
	}
	backupPath = abs

	// A rejected unit never creates the restore marker and never touches the
	// live database. The manifest is the unit's binding: it names the tar and
	// the database artifact it belongs to.
	unitDir := filepath.Dir(backupPath)
	manifestPath := media.ManifestPathFor(backupPath)
	m, err := media.ReadMediaManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("read media manifest %q: %w (cmd/restore restores complete units only — a pre-migrate DB-only artifact belongs to cmd/migrate's rollback path)", manifestPath, err)
	}
	if err := media.ValidateManifest(m); err != nil {
		return fmt.Errorf("invalid media manifest %q: %w", manifestPath, err)
	}
	if m.DBFileName != filepath.Base(backupPath) {
		return fmt.Errorf("media manifest %q binds the database artifact %q, not the selected backup %q — the unit's artifacts are mismatched", manifestPath, m.DBFileName, filepath.Base(backupPath))
	}
	if err := media.VerifyMediaTar(unitDir, m); err != nil {
		return fmt.Errorf("verify media tarball: %w (rejected before any database change)", err)
	}
	tarPath := media.TarPathFor(unitDir, m)

	res, err := database.StagedRestore(database.StagedRestoreOptions{
		DataDir:         cfg.DataDir,
		BackupPath:      backupPath,
		AcknowledgeLoss: acknowledge,
		// The extraction slot: runs after database activation and before
		// marker removal — the database package stays media-free.
		PostActivation: func() error {
			return media.ExtractMediaTar(cfg.DataDir, tarPath)
		},
	})
	if err != nil {
		return err
	}

	if res.AcknowledgedLoss {
		// The operator accepted it explicitly; keep the evidence in the logs.
		logger.Warn("restore proceeded over post-backup account-security changes",
			"liveNewestMs", res.LiveNewestMS, "backupNewestMs", res.BackupNewestMS)
	}

	// Best-effort summary: the restore is complete and the marker removed, so
	// a failed stdout write must not turn it into a reported failure — the
	// retained quarantine directory stays discoverable on disk.
	fmt.Fprintf(out, "restore complete: %s activated; media extracted from %s; previous database files retained in %s (the rollback set — remove after verification)\n",
		res.BackupPath, tarPath, res.QuarantineDir)
	return nil
}
