// Package logging owns the application's log SINK: the
// stderr JSON stream stays the container-facing channel, and a second
// JSON-lines file beside it — size-rotated under the data directory — gives
// the super-admin logs viewer something to read (GET /api/v1/staff/logs).
//
// The package has no configuration surface on purpose: no env var, no level
// knob, no retention policy ("no new env vars"). The numbers live in
// the constants below and are pinned by test.
//
// Two rules shape the writer:
//
//   - Rotation is size-triggered and bounded: the live file plus its backups
//     hold at most ~maxBytes × maxFiles (a single record larger than maxBytes
//     passes through whole rather than being split).
//   - A logging failure NEVER fails the request. slog ignores the error a
//     handler returns, so the writer reports a failure once to its reporter
//     (stderr) and keeps going: a broken sink degrades to stderr-only rather
//     than taking the process down or silently losing everything.
package logging

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	// DirName is the log directory, created under the data directory like
	// data/backups and data/media (main.go owns the data directory itself).
	DirName = "logs"

	// FileName is the live log file inside DirName. Each rotated backup adds
	// a numeric suffix: FileName+".1" is the newest backup.
	FileName = "app.log"

	// DefaultMaxBytes is the rotation threshold — 5 MB.
	// A write that would cross it rotates first, so a single record is
	// never split across two files.
	DefaultMaxBytes = 5 << 20

	// DefaultMaxFiles counts the live file PLUS its backups: 4 files, 5 MB
	// each — the ~20 MB ceiling. The reader walks the same
	// number, so the two sides cannot disagree about how many files exist.
	DefaultMaxFiles = 4

	// dirMode/fileMode keep the sink owner-only, like the rest of the data
	// directory: a log carries client addresses and operational detail.
	dirMode  = 0o700
	fileMode = 0o600
)

// RotatingWriter is an io.Writer that appends to a size-rotated log file. It
// is safe for concurrent use, which the slog handler wrapping it requires.
type RotatingWriter struct {
	path     string
	maxBytes int64
	maxFiles int

	// report receives sink failures, at most once per failure episode (see
	// reportOnce). A nil reporter is silent.
	report func(error)

	mu       sync.Mutex
	f        *os.File
	size     int64
	reported bool
}

// NewRotatingWriter creates dir if needed and opens the live file for
// appending, adopting the size of a file left behind by an earlier process.
// report receives sink failures; pass nil to stay silent.
//
// maxFiles counts the live file plus its backups, so it must be at least 2 —
// a "rotation" that produced no backup would just be truncation.
func NewRotatingWriter(dir string, maxBytes int64, maxFiles int, report func(error)) (*RotatingWriter, error) {
	if maxBytes <= 0 {
		return nil, errors.New("logging: maxBytes must be positive")
	}
	if maxFiles < 2 {
		return nil, errors.New("logging: maxFiles must be at least 2")
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, err
	}

	w := &RotatingWriter{
		path:     filepath.Join(dir, FileName),
		maxBytes: maxBytes,
		maxFiles: maxFiles,
		report:   report,
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// Write appends p, rotating first when the write would cross the size
// threshold. A failed rotation costs the ceiling, never the record: the
// handle it closed is reopened so the write still lands.
func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.f == nil {
		// A previous open failed (full disk, permissions). Retry once per
		// write so a transient condition heals without a restart.
		if err := w.open(); err != nil {
			w.reportOnce(err)
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			w.reportOnce(err)
			// rotate closes the live handle before renaming, so a failure
			// leaves the writer without one. Reopen: the record matters more
			// than the ceiling, and the rename may not even have happened.
			if w.f == nil {
				if err := w.open(); err != nil {
					w.reportOnce(err)
					return 0, err
				}
			}
		}
	}

	n, err := w.f.Write(p)
	w.size += int64(n)
	if err != nil {
		w.reportOnce(err)
		return n, err
	}
	w.reported = false
	return n, nil
}

// Close releases the live file handle. It is not a latch: a later Write
// reopens the file (the writer is a sink, not a lifecycle object), which is
// what makes the failure paths above recoverable. Safe to call twice.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// rotate shifts FileName → FileName.1, .1 → .2, …, dropping the oldest
// backup, then opens a fresh live file.
func (w *RotatingWriter) rotate() error {
	if w.f != nil {
		// A close failure must not block the rotation: the renames below are
		// what bound the disk usage.
		_ = w.f.Close()
		w.f = nil
	}

	if err := removeIfExists(w.backupPath(w.maxFiles - 1)); err != nil {
		return err
	}
	for i := w.maxFiles - 2; i >= 1; i-- {
		if err := renameIfExists(w.backupPath(i), w.backupPath(i+1)); err != nil {
			return err
		}
	}
	if err := renameIfExists(w.path, w.backupPath(1)); err != nil {
		return err
	}
	return w.open()
}

func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.f = f
	w.size = info.Size()
	return nil
}

// backupPath is the path of backup slot n (1 is the newest).
func (w *RotatingWriter) backupPath(n int) string {
	return w.path + "." + strconv.Itoa(n)
}

// reportOnce reports an error at most once per failure episode, so a sink
// that fails on every write cannot flood the stderr stream it sits beside.
// Write clears the latch after a success.
func (w *RotatingWriter) reportOnce(err error) {
	if w.report == nil || w.reported {
		return
	}
	w.reported = true
	w.report(err)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func renameIfExists(from, to string) error {
	if err := os.Rename(from, to); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
