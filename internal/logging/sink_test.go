package logging

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Rotating sink tests: the size trigger, the
// file-count ceiling, and the rule that a broken sink never takes the process
// down.

// TestDefaults_MatchTheDecision pins the sink's numbers — 5 MB × 4 files.
// They are constants, not configuration, so a drift would be a
// contract change rather than a tweak.
func TestDefaults_MatchTheDecision(t *testing.T) {
	t.Parallel()

	if DefaultMaxBytes != 5<<20 {
		t.Errorf("DefaultMaxBytes = %d, want %d (5 MB)", DefaultMaxBytes, 5<<20)
	}
	if DefaultMaxFiles != 4 {
		t.Errorf("DefaultMaxFiles = %d, want 4 (the live file plus three backups)", DefaultMaxFiles)
	}
	if DirName != "logs" || FileName != "app.log" {
		t.Errorf("sink layout = %s/%s, want logs/app.log", DirName, FileName)
	}
}

func TestNewRotatingWriter_RejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		bytes int64
		files int
	}{
		{"zero bytes", 0, 3},
		{"negative bytes", -1, 3},
		{"single file", 64, 1},
	} {
		if _, err := NewRotatingWriter(t.TempDir(), tc.bytes, tc.files, nil); err == nil {
			t.Errorf("%s: want an error, got nil", tc.name)
		}
	}
}

// TestNewRotatingWriter_UncreatableDirectory pins the constructor's failure
// path — the one main.go degrades from — rather than a panic or a silent
// stderr-only fallback decided deeper down.
func TestNewRotatingWriter_UncreatableDirectory(t *testing.T) {
	t.Parallel()

	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("x"), fileMode); err != nil {
		t.Fatalf("seed parent file: %v", err)
	}
	if _, err := NewRotatingWriter(filepath.Join(parent, "logs"), 64, 3, nil); err == nil {
		t.Error("want an error when the directory cannot be created, got nil")
	}
}

// TestRotatingWriter_RotatesAndBoundsFiles writes well past the threshold and
// pins three properties at once: the file count never exceeds maxFiles, no
// record is ever split by a rotation, and the newest record is always in the
// live file while older ones sit in the numbered backups.
func TestRotatingWriter_RotatesAndBoundsFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const maxBytes, maxFiles = 64, 3
	w, err := NewRotatingWriter(dir, maxBytes, maxFiles, nil)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	// Enough records to cross the threshold several times, so the file-count
	// ceiling — not just the first rotation — is what the assertions see.
	const records = 40
	for i := range records {
		if _, err := fmt.Fprintf(w, "{\"n\":%d}\n", i); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	names := logFileNames(t, dir, maxFiles)
	if len(names) != maxFiles {
		t.Fatalf("files = %v, want exactly %d once the threshold is crossed", names, maxFiles)
	}

	var total int64
	perFile := make([][]int, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		total += int64(len(data))
		perFile = append(perFile, decodeRecords(t, name, data))
	}
	if total > maxBytes*maxFiles {
		t.Errorf("total bytes = %d, want at most %d (maxBytes × maxFiles)", total, maxBytes*maxFiles)
	}

	// The live file holds the newest record last; .1 holds strictly older
	// ones, so the rotation moved a complete file rather than a fragment.
	live := readN(t, filepath.Join(dir, FileName))
	if len(live) == 0 || live[len(live)-1] != records-1 {
		t.Errorf("live file = %v, want the newest record (%d) last", live, records-1)
	}
	if first := readN(t, filepath.Join(dir, FileName+".1")); len(first) == 0 || first[0] >= records-1 {
		t.Errorf(".1 = %v, want records older than the live file", first)
	}

	// Read oldest file first (names is newest-first), the surviving files form
	// one contiguous run ending at the newest record — the oldest file was
	// dropped whole, not truncated.
	var seen []int
	for _, p := range slices.Backward(perFile) {
		seen = append(seen, p...)
	}
	if len(seen) == 0 || seen[len(seen)-1] != records-1 {
		t.Fatalf("collected records = %v, want them to end at the newest", seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] != seen[i-1]+1 {
			t.Fatalf("records are not contiguous at %d: %v", i, seen)
		}
	}
}

// TestRotatingWriter_FailedRotationKeepsTheRecord pins the recovery path the
// writer exists for: rotate() closes the live handle before it renames, so a
// rotation failure (here: the oldest slot is a non-empty directory, which
// Remove refuses) must reopen the file rather than lose the record or write
// into a nil handle.
func TestRotatingWriter_FailedRotationKeepsTheRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const maxBytes, maxFiles = 32, 3

	// The oldest backup slot cannot be removed: a non-empty directory there
	// fails os.Remove with ENOTEMPTY, which is the first thing rotate does.
	for _, name := range []string{FileName + ".1", FileName + ".2"} {
		slot := filepath.Join(dir, name)
		if err := os.MkdirAll(slot, dirMode); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(slot, "occupied"), []byte("x"), fileMode); err != nil {
			t.Fatalf("occupy %s: %v", name, err)
		}
	}

	var reported []error
	w, err := NewRotatingWriter(dir, maxBytes, maxFiles, func(err error) { reported = append(reported, err) })
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	first := strings.Repeat("a", 20) + "\n"
	if _, err := w.Write([]byte(first)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	second := strings.Repeat("b", 20) + "\n"
	if n, err := w.Write([]byte(second)); err != nil {
		t.Fatalf("write across the threshold: %v (wrote %d bytes)", err, n)
	}

	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read live file: %v", err)
	}
	if got := string(data); got != first+second {
		t.Errorf("live file = %q, want both records (the ceiling may fail, the record may not)", got)
	}
	if len(reported) != 1 {
		t.Errorf("reported %d failures, want exactly 1", len(reported))
	}
}

// TestRotatingWriter_ConcurrentWritesAreRecorded pins the mutex: the handler
// sharing this writer is concurrent, and a torn or interleaved record would be
// invisible in a sequential test. The race DETECTOR is unavailable here (the
// project builds with CGO disabled and -race needs it), so the assertions are
// about the RECORD STREAM: every surviving record decodes, and each writer's
// own sequence is contiguous up to its last record — a lost write would leave
// a hole. Records older than the ceiling are legitimately discarded, which is
// why this cannot assert a total count.
func TestRotatingWriter_ConcurrentWritesAreRecorded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const maxFiles = 4

	// The ceiling deliberately exceeds what this test writes (4 × 2 KB against
	// ~4.2 KB of records), so rotation runs several times but prunes nothing —
	// every record must survive, which is what makes a lost write detectable.
	w, err := NewRotatingWriter(dir, 2048, maxFiles, nil)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	const writers, perWriter = 8, 25
	var wg sync.WaitGroup
	for id := range writers {
		wg.Go(func() {
			for n := range perWriter {
				if _, err := fmt.Fprintf(w, "{\"id\":%d,\"n\":%d}\n", id, n); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()

	seen := make(map[int][]int, writers)
	// Oldest file first, so each writer's sequence reads in write order.
	names := logFileNames(t, dir, maxFiles)
	for _, name := range slices.Backward(names) {

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for line := range strings.Lines(strings.TrimSuffix(string(data), "\n")) {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var rec struct{ ID, N int }
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("%s: torn record %q: %v", name, line, err)
			}
			seen[rec.ID] = append(seen[rec.ID], rec.N)
		}
	}

	// Every writer is present and every writer's own sequence is complete and
	// contiguous: nothing was torn by an interleaved write, and no write was
	// lost. Records older than the ceiling would legitimately vanish, which is
	// why the ceiling above is set past the total volume.
	if len(seen) != writers {
		t.Fatalf("writers present = %d, want %d", len(seen), writers)
	}
	for id, ns := range seen {
		if len(ns) != perWriter {
			t.Errorf("writer %d: %d records, want %d (none lost)", id, len(ns), perWriter)
			continue
		}
		for i, n := range ns {
			if n != i {
				t.Errorf("writer %d: records %v are not the ordered sequence 0..%d", id, ns, perWriter-1)
				break
			}
		}
	}
}

// TestNewRotatingWriter_CreatesTheDirectory pins the happy path of the
// directory contract: the sink creates its own subdirectory under the data
// directory, like data/backups and data/media do.
func TestNewRotatingWriter_CreatesTheDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "logs")
	w, err := NewRotatingWriter(dir, 1024, 2, nil)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat sink dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != dirMode {
		t.Errorf("dir mode = %o, want %o (the sink is owner-only)", perm, dirMode)
	}
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRotatingWriter_AdoptsExistingSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(strings.Repeat("x", 40)+"\n"), fileMode); err != nil {
		t.Fatalf("seed live file: %v", err)
	}

	w, err := NewRotatingWriter(dir, 48, 3, nil)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	if _, err := fmt.Fprintln(w, strings.Repeat("y", 20)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".1")); err != nil {
		t.Errorf("the adopted size did not trigger rotation: %v", err)
	}
}

// TestRotatingWriter_FailureIsReportedOnceAndNotFatal pins the robustness
// rule: when the sink cannot be reopened, Write returns an error (which slog
// discards) and the reporter hears about it ONCE per failure episode rather
// than on every record.
func TestRotatingWriter_FailureIsReportedOnceAndNotFatal(t *testing.T) {
	t.Parallel()

	var reported []error
	w, _, shutdown := newShadowedWriter(t, func(err error) { reported = append(reported, err) })
	defer shutdown()
	defer w.Close()

	if _, err := w.Write([]byte("first\n")); err == nil {
		t.Error("write: want an error once the sink cannot reopen")
	}
	if _, err := w.Write([]byte("second\n")); err == nil {
		t.Error("second write: want an error")
	}
	if len(reported) != 1 {
		t.Errorf("reported %d failures, want exactly 1 (once per episode)", len(reported))
	}
}

// TestRotatingWriter_FailureWithNilReporterIsSafe pins that the reporter is
// optional: a sink that cannot reopen must still return cleanly instead of
// panicking or exiting.
func TestRotatingWriter_FailureWithNilReporterIsSafe(t *testing.T) {
	t.Parallel()

	w, _, shutdown := newShadowedWriter(t, nil)
	defer shutdown()
	defer w.Close()

	if _, err := w.Write([]byte("hello\n")); err == nil {
		t.Error("write: want an error once the sink cannot reopen")
	}
}

// newShadowedWriter returns a writer whose sink has been made unreopenable:
// the live file and its directory are replaced by a regular file of the same
// name, so every later write fails to reopen. The caller closes it.
func newShadowedWriter(t *testing.T, report func(error)) (*RotatingWriter, string, func()) {
	t.Helper()

	dir := t.TempDir()
	w, err := NewRotatingWriter(dir, 1024, 3, report)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), fileMode); err != nil {
		t.Fatalf("shadow dir with a file: %v", err)
	}
	return w, dir, func() { _ = os.Remove(dir) }
}

// logFileNames lists the sink files newest first, ignoring anything else.
func logFileNames(t *testing.T, dir string, maxFiles int) []string {
	t.Helper()
	names := make([]string, 0, maxFiles)
	for n := range maxFiles {
		name := FileName
		if n > 0 {
			name = fmt.Sprintf("%s.%d", FileName, n)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			names = append(names, name)
		}
	}
	return names
}

// decodeRecords parses one file's records, failing the test when rotation
// split a record in half.
func decodeRecords(t *testing.T, name string, data []byte) []int {
	t.Helper()
	var out []int
	for line := range strings.Lines(strings.TrimSuffix(string(data), "\n")) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec struct{ N int }
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("%s: a record was split by rotation: %q (%v)", name, line, err)
		}
		out = append(out, rec.N)
	}
	return out
}

// readN decodes the "n" field of every record in one sink file, in file order.
func readN(t *testing.T, path string) []int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return decodeRecords(t, filepath.Base(path), data)
}
