package logging

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// maxFileReadBytes caps how much of ONE file the reader looks at. The writer
// rotates well below this (5 MB), so the cap only matters for a file an
// earlier process left oversized; reading the tail is what the viewer wants
// anyway.
const maxFileReadBytes = 8 << 20

// Entry is one parsed log record as the viewer sees it.
type Entry struct {
	// Time is the record's instant (slog's own "time" attribute).
	Time time.Time
	// Level is the canonical lower-cased level name — the same vocabulary
	// ParseLevel accepts, so a stored line and a query value are always
	// spelled the same way and the wire can never carry a stray spelling.
	Level string
	// Msg is the record's message.
	Msg string
	// Fields is every remaining attribute, untouched, as written by slog.
	// Never nil: a record with no attributes carries an empty map, so the
	// wire always has an object to render.
	Fields map[string]any
}

// Filter is the viewer's query: a minimum level, an optional case-insensitive
// substring over the raw line, and a hard cap on how many matches to return.
type Filter struct {
	// MinLevel keeps records at this level or above.
	MinLevel slog.Level
	// Query is matched case-insensitively against the whole raw line, so it
	// reaches field values as well as the message. Empty means no filter.
	Query string
	// Limit is the maximum number of entries returned. Must be positive.
	Limit int
}

// ParseLevel maps the viewer's level vocabulary — debug, info, warn, error —
// to its slog level. It is the ONE place the spelling is defined, so a query
// value and a stored line can never drift apart.
func ParseLevel(name string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return 0, false
	}
}

// LevelName is ParseLevel's inverse: the canonical spelling of a parsed level,
// so Entry.Level is derived from the level rather than echoed from the line
// (a foreign line spelling " warn" must not reach the wire).
func LevelName(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "error"
	case level >= slog.LevelWarn:
		return "warn"
	case level >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// Read returns the newest log entries under dir, newest first, filtered and
// capped by f. Files are read newest → oldest (the live file, then .1, .2,
// …), and each is scanned from its last line backwards, so the cap stops the
// work as early as the answer allows. ctx bounds the scan: a superseded
// request stops rather than reading the whole sink.
//
// A missing directory or a missing file is not an error: a fresh install has
// no log file yet and answers an empty list. A line that does not parse is
// skipped — the tail line of a file is a partial write whenever the process
// was killed mid-record.
func Read(ctx context.Context, dir string, f Filter) ([]Entry, error) {
	entries := make([]Entry, 0, min(max(f.Limit, 0), 64))
	if f.Limit <= 0 {
		return entries, nil
	}
	query := strings.ToLower(f.Query)

	for _, path := range readPaths(dir) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lines, err := readLines(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, raw := range slices.Backward(lines) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			entry, rank, ok := parseLine(raw)
			if !ok || rank < f.MinLevel {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(raw), query) {
				continue
			}
			entries = append(entries, entry)
			if len(entries) == f.Limit {
				return entries, nil
			}
		}
	}
	return entries, nil
}

// readPaths lists the sink files newest first. The count comes from
// DefaultMaxFiles, the same constant the writer rotates with, so the reader
// can never walk past what the sink produces.
func readPaths(dir string) []string {
	live := filepath.Join(dir, FileName)
	paths := make([]string, 0, DefaultMaxFiles)
	paths = append(paths, live)
	for n := 1; n < DefaultMaxFiles; n++ {
		paths = append(paths, live+"."+strconv.Itoa(n))
	}
	return paths
}

// readLines reads a file's lines, keeping only its tail when the file is
// larger than maxFileReadBytes.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var reader io.Reader = f
	if info.Size() > maxFileReadBytes {
		if _, err := f.Seek(-maxFileReadBytes, io.SeekEnd); err != nil {
			return nil, err
		}
		reader = io.LimitReader(f, maxFileReadBytes)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

// parseLine decodes one JSON-lines record. A line missing a parseable time,
// a known level, or a message is rejected: the writer always emits all three,
// so anything else is a partial write or foreign content.
func parseLine(raw string) (Entry, slog.Level, bool) {
	if strings.TrimSpace(raw) == "" {
		return Entry{}, 0, false
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return Entry{}, 0, false
	}

	rank, ok := ParseLevel(anyString(fields["level"]))
	if !ok {
		return Entry{}, 0, false
	}
	msg, ok := fields["msg"].(string)
	if !ok {
		return Entry{}, 0, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, anyString(fields["time"]))
	if err != nil {
		return Entry{}, 0, false
	}

	delete(fields, "time")
	delete(fields, "level")
	delete(fields, "msg")
	if fields == nil {
		fields = map[string]any{}
	}

	return Entry{
		Time:   parsed,
		Level:  LevelName(rank),
		Msg:    msg,
		Fields: fields,
	}, rank, true
}

// anyString reads a JSON string field, tolerating any other type: a foreign
// line must be rejected by the caller, not panic here.
func anyString(v any) string {
	s, _ := v.(string)
	return s
}
