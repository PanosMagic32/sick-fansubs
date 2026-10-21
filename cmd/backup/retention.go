package main

import (
	"cmp"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// backupFolders lists date-stamped folders under backupDir, newest first
// (YYYY-MM-DD strings sort chronologically). Foreign entries are never
// returned and never touched. It and clearStaleStages are the two retention
// operations that enumerate existing units rather than create one.
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
