package migration

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Bounds on the source shape. maxLeadingWhitespace caps the whitespace
// skipped before top-level shape detection, and maxSourceLineBytes caps one
// JSON-lines record (the array path decodes in memory without a line bound).
const (
	maxLeadingWhitespace = 4096
	maxSourceLineBytes   = 1 << 20
)

// readLegacyRecords reads a legacy export file that is either a JSON array
// of documents (the shape the legacy database's export produced, as seen in
// the local production copy) or JSON-lines, decoding each record as T.
// Leading whitespace is tolerated up to maxLeadingWhitespace bytes so a
// pretty-printed array routes correctly; a JSONL record line is bounded by
// maxSourceLineBytes (an over-bound line aborts the read with the bound
// named — it is a malformed source, not an importable record). The bound is
// exact for LF-terminated input (the legacy exports); a CRLF line of exactly
// the bound loses one byte to the CR. Note: the array path decodes the whole
// array in memory; only the JSON-lines path streams record by record.
func readLegacyRecords[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("migration: open source file: %w", err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, maxLeadingWhitespace+1)

	// Skip leading whitespace (bounded) before detecting the top-level
	// shape, so a pretty-printed export array still routes to the array path.
	var first byte
	for skipped := 0; ; skipped++ {
		if skipped > maxLeadingWhitespace {
			return nil, errors.New("migration: source file starts with excessive whitespace")
		}
		b, err := br.Peek(skipped + 1)
		if errors.Is(err, io.EOF) {
			return nil, nil // an empty/whitespace-only source is valid — nothing to import
		}
		if err != nil {
			return nil, fmt.Errorf("migration: read source file: %w", err)
		}
		c := b[skipped]
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			first = c
			break
		}
	}
	if first == '[' {
		var records []T
		if err := json.NewDecoder(br).Decode(&records); err != nil {
			return nil, fmt.Errorf("migration: decode source array: %w", err)
		}
		return records, nil
	}

	// JSON-lines: one document per line. The scanner buffer is one byte over
	// maxSourceLineBytes so a line of exactly the bound can still terminate
	// with its newline; the next byte over trips ErrTooLong.
	var records []T
	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 64*1024), maxSourceLineBytes+1)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var rec T
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("migration: decode source record: %w", err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("migration: source line exceeds the %d-byte bound", maxSourceLineBytes)
		}
		return nil, fmt.Errorf("migration: read source lines: %w", err)
	}
	return records, nil
}

// ReadLegacyBlogPostsFile reads a legacy blog-post export file.
func ReadLegacyBlogPostsFile(path string) ([]LegacyBlogPost, error) {
	return readLegacyRecords[LegacyBlogPost](path)
}

// ReadLegacyProjectsFile reads a legacy project export file.
func ReadLegacyProjectsFile(path string) ([]LegacyProject, error) {
	return readLegacyRecords[LegacyProject](path)
}

// ReadLegacyUsersFile reads a legacy user export file.
func ReadLegacyUsersFile(path string) ([]LegacyUser, error) {
	return readLegacyRecords[LegacyUser](path)
}
