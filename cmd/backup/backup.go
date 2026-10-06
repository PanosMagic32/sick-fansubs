package main

import (
	"cmp"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Athens inside alpine containers (the cron timezone)

	"filippo.io/age"

	appconfig "sick-fansubs/internal/config"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
)

const (
	// backupDeadline bounds the page-copy loop (docs/patterns/go/sqlite.md:
	// checked between page batches). Generous for a small DB; measured at the
	// rehearsal.
	backupDeadline = 10 * time.Minute

	// keepBackups is the local retention: the newest N date-stamped
	// folders survive a successful publish. CONSTRAINT: it must
	// stay well below the audit retention window (365 days — cmd/api's
	// auditRetentionWindow) — audit events are the post-restore
	// reconciliation evidence, so any retained backup's event
	// history must still be queryable.
	keepBackups = 10

	athensTZName = "Europe/Athens"

	// dbFileName/mediaTarName are the fixed unit artifact names inside the
	// date-stamped folder; the manifest name derives from the database name
	// via media.ManifestPathFor.
	dbFileName   = "sick-fansubs.db"
	mediaTarName = "sick-fansubs.media.tar"
)

// stagingSuffix/previousSuffix name the unit swap's sibling folders
// (the staged replace). The suffixes are
// deliberately NOT a YYYY-MM-DD shape, so backupFolders/prune/mega-put never
// match them — a crash-leftover is foreign and inert by design.
const (
	stagingSuffix  = ".staging"
	previousSuffix = ".previous"
)

// folderDatePattern matches date-stamped backup folders. Pruning and status
// ignore everything else — foreign files/folders under BACKUP_DIR are never
// touched.
var folderDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// config is the command's environment contract (same conventions as
// internal/config: flags override env, DATA_DIR defaults to ./data resolved
// to an absolute path). AgeRecipient is the parsed recipient — parsed once
// at resolution so the pipeline never re-parses the same string.
type config struct {
	DataDir      string
	BackupDir    string
	AgeRecipient *age.X25519Recipient
}

// run is the testable seam — main() only wires flags/env, the logger, and
// now. runBackup takes the resolved config; -status resolves BACKUP_DIR only,
// so the diagnostic works while a missing age key is diagnosed.
func run(logger *slog.Logger, out io.Writer, status bool, dataDir, backupDir, ageRecipient string, replaceToday, preMigrate bool, now time.Time) error {
	if status {
		if backupDir == "" {
			return fmt.Errorf("BACKUP_DIR must not be empty")
		}
		return runStatus(out, absolutePath(backupDir))
	}
	cfg, err := resolveConfig(dataDir, backupDir, ageRecipient)
	if err != nil {
		return err
	}
	return runBackup(logger, out, cfg, replaceToday, preMigrate, now)
}

// resolveConfig validates and absolutizes the environment contract.
func resolveConfig(dataDir, backupDir, ageRecipient string) (config, error) {
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = appconfig.EnvDevelopment
	}
	var ageRecipientParsed *age.X25519Recipient
	if appEnv != appconfig.EnvDevelopment && appEnv != appconfig.EnvProduction {
		return config{}, fmt.Errorf("APP_ENV must be %q or %q, got %q", appconfig.EnvDevelopment, appconfig.EnvProduction, appEnv)
	}
	// Production fails closed before anything is created: published backups
	// containing user data must never be plaintext.
	if appEnv == appconfig.EnvProduction && ageRecipient == "" {
		return config{}, fmt.Errorf("AGE_RECIPIENT is required in APP_ENV=production — refusing to publish a plaintext backup")
	}
	if ageRecipient != "" {
		r, err := age.ParseX25519Recipient(ageRecipient)
		if err != nil {
			return config{}, fmt.Errorf("AGE_RECIPIENT is not a valid age recipient: %w", err)
		}
		ageRecipientParsed = r
	}
	if dataDir == "" {
		return config{}, fmt.Errorf("DATA_DIR must not be empty")
	}
	if backupDir == "" {
		return config{}, fmt.Errorf("BACKUP_DIR must not be empty")
	}
	return config{DataDir: absolutePath(dataDir), BackupDir: absolutePath(backupDir), AgeRecipient: ageRecipientParsed}, nil
}

// absolutePath resolves p to an absolute path, returning p unchanged when the
// resolution fails (callers are already validated).
func absolutePath(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

// runBackup executes the full unit pipeline; the replace and pre-migrate
// flags are the deploy trigger's policy (docs/ops/backup.md,
// cmd/backup/AGENTS.md).
func runBackup(logger *slog.Logger, out io.Writer, cfg config, replaceToday, preMigrate bool, now time.Time) (retErr error) {
	athens, err := time.LoadLocation(athensTZName)
	if err != nil {
		return fmt.Errorf("load timezone %s: %w", athensTZName, err)
	}
	folderName := now.In(athens).Format("2006-01-02")
	folder := filepath.Join(cfg.BackupDir, folderName)

	// Refuse to publish a second unit into today's folder — a successful
	// daily backup already exists; resolve by hand rather than clobbering.
	// The ONE exception is the deploy trigger (replaceToday): its rollback
	// point must be fresh. The replacement is STAGED below — the old unit is
	// removed only after the new one is complete, so a failed replace never
	// destroys the rollback point it exists to protect.
	exists := false
	if entries, err := os.ReadDir(folder); err == nil && len(entries) > 0 {
		exists = true
		if !replaceToday {
			return fmt.Errorf("backup folder %s already exists and is not empty — today's unit is already published", folder)
		}
	}

	// Build the unit in a STAGING folder. Every `<date>.staging` under
	// BACKUP_DIR can only be a previous run's crash leftover — the production
	// host lock serializes triggers and a successful run renames the stage into
	// place — so stale stages are cleared here; reusing one would let the
	// crashed run's plaintext temp files ride into the world-traversable
	// published unit. The command takes no lock itself, so concurrent manual
	// runs are unsupported. Created owner-only (0700); an ENCRYPTED unit's
	// folder is widened to 0755 only at the publish step, once it holds
	// ciphertext alone.
	if err := clearStaleStages(logger, cfg.BackupDir); err != nil {
		return err
	}
	stage := folder + stagingSuffix
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return fmt.Errorf("create backup staging folder: %w", err)
	}
	defer func() {
		if retErr != nil {
			_ = os.RemoveAll(stage)
		}
	}()

	// The deploy trigger (preMigrate) validates the history as a contiguous
	// prefix — its backup is taken BEFORE the deploy's migration, so the live
	// database can be an older prefix of this artifact's embedded set. Every
	// other trigger keeps the exact-history check. The deadline is wall-clock
	// (bounds the real page-copy loop); `now` only pins the folder name and
	// manifest timestamp.
	backupFn := database.BackupForDaily
	if preMigrate {
		backupFn = database.BackupForPreMigration
	}
	dbPath, err := backupFn(cfg.DataDir, stage, dbFileName, time.Now().Add(backupDeadline))
	if err != nil {
		return fmt.Errorf("database backup: %w", err)
	}
	logger.Info("database artifact published", "path", dbPath)

	tarSHA, err := media.BuildMediaTar(cfg.DataDir, stage, mediaTarName)
	if err != nil {
		return fmt.Errorf("media tarball: %w", err)
	}
	manifest := media.MediaManifest{
		BackupTimestampMS: now.UnixMilli(),
		DBFileName:        dbFileName,
		MediaTarFileName:  mediaTarName,
		MediaTarSHA256:    tarSHA,
	}
	manifestPath := media.ManifestPathFor(dbPath)
	if err := media.WriteMediaManifest(manifestPath, manifest); err != nil {
		return err
	}
	logger.Info("media tarball + manifest published", "tar", filepath.Join(stage, mediaTarName), "manifest", manifestPath)

	if cfg.AgeRecipient != nil {
		for _, name := range []string{dbFileName, mediaTarName, filepath.Base(manifestPath)} {
			if err := encryptFile(stage, name, cfg.AgeRecipient); err != nil {
				return err
			}
		}
		logger.Info("unit encrypted with age; folder holds ciphertext only")
	} else {
		logger.Warn("publishing PLAINTEXT unit — AGE_RECIPIENT unset (development only; production fails closed)")
	}

	// The published unit holds exactly its artifacts. Anything else is crash
	// debris and must abort the publish rather than ride into a
	// world-traversable folder (the entry sweep makes this unreachable in
	// practice; the check keeps a future regression from publishing it).
	expected := []string{dbFileName, mediaTarName, filepath.Base(manifestPath)}
	if cfg.AgeRecipient != nil {
		expected = []string{dbFileName + ".age", mediaTarName + ".age", filepath.Base(manifestPath) + ".age"}
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return fmt.Errorf("read backup staging folder: %w", err)
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("backup staging folder holds %d entries, want exactly %d unit artifacts", len(entries), len(expected))
	}
	for _, e := range entries {
		if !slices.Contains(expected, e.Name()) {
			return fmt.Errorf("backup staging folder holds unexpected entry %q — refusing to publish", e.Name())
		}
	}

	// Every entry is synced (the encryption loop syncs each ciphertext, but a
	// plaintext unit gets none there) before the publish rename, so the rename
	// itself is the only pending step the directory sync below persists.
	if err := syncPath(stage); err != nil {
		return fmt.Errorf("sync backup staging folder: %w", err)
	}

	if cfg.AgeRecipient != nil {
		// The folder becomes world-traversable only NOW, after the entry check
		// proved it holds ciphertext alone: age encryption is the access
		// boundary, and the host-side push script reads the unit as the deploy
		// account. The explicit chmod defeats any umask.
		if err := os.Chmod(stage, 0o755); err != nil {
			return fmt.Errorf("publish folder mode: %w", err)
		}
	}

	// The old unit (when replacing) moves aside first and is removed only
	// after the new one is in place — a failure before the swap leaves it
	// untouched, and the defer above removes the stage.
	if exists {
		old := folder + previousSuffix
		_ = os.RemoveAll(old) // a crashed earlier replace may have left one
		if err := os.Rename(folder, old); err != nil {
			return fmt.Errorf("move today's backup unit aside: %w", err)
		}
		if err := os.Rename(stage, folder); err != nil {
			_ = os.Rename(old, folder) // restore the previous unit
			return fmt.Errorf("publish replacement backup unit: %w", err)
		}
		_ = os.RemoveAll(old)
		logger.Info("replacing today's existing backup unit (deploy trigger)", "folder", folder)
	} else if err := os.Rename(stage, folder); err != nil {
		return fmt.Errorf("publish backup unit: %w", err)
	}

	// Publication must survive a crash: fsync the backup directory after the
	// rename (and the replace swap). A failure here is fatal by design —
	// reporting success on a rename that may not be durable would let the
	// operator, the push script, and the off-VPS RPO trust a unit that can
	// vanish.
	if err := syncPath(cfg.BackupDir); err != nil {
		return fmt.Errorf("sync backup directory after publish (the unit %q is renamed into place but not crash-durable — remove it before re-running): %w", folder, err)
	}

	if _, err := pruneOld(logger, cfg.BackupDir, keepBackups); err != nil {
		// The new unit is complete and published; a retention failure is
		// reported (with the current folder count) without failing the backup
		// the trigger just produced.
		retained, _ := backupFolders(cfg.BackupDir)
		logger.Warn("retention prune failed — remove the old backup folders by hand", "error", err, "retained", len(retained))
	}

	// Best-effort summary: the unit is published and durable, so a failed
	// stdout write must not fail the backup — the cron's push follows the
	// exit code, and the folder is visible on disk.
	fmt.Fprintf(out, "backup complete: %s (db + media tarball + manifest%s)\n",
		folder, map[bool]string{true: ", age-encrypted", false: ", plaintext"}[cfg.AgeRecipient != nil])
	return nil
}

// encryptFile age-encrypts dir/name to dir/name.age and removes the
// plaintext. The .age starts owner-only (0600) and is widened to 0644 only
// after it is complete and fsynced (see the publish chmod); a pre-existing
// .age is refused (O_EXCL) rather than silently replaced.
func encryptFile(dir, name string, recipient *age.X25519Recipient) error {
	src := filepath.Join(dir, name)
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %q for encryption: %w", name, err)
	}
	defer in.Close()

	dst := src + ".age"
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create %q: %w", name+".age", err)
	}
	w, err := age.Encrypt(out, recipient)
	if err != nil {
		out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("encrypt %q: %w", name, err)
	}
	if _, err := io.Copy(w, in); err != nil {
		w.Close()
		out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("encrypt %q: %w", name, err)
	}
	if err := w.Close(); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("finish encryption of %q: %w", name, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("close %q: %w", name+".age", err)
	}
	// Publish discipline (docs/patterns/go/sqlite.md, applied to the
	// ciphertext): the .age is fsynced before its plaintext is removed, and
	// the folder is fsynced after the removal — the VPS-local generations are
	// the primary copy, so a crash must never leave only a torn
	// ciphertext.
	if err := syncPath(dst); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("sync %q: %w", name+".age", err)
	}
	// World-readable CIPHERTEXT, but only once COMPLETE:
	// age encryption is the access boundary and the host-side
	// push script reads the unit as the deploy account. The explicit chmod
	// defeats any umask; a torn .age never becomes readable mid-write (it was
	// created 0600 above).
	if err := os.Chmod(dst, 0o644); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("publish %q mode: %w", name+".age", err)
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove plaintext %q: %w", name, err)
	}
	if err := syncPath(dir); err != nil {
		return fmt.Errorf("sync backup folder after encryption: %w", err)
	}
	return nil
}

// syncPath fsyncs a file or directory so a completed publish survives a
// crash (the publication discipline in docs/patterns/go/sqlite.md).
func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// backupFolders lists date-stamped folders under backupDir, newest first
// (YYYY-MM-DD strings sort chronologically). Foreign entries are never
// returned and never touched.
func backupFolders(backupDir string) ([]string, error) {
	entries, err := os.ReadDir(backupDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read backup directory: %w", err)
	}
	var folders []string
	for _, e := range entries {
		if !e.IsDir() || !folderDatePattern.MatchString(e.Name()) {
			continue
		}
		folders = append(folders, filepath.Join(backupDir, e.Name()))
	}
	slices.SortFunc(folders, func(a, b string) int { return cmp.Compare(b, a) })
	return folders, nil
}

// pruneOld removes date-stamped folders beyond keep, oldest first. Pruning
// runs only after a successful publish (the caller's ordering) and only over
// YYYY-MM-DD folders.
func pruneOld(logger *slog.Logger, backupDir string, keep int) (int, error) {
	folders, err := backupFolders(backupDir)
	if err != nil {
		return 0, err
	}
	if len(folders) <= keep {
		return 0, nil
	}
	pruned := 0
	for _, f := range folders[keep:] {
		if err := os.RemoveAll(f); err != nil {
			return pruned, fmt.Errorf("prune %q: %w", f, err)
		}
		pruned++
		logger.Info("pruned old backup", "folder", f)
	}
	return pruned, nil
}

// clearStaleStages removes every date-shaped staging folder under backupDir —
// each one is provably a crashed run's leftover on the production host.
func clearStaleStages(logger *slog.Logger, backupDir string) error {
	entries, err := os.ReadDir(backupDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read backup directory: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasSuffix(name, stagingSuffix) {
			continue
		}
		if !folderDatePattern.MatchString(strings.TrimSuffix(name, stagingSuffix)) {
			continue
		}
		p := filepath.Join(backupDir, name)
		logger.Warn("removing stale staging folder left by a previous crashed run", "stage", p)
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("remove stale backup staging folder: %w", err)
		}
	}
	return nil
}

// runStatus prints the newest published folder and its age — the lightweight
// backup-status glance.
func runStatus(out io.Writer, backupDir string) error {
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		if _, err := fmt.Fprintf(out, "backup directory %s does not exist\n", backupDir); err != nil {
			return fmt.Errorf("write status: %w", err)
		}
		return nil
	}
	folders, err := backupFolders(backupDir)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		if _, err := fmt.Fprintln(out, "no published backups yet"); err != nil {
			return fmt.Errorf("write status: %w", err)
		}
		return nil
	}
	newest := folders[0]
	info, err := os.Stat(newest)
	if err != nil {
		return fmt.Errorf("stat newest backup: %w", err)
	}
	age := time.Since(info.ModTime()).Truncate(time.Second)
	if _, err := fmt.Fprintf(out, "newest backup: %s (%s ago)\n", newest, age); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}

// envOr returns env[name] when set, else def — the flag default bridge so
// flags override the environment without duplicating the fallbacks.
func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
