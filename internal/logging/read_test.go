package logging

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Log reader tests: the newest-first order
// across rotated files, the level and substring filters, the cap, and the
// rule that unreadable or malformed content degrades instead of failing.

const testInstant = "2026-09-18T10:00:00.000Z"

// jsonLine builds one JSON-lines record in the shape the stderr/file handler
// writes (time, level, msg, then the extra attributes).
func jsonLine(level, msg string, attrs ...any) string {
	fields := map[string]any{"time": testInstant, "level": level, "msg": msg}
	for i := 0; i+1 < len(attrs); i += 2 {
		key, _ := attrs[i].(string)
		fields[key] = attrs[i+1]
	}
	data, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// writeSinkFile writes raw lines into one sink file (no rotation, so the
// reader's own ordering rules are what is under test).
func writeSinkFile(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), fileMode); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
}

func msgs(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Msg)
	}
	return out
}

func TestParseLevel_Vocabulary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{" Warn ", slog.LevelWarn},
		{"error", slog.LevelError},
	} {
		got, ok := ParseLevel(tc.in)
		if !ok || got != tc.want {
			t.Errorf("ParseLevel(%q) = %v/%v, want %v/true", tc.in, got, ok, tc.want)
		}
	}
	for _, bad := range []string{"", "warning", "trace", "WARN1"} {
		if _, ok := ParseLevel(bad); ok {
			t.Errorf("ParseLevel(%q) accepted an unknown level", bad)
		}
	}
}

// TestRead_MissingDirectoryIsEmpty pins the fresh-install case: no sink yet
// is not an error.
func TestRead_MissingDirectoryIsEmpty(t *testing.T) {
	t.Parallel()

	entries, err := Read(context.Background(), filepath.Join(t.TempDir(), "logs"), Filter{MinLevel: slog.LevelWarn, Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %v, want none", msgs(entries))
	}
}

// TestRead_NewestFirstAcrossFiles pins the order the viewer depends on: the
// records of the live file come first, then the backup, each newest-first.
func TestRead_NewestFirstAcrossFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSinkFile(t, dir, FileName, jsonLine("ERROR", "live-old"), jsonLine("ERROR", "live-new"))
	writeSinkFile(t, dir, FileName+".1", jsonLine("ERROR", "backup-old"), jsonLine("ERROR", "backup-new"))

	entries, err := Read(context.Background(), dir, Filter{MinLevel: slog.LevelWarn, Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []string{"live-new", "live-old", "backup-new", "backup-old"}
	if got := msgs(entries); !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// TestRead_LevelFilterKeepsTheFloorAndAbove pins that `level` is a minimum
// severity, not an equality filter.
func TestRead_LevelFilterKeepsTheFloorAndAbove(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSinkFile(t, dir, FileName,
		jsonLine("DEBUG", "d"), jsonLine("INFO", "i"), jsonLine("WARN", "w"), jsonLine("ERROR", "e"))

	entries, err := Read(context.Background(), dir, Filter{MinLevel: slog.LevelWarn, Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, want := msgs(entries), []string{"e", "w"}; !slices.Equal(got, want) {
		t.Errorf("warn floor = %v, want %v", got, want)
	}

	entries, err = Read(context.Background(), dir, Filter{MinLevel: slog.LevelInfo, Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, want := msgs(entries), []string{"e", "w", "i"}; !slices.Equal(got, want) {
		t.Errorf("info floor = %v, want %v", got, want)
	}
}

// TestRead_SubstringFilterIsCaseInsensitive pins that `q` reaches any part of
// the raw line, including a field value rather than the message.
func TestRead_SubstringFilterIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSinkFile(t, dir, FileName,
		jsonLine("ERROR", "boom", "requestId", "AbC123"),
		jsonLine("ERROR", "quiet"))

	entries, err := Read(context.Background(), dir, Filter{MinLevel: slog.LevelWarn, Query: "abc123", Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, want := msgs(entries), []string{"boom"}; !slices.Equal(got, want) {
		t.Errorf("field substring = %v, want %v", got, want)
	}

	entries, err = Read(context.Background(), dir, Filter{MinLevel: slog.LevelWarn, Query: "QUIET", Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, want := msgs(entries), []string{"quiet"}; !slices.Equal(got, want) {
		t.Errorf("message substring = %v, want %v", got, want)
	}
}

// TestRead_LimitCapsTheResult pins the cap and that it is the newest matches
// that survive it.
func TestRead_LimitCapsTheResult(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSinkFile(t, dir, FileName+".1", jsonLine("ERROR", "oldest"), jsonLine("ERROR", "older"))
	writeSinkFile(t, dir, FileName, jsonLine("ERROR", "newer"), jsonLine("ERROR", "newest"))

	entries, err := Read(context.Background(), dir, Filter{MinLevel: slog.LevelWarn, Limit: 2})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, want := msgs(entries), []string{"newest", "newer"}; !slices.Equal(got, want) {
		t.Errorf("limit 2 = %v, want %v", got, want)
	}

	if entries, err := Read(context.Background(), dir, Filter{MinLevel: slog.LevelWarn, Limit: 0}); err != nil || len(entries) != 0 {
		t.Errorf("limit 0 = %v/%v, want no entries and no error", msgs(entries), err)
	}
}

// TestRead_SkipsMalformedLines pins the degraded-input contract: a truncated
// tail line, foreign JSON, an unknown level, and a missing time are skipped
// while the good records still come back.
func TestRead_SkipsMalformedLines(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSinkFile(t, dir, FileName,
		jsonLine("WARN", "good"),
		`{"time":"2026-09-18T10:00:00.000Z","level":"WARN","msg":"truncated`,
		`not json at all`,
		`{"time":"2026-09-18T10:00:00.000Z","level":"VERBOSE","msg":"unknown level"}`,
		`{"time":"nonsense","level":"WARN","msg":"bad time"}`,
		`{"level":"WARN","msg":"no time"}`,
		`{"time":"2026-09-18T10:00:00.000Z","level":"WARN"}`,
		``,
	)

	entries, err := Read(context.Background(), dir, Filter{MinLevel: slog.LevelWarn, Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got, want := msgs(entries), []string{"good"}; !slices.Equal(got, want) {
		t.Errorf("entries = %v, want only the well-formed record %v", got, want)
	}
}

// TestRead_ExtractsFields pins the wire mapping: time and level are parsed,
// the message is the message, and every other attribute survives untouched.
func TestRead_ExtractsFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSinkFile(t, dir, FileName,
		jsonLine("WARN", "flaky", "requestId", "r1", "attempt", 3),
		jsonLine("WARN", "bare"))

	entries, err := Read(context.Background(), dir, Filter{MinLevel: slog.LevelWarn, Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want 2", msgs(entries))
	}

	first := entries[0]
	if first.Msg != "bare" {
		t.Fatalf("newest entry = %q, want %q", first.Msg, "bare")
	}
	if first.Level != "warn" {
		t.Errorf("Level = %q, want the lower-cased level", first.Level)
	}
	if first.Time.UTC().Format("2006-01-02T15:04:05Z") != "2026-09-18T10:00:00Z" {
		t.Errorf("Time = %s, want the record's instant", first.Time)
	}
	if len(first.Fields) != 0 {
		t.Errorf("Fields = %v, want none for a record with no attributes", first.Fields)
	}

	withAttrs := entries[1]
	if withAttrs.Fields["requestId"] != "r1" {
		t.Errorf("Fields[requestId] = %v, want r1", withAttrs.Fields["requestId"])
	}
	if withAttrs.Fields["attempt"] != float64(3) {
		t.Errorf("Fields[attempt] = %v (%T), want 3", withAttrs.Fields["attempt"], withAttrs.Fields["attempt"])
	}
	for _, reserved := range []string{"time", "level", "msg"} {
		if _, present := withAttrs.Fields[reserved]; present {
			t.Errorf("Fields still carries the reserved key %q", reserved)
		}
	}
}

// TestRead_TailOfOversizedFile pins the read cap: a file larger than
// maxFileReadBytes is read from its tail, so an oversized file left behind by
// an earlier process cannot turn a viewer request into an unbounded read.
func TestRead_TailOfOversizedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pad := strings.Repeat("x", 900)

	// The first record is the discriminator: it sits far outside the read cap,
	// so asking for EVERY record must still not return it. (A limit of 1 would
	// pass with or without the cap, which is why the earlier version of this
	// test could not fail.)
	var b strings.Builder
	b.WriteString(jsonLine("ERROR", "head marker", "blob", pad))
	b.WriteString("\n")
	total := 1
	for b.Len() <= maxFileReadBytes {
		total++
		b.WriteString(jsonLine("ERROR", "record", "i", total, "blob", pad))
		b.WriteString("\n")
	}
	b.WriteString(jsonLine("ERROR", "tail"))
	b.WriteString("\n")
	total++

	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(b.String()), fileMode); err != nil {
		t.Fatalf("seed oversized file: %v", err)
	}

	entries, err := Read(context.Background(), dir, Filter{MinLevel: slog.LevelWarn, Limit: total})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if entries[0].Msg != "tail" {
		t.Errorf("newest entry = %q, want the last record in the file", entries[0].Msg)
	}
	if len(entries) >= total {
		t.Errorf("entries = %d of %d, want fewer: the read is capped at %d bytes", len(entries), total, maxFileReadBytes)
	}
	if slices.Contains(msgs(entries), "head marker") {
		t.Error("the record outside the read window was returned — the cap is not applied")
	}
}
