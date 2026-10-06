package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"sick-fansubs/internal/logging"
)

// TestFileSinkHandler_DualSink pins the happy path end to end: one record,
// two destinations, the same decoded record, in the directory the viewer
// reads.
func TestFileSinkHandler_DualSink(t *testing.T) {
	t.Parallel()

	dataDir := testDataDir(t)
	var stderr bytes.Buffer
	handler, closeLog, err := fileSinkHandler(dataDir, slog.NewJSONHandler(&stderr, logHandlerOptions))
	if err != nil {
		t.Fatalf("fileSinkHandler: %v", err)
	}
	defer closeLog()

	slog.New(handler).With("requestId", "r1").Warn("a warning")

	stderrRecords := decodeLogRecords(t, stderr.String())
	if len(stderrRecords) != 1 {
		t.Fatalf("stderr records = %d, want 1", len(stderrRecords))
	}
	if got := stderrRecords[0]["msg"]; got != "a warning" {
		t.Errorf("stderr msg = %v, want a warning", got)
	}
	if got := stderrRecords[0]["requestId"]; got != "r1" {
		t.Errorf("stderr requestId = %v, want r1", got)
	}
	if got := stderrRecords[0]["level"]; got != "WARN" {
		t.Errorf("stderr level = %v, want WARN", got)
	}

	path := filepath.Join(dataDir, logging.DirName, logging.FileName)
	fileBody, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the sink file %s: %v", path, err)
	}
	fileRecords := decodeLogRecords(t, string(fileBody))
	if len(fileRecords) != 1 {
		t.Fatalf("sink records = %d, want 1", len(fileRecords))
	}
	if !reflect.DeepEqual(fileRecords[0], stderrRecords[0]) {
		t.Errorf("sinks differ: stderr %v, file %v", stderrRecords[0], fileRecords[0])
	}
}

// TestFileSinkHandler_DegradesToStderr pins the never-fatal rule: with the
// data directory unusable the error is returned, the handler still logs, and
// startup has nothing to abort for.
func TestFileSinkHandler_DegradesToStderr(t *testing.T) {
	t.Parallel()

	// A regular file where the data directory should be: the sink's MkdirAll
	// cannot succeed.
	blocked := filepath.Join(testDataDir(t), "data")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed blocking file: %v", err)
	}

	var stderr bytes.Buffer
	handler, closeLog, err := fileSinkHandler(blocked, slog.NewJSONHandler(&stderr, logHandlerOptions))
	if err == nil {
		t.Fatal("fileSinkHandler() err = nil, want error")
	}
	defer closeLog()

	slog.New(handler).Error("still logging")
	records := decodeLogRecords(t, stderr.String())
	if len(records) != 1 || records[0]["msg"] != "still logging" {
		t.Errorf("stderr records = %v, want the still-logging record", records)
	}
}
