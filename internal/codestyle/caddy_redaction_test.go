package codestyle

import (
	"path/filepath"
	"strings"
	"testing"
)

var caddyRedactionFields = []string{
	"request>headers>Referer delete",
	"request>headers>Cookie delete",
	"request>headers>X-Csrf-Token delete",
	"request>headers>Authorization delete",
	"resp_headers>Set-Cookie delete",
}

// TestCaddyRedactionFields guards docs/ops/deploy.md rule 3: each Caddy log
// block deletes the credential-bearing request and response fields. Caddy
// matches field names exactly, so the CSRF key is the canonical Go spelling
// X-Csrf-Token; X-CSRF-Token silently matches nothing.
func TestCaddyRedactionFields(t *testing.T) {
	// Probes: a complete block has no missing field; a partial block is
	// flagged before the repository file is scanned.
	good := "log {\n format filter {\n fields {\n" +
		" request>uri query {\n delete token\n }\n" +
		" request>headers>Referer delete\n" +
		" request>headers>Cookie delete\n" +
		" request>headers>X-Csrf-Token delete\n" +
		" request>headers>Authorization delete\n" +
		" resp_headers>Set-Cookie delete\n" +
		" }\n }\n}\n"
	if missing := missingCaddyRedactions(good); len(missing) != 0 {
		t.Fatalf("good probe flagged missing fields: %v", missing)
	}
	if missing := missingCaddyRedactions("log {\n request>headers>Cookie delete\n}\n"); len(missing) == 0 {
		t.Fatal("bad probe was not flagged")
	}

	caddy := readCF(t, filepath.Join(repoRoot(t), "Caddyfile"))
	blocks := caddyLogBlocks(caddy)
	if len(blocks) != 2 {
		t.Fatalf("Caddyfile log blocks = %d, want 2 (docs/ops/deploy.md rule 3)", len(blocks))
	}
	for _, block := range blocks {
		for _, field := range missingCaddyRedactions(block.text) {
			t.Errorf("Caddyfile:%d log block is missing %q (docs/ops/deploy.md rule 3)", block.line, field)
		}
	}
	if before, _, ok := strings.Cut(caddy, "X-CSRF-Token"); ok {
		t.Errorf("Caddyfile:%d uses X-CSRF-Token; Caddy matches the canonical X-Csrf-Token exactly (docs/ops/deploy.md rule 3)",
			1+strings.Count(before, "\n"))
	}
}

func missingCaddyRedactions(block string) []string {
	var missing []string
	for _, field := range caddyRedactionFields {
		if strings.Count(block, field) != 1 {
			missing = append(missing, field)
		}
	}
	if !strings.Contains(block, "request>uri query") || strings.Count(block, "delete token") != 1 {
		missing = append(missing, "request>uri query token")
	}
	return missing
}

type caddyLogBlock struct {
	line int
	text string
}

// caddyLogBlocks returns each top-level "log {" block with its source line,
// including nested blocks, by tracking brace depth.
func caddyLogBlocks(text string) []caddyLogBlock {
	var blocks []caddyLogBlock
	search := 0
	for search < len(text) {
		rel := strings.Index(text[search:], "log {")
		if rel < 0 {
			break
		}
		start := search + rel
		openRel := strings.IndexByte(text[start:], '{')
		if openRel < 0 {
			break
		}
		open := start + openRel
		depth := 0
		end := -1
		for i := open; i < len(text); i++ {
			switch text[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			break
		}
		blocks = append(blocks, caddyLogBlock{
			line: 1 + strings.Count(text[:start], "\n"),
			text: text[start : end+1],
		})
		search = end + 1
	}
	return blocks
}
