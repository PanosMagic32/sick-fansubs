package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// testDataDir returns an owner-only temporary directory for tests that open a
// database: database.Open refuses a data directory with group or other access,
// and t.TempDir can inherit wider bits from the environment.
func testDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod test data dir: %v", err)
	}
	return dir
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
