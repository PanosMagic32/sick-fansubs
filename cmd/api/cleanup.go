package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
	"sick-fansubs/internal/store"
)

// cleanupExpiredSessions deletes expired session rows in bounded batches
// until a batch removes fewer than the batch size (nothing expired remains).
// It honors ctx cancellation between batches and returns the total deleted.
func cleanupExpiredSessions(ctx context.Context, db *sql.DB) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := store.DeleteExpiredSessions(ctx, db, time.Now().UnixMilli(), store.CleanupBatchSize)
		if err != nil {
			return total, err
		}
		total += n
		if n < store.CleanupBatchSize {
			return total, nil
		}
	}
}

// startSessionCleanup runs the hourly expired-session cleanup until ctx is
// cancelled, logging failures and retrying on the next tick. Main waits on it
// before closing the database.
func startSessionCleanup(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := cleanupExpiredSessions(ctx, db)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.Warn("expired-session cleanup failed", "error", err)
				}
				continue
			}
			if n > 0 {
				slog.Info("expired-session cleanup", "deleted", n)
			}
		}
	}
}

// startupPassBudget bounds each startup maintenance pass — a hung pass must
// not block the server from starting; the next hourly tick retries.
const startupPassBudget = 30 * time.Second

// mediaSweepInterval is the media orphan sweep cadence: hourly, like the
// session cleanup — the 24 h grace window makes the tick
// rate a responsiveness knob only.
const mediaSweepInterval = time.Hour

// auditRetentionWindow is the audit-retention window: events older than this are
// deleted by the cleanup task. It must stay ≥ the backup-retention window
// (10 daily units) — events are the post-restore reconciliation evidence.
const auditRetentionWindow = 365 * 24 * time.Hour

// cleanupAuditEvents deletes events older than the retention window in
// bounded batches until a batch removes fewer than the batch size (nothing
// expired remains). It honors ctx cancellation between batches and returns
// the total deleted. The cutoff is the wall clock at call time — the 365-day
// window dwarfs any in-run drift.
func cleanupAuditEvents(ctx context.Context, db *sql.DB) (int64, error) {
	cutoff := time.Now().Add(-auditRetentionWindow).UnixMilli()
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := store.DeleteExpiredAuditEvents(ctx, db, cutoff, store.CleanupBatchSize)
		if err != nil {
			return total, err
		}
		total += n
		if n < store.CleanupBatchSize {
			return total, nil
		}
	}
}

// startAuditCleanup runs the hourly audit-retention cleanup on the session
// cleanup's cadence until ctx is cancelled, logging failures and retrying on
// the next tick. The caller waits on it before shutting down.
func startAuditCleanup(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := cleanupAuditEvents(ctx, db)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.Warn("audit cleanup failed", "error", err)
				}
				continue
			}
			if n > 0 {
				slog.Info("audit cleanup", "deleted", n)
			}
		}
	}
}

// walMonitorInterval is the WAL-size readout cadence: hourly, like the other
// background tasks.
const walMonitorInterval = time.Hour

// walWarnBytes is the WAL-size warning threshold: ~16× SQLite's default
// auto-checkpoint target (1000 pages), high enough that a healthy WAL stays
// quiet and low enough to catch a pinned one early.
const walWarnBytes = 64 << 20

// databaseFileSizes reports the main database file and its WAL sidecar in
// bytes; a missing file is zero. The readout is observation only — it never
// checkpoints or tunes anything (docs/patterns/go/sqlite.md rule 13).
func databaseFileSizes(dataDir string) (mainBytes, walBytes int64, err error) {
	if info, statErr := os.Stat(filepath.Join(dataDir, database.FileName)); statErr == nil {
		mainBytes = info.Size()
	} else if !os.IsNotExist(statErr) {
		return 0, 0, statErr
	}

	info, err := os.Stat(filepath.Join(dataDir, database.FileName+"-wal"))
	if err == nil {
		walBytes = info.Size()
	} else if !os.IsNotExist(err) {
		return 0, 0, err
	}
	return mainBytes, walBytes, nil
}

// checkWALSize logs one WAL-size sample: Info normally, Warn at or above
// walWarnBytes. A large reading is a high-water mark — only repeated samples
// show growth.
func checkWALSize(dataDir string) {
	mainBytes, walBytes, err := databaseFileSizes(dataDir)
	if err != nil {
		slog.Warn("database file size read failed", "error", err)
		return
	}
	if walBytes >= walWarnBytes {
		slog.Warn("database WAL at high-water size — confirm no long-running reader pins it (the file stays this large until truncated)",
			"walBytes", walBytes, "thresholdBytes", walWarnBytes, "databaseBytes", mainBytes)
		return
	}
	slog.Info("database WAL size", "walBytes", walBytes, "databaseBytes", mainBytes)
}

// startWALMonitor samples the WAL size at startup and hourly until ctx is
// cancelled. It only reads file metadata — no checkpoint, no pragma change
// (docs/patterns/go/sqlite.md rule 13). The caller waits on it before close.
func startWALMonitor(ctx context.Context, dataDir string) {
	checkWALSize(dataDir)
	ticker := time.NewTicker(walMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkWALSize(dataDir)
		}
	}
}

// sweepMediaOrphans runs one grace-windowed orphan sweep:
// snapshot the referenced set from the database, then walk the media
// directory. The reference snapshot and the deletion policy live in
// store.ReferencedMediaPaths and media.Sweep.
func sweepMediaOrphans(ctx context.Context, db *sql.DB, dataDir string) (media.SweepStats, error) {
	refs, err := store.ReferencedMediaPaths(ctx, db)
	if err != nil {
		return media.SweepStats{}, err
	}
	return media.Sweep(ctx, slog.Default(), dataDir, refs, time.Now(), media.SweepGrace)
}

// startMediaSweep runs the hourly media orphan sweep until ctx is cancelled,
// logging failures and retrying on the next tick. The caller waits on it
// before shutting down.
func startMediaSweep(ctx context.Context, db *sql.DB, dataDir string) {
	ticker := time.NewTicker(mediaSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats, err := sweepMediaOrphans(ctx, db, dataDir)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.Warn("media orphan sweep failed", "error", err,
						"scanned", stats.Scanned, "deleted", stats.Deleted,
						"tempDeleted", stats.TempDeleted, "failed", stats.Failed)
				}
				continue
			}
			slog.Info("media orphan sweep",
				"scanned", stats.Scanned, "deleted", stats.Deleted,
				"tempDeleted", stats.TempDeleted, "failed", stats.Failed)
		}
	}
}
