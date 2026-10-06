package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// discardLogger returns a silent logger for command tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// decodeLogRecords parses each JSON log line into a generic record, so tests
// assert decoded fields rather than serialized bytes
// (docs/patterns/go/testing.md).
func decodeLogRecords(t *testing.T, body string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}
