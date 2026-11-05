package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// acquireRestoreLock serializes the whole staged restore. The marker is the
// crash record, not a liveness lock: a second command that finds a marker
// would otherwise resume a run that is still in progress. The lock file is
// created in the data directory and held for the command's lifetime.
func acquireRestoreLock(dataDir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dataDir, ".restore.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open restore lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("another restore is already running for %s", dataDir)
		}
		return nil, fmt.Errorf("acquire restore lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
