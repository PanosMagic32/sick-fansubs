package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadLegacyBlogPostsFile_LeadingWhitespace pins that a pretty-printed
// export array (leading whitespace) routes to the array path, not the
// JSON-lines path.
func TestReadLegacyBlogPostsFile_LeadingWhitespace(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	path := filepath.Join(dir, "pretty.json")
	content := "  \n\t[{\"_id\":{\"$oid\":\"65b0a1c2d3e4f5a6b7c8d9e0\"},\"title\":\"T\",\"thumbnail\":\"https://example.com/t.jpg\",\"createdAt\":{\"$date\":\"2024-01-10T10:00:00.000Z\"}}]\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	records, err := ReadLegacyBlogPostsFile(path)
	if err != nil {
		t.Fatalf("ReadLegacyBlogPostsFile: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records: got %d, want 1", len(records))
	}
	if records[0].Title != "T" {
		t.Errorf("title: got %q", records[0].Title)
	}
}

// TestReadLegacyBlogPostsFile_LeadingWhitespaceBound pins both edges of the
// leading-whitespace bound: 4096 bytes still route, 4097 reject with the
// intended message (not a bufio buffer error).
func TestReadLegacyBlogPostsFile_LeadingWhitespaceBound(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	record := `[{"_id":{"$oid":"65b0a1c2d3e4f5a6b7c8d9e0"},"title":"T"}]`

	atBound := filepath.Join(dir, "whitespace-at-bound.json")
	if err := os.WriteFile(atBound, []byte(strings.Repeat(" ", maxLeadingWhitespace)+record), 0o600); err != nil {
		t.Fatalf("write at-bound source: %v", err)
	}
	if _, err := ReadLegacyBlogPostsFile(atBound); err != nil {
		t.Errorf("at-bound leading whitespace: %v", err)
	}

	overBound := filepath.Join(dir, "whitespace-over-bound.json")
	if err := os.WriteFile(overBound, []byte(strings.Repeat(" ", maxLeadingWhitespace+1)+record), 0o600); err != nil {
		t.Fatalf("write over-bound source: %v", err)
	}
	_, err := ReadLegacyBlogPostsFile(overBound)
	if err == nil {
		t.Fatal("over-bound leading whitespace: expected rejection")
	}
	if !strings.Contains(err.Error(), "excessive whitespace") {
		t.Errorf("err = %v, want the excessive-whitespace message", err)
	}
}

// TestReadLegacyBlogPostsFile_LineBound pins both edges of the JSONL record
// bound: a line of exactly maxSourceLineBytes decodes, one byte more fails
// with the bound named (not a bare bufio error).
func TestReadLegacyBlogPostsFile_LineBound(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	const prefix, suffix = `{"title":"`, `"}`
	padding := maxSourceLineBytes - len(prefix) - len(suffix)

	atBound := filepath.Join(dir, "line-at-bound.jsonl")
	if err := os.WriteFile(atBound, []byte(prefix+strings.Repeat("a", padding)+suffix+"\n"), 0o600); err != nil {
		t.Fatalf("write at-bound source: %v", err)
	}
	records, err := ReadLegacyBlogPostsFile(atBound)
	if err != nil {
		t.Fatalf("at-bound line: %v", err)
	}
	if len(records) != 1 || len(records[0].Title) != padding {
		t.Errorf("at-bound records = %d, title length = %d, want 1 / %d", len(records), len(records[0].Title), padding)
	}

	overBound := filepath.Join(dir, "line-over-bound.jsonl")
	if err := os.WriteFile(overBound, []byte(prefix+strings.Repeat("a", padding+1)+suffix+"\n"), 0o600); err != nil {
		t.Fatalf("write over-bound source: %v", err)
	}
	_, err = ReadLegacyBlogPostsFile(overBound)
	if err == nil {
		t.Fatal("over-bound line: expected rejection")
	}
	if !strings.Contains(err.Error(), "exceeds the") {
		t.Errorf("err = %v, want the size-bound message", err)
	}
}

// TestReadLegacyBlogPostsFile_EmptySource pins that an empty or
// whitespace-only source is valid (nothing to import), not an error.
func TestReadLegacyBlogPostsFile_EmptySource(t *testing.T) {
	t.Parallel()

	dir := testDataDir(t)
	path := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	records, err := ReadLegacyBlogPostsFile(path)
	if err != nil {
		t.Fatalf("ReadLegacyBlogPostsFile: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("records: got %d, want 0", len(records))
	}
}
