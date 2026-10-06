// Command backup creates the daily backup unit: a validated online
// database backup, a tarball of the media directory, and the manifest that
// binds them, published into a date-stamped folder under BACKUP_DIR and
// age-encrypted per artifact as the final publish step.
//
// The command runs inside the single-owner unready window: the
// host cron stops the application first, so the database artifact and the
// media tarball share one capture point. The database half fails closed when
// a restore marker is present (the strict-opener gate in
// docs/patterns/go/sqlite.md). The media half reuses the
// manifest/tarball contract.
//
// Encryption: each artifact is encrypted with the age recipient public
// key from AGE_RECIPIENT. The VPS holds only the public key, so published
// folders contain ciphertext only and the VPS can never decrypt them. In
// APP_ENV=production an unset AGE_RECIPIENT fails closed before anything is
// created; in APP_ENV=development it publishes plaintext with a warning so
// local runs work before the key exists.
//
// Retention: after a successful publish, only the newest 10
// date-stamped folders under BACKUP_DIR survive. Pruning touches nothing but
// YYYY-MM-DD folders — the pre-migrate safety nets under
// <DATA_DIR>/backups/ are a different tree and are never pruned.
//
// The -status mode prints the newest published folder and its age (the
// make backup-status glance) and exits 0 even when nothing is published yet.
// The deploy trigger runs the command with -replace-today -pre-migrate (see
// deploy/backup-now.sh) — its rollback unit is prefix-validated because the
// backup runs before the deploy's migration; every other trigger runs with
// neither flag (exact history, never-clobber).
//
// Usage:
//
//	DATA_DIR=/app/data BACKUP_DIR=/app/backups AGE_RECIPIENT=age1... cmd/backup
//	cmd/backup -status
//
// Exit codes: 0 success, 1 on any failure. Outcomes go to structured logs on
// stderr; the unit summary prints to stdout. The command logic lives in
// backup.go — this file is only the entry point.
package main

import (
	"flag"
	"log/slog"
	"os"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	status := flag.Bool("status", false, "print the newest published backup folder and its age (make backup-status)")
	dataDir := flag.String("data-dir", envOr("DATA_DIR", "./data"), "application data directory")
	backupDir := flag.String("backup-dir", envOr("BACKUP_DIR", "./backups"), "published backup unit directory")
	ageRecipient := flag.String("age-recipient", os.Getenv("AGE_RECIPIENT"), "age recipient public key (age1...)")
	// replaceToday is the deploy-trigger escape hatch: when today's unit
	// already exists, the deploy preflight
	// replaces it with a FRESH unit instead of failing closed — the
	// deploy's rollback point must be fresh, and pushing a tag is the
	// operator's explicit resolution. Every other trigger (cron, down.sh)
	// keeps the plain fail-closed behavior — only the deploy scripts pass
	// this flag (alongside -pre-migrate).
	replaceToday := flag.Bool("replace-today", false, "replace today's existing backup unit (deploy trigger only)")
	// preMigrate is the deploy trigger's history mode: the deploy backup runs
	// BEFORE the deploy's migration, so
	// the live database is an older prefix of THIS image's embedded set when
	// the new tag ships a new migration. The unit is then validated as a
	// contiguous prefix instead of the exact set. Only the deploy scripts
	// pass this flag; cron and down.sh keep exact validation.
	preMigrate := flag.Bool("pre-migrate", false, "validate the backup history as a contiguous prefix (deploy trigger only)")
	flag.Parse()

	if err := run(logger, os.Stdout, *status, *dataDir, *backupDir, *ageRecipient, *replaceToday, *preMigrate, time.Now()); err != nil {
		logger.Error("backup failed", "error", err)
		os.Exit(1)
	}
}
