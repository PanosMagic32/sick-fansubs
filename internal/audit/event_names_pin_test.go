package audit

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// The audit vocabulary lives in FOUR places by design: the Go constants, the
// `eventNames` slice (the browser's allowlist), the frontend's AUDIT_EVENTS
// list (the filter options and the wire DTO's type), and the OpenAPI `event`
// enum. A drift means the UI offers a kind the server rejects with a 422, or
// silently omits one it accepts — so every copy is pinned here, the cheap
// place to learn about it (the VAPID-key pin precedent). The Greek labels are
// the fifth copy; their completeness is pinned by the web suite.

// frontendEventsPattern extracts the array literal between the brackets of
// `export const AUDIT_EVENTS = [ ... ] as const`.
var frontendEventsPattern = regexp.MustCompile(`(?s)export const AUDIT_EVENTS\s*=\s*\[(.*?)\]\s*as const;`)

// openAPIEventEnumPattern extracts the enum list of the audit browser's
// `event` query parameter — the first `enum:` after `name: event`.
var openAPIEventEnumPattern = regexp.MustCompile(`(?s)name: event\b.*?enum:\s*\n((?:\s+- [a-z_]+\s*\n)+)`)

// listedEventPattern matches one `- snake_name` entry in an extracted enum.
var listedEventPattern = regexp.MustCompile(`(?m)^\s+- ([a-z_]+)\s*$`)

// eventConstantPattern matches a string-valued event constant declaration.
var eventConstantPattern = regexp.MustCompile(`(?m)^\s*(Event[A-Za-z]+)(?:\s+string)?\s*=\s*"([a-z_]+)"`)

// eventNamesRefPattern matches one Event… identifier inside the eventNames
// slice literal.
var eventNamesRefPattern = regexp.MustCompile(`\b(Event[A-Za-z]+)\b`)

// quotedNamePattern matches one double-quoted lower_snake name in that block.
var quotedNamePattern = regexp.MustCompile(`"([a-z_]+)"`)

// lineCommentPattern strips a trailing `// …` so a COMMENTED-OUT entry cannot
// satisfy the pin (the vapid-pin precedent: a commented-out value must not
// pass as present).
var lineCommentPattern = regexp.MustCompile(`//[^\n]*`)

// assertVocabularyCopy fails when one copy of the vocabulary differs from
// EventNames() in length or order.
func assertVocabularyCopy(t *testing.T, source string, names []string) {
	t.Helper()
	backend := EventNames()
	if len(names) != len(backend) {
		t.Fatalf("%s lists %d events (%v), the backend %d (%v)", source, len(names), names, len(backend), backend)
	}
	for i := range backend {
		if names[i] != backend[i] {
			t.Errorf("%s: event %d is %q, the backend has %q — the copies must stay identical and in the same order", source, i, names[i], backend[i])
		}
	}
}

func TestEventNamesMatchFrontend(t *testing.T) {
	t.Parallel()

	root := auditRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "web", "src", "features", "admin", "data-access", "audit-api.ts"))
	if err != nil {
		t.Fatalf("read the frontend audit vocabulary: %v", err)
	}

	block := frontendEventsPattern.FindSubmatch(data)
	if block == nil {
		t.Fatal("web/src/features/admin/data-access/audit-api.ts must declare `export const AUDIT_EVENTS = [ … ] as const;`")
	}
	body := lineCommentPattern.ReplaceAll(block[1], nil)
	matches := quotedNamePattern.FindAllSubmatch(body, -1)
	frontend := make([]string, 0, len(matches))
	for _, m := range matches {
		frontend = append(frontend, string(m[1]))
	}

	assertVocabularyCopy(t, "the frontend AUDIT_EVENTS list", frontend)
}

// TestEventNamesMatchOpenAPI pins the wire contract's enum: it is
// hand-maintained prose, so a new event that misses it would make a
// validating client omit — or refuse — a kind the server accepts.
func TestEventNamesMatchOpenAPI(t *testing.T) {
	t.Parallel()

	root := auditRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read the wire contract: %v", err)
	}

	if n := bytes.Count(data, []byte("name: event")); n != 1 {
		t.Fatalf("docs/api/openapi.yaml must carry exactly one `name: event` parameter, found %d — the pin's anchor moved", n)
	}

	block := openAPIEventEnumPattern.FindSubmatch(data)
	if block == nil {
		t.Fatal("docs/api/openapi.yaml must carry the audit browser's `event` enum under `name: event`")
	}
	matches := listedEventPattern.FindAllSubmatch(block[1], -1)
	openapi := make([]string, 0, len(matches))
	for _, m := range matches {
		openapi = append(openapi, string(m[1]))
	}

	assertVocabularyCopy(t, "the OpenAPI event enum", openapi)
}

// TestEventConstantsJoinEventNames pins `eventNames`' completeness: every
// string-valued Event constant is listed there, and every identifier in the
// slice names a declared constant — a new event cannot be half-added.
func TestEventConstantsJoinEventNames(t *testing.T) {
	t.Parallel()

	root := auditRepoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "internal", "audit", "audit.go"))
	if err != nil {
		t.Fatalf("read audit.go: %v", err)
	}

	declared := map[string]string{}
	for _, m := range eventConstantPattern.FindAllSubmatch(src, -1) {
		declared[string(m[1])] = string(m[2])
	}
	if len(declared) == 0 {
		t.Fatal("no event constants found — the extraction pattern has drifted")
	}

	const marker = "var eventNames = []string{"
	start := bytes.Index(src, []byte(marker))
	if start < 0 {
		t.Fatalf("audit.go must declare `%s`", marker)
	}
	end := bytes.IndexByte(src[start:], '}')
	if end < 0 {
		t.Fatal("the eventNames slice literal has no closing brace")
	}
	body := src[start : start+end]

	listed := map[string]bool{}
	for _, m := range eventNamesRefPattern.FindAllSubmatch(body, -1) {
		name := string(m[1])
		listed[name] = true
		if _, ok := declared[name]; !ok {
			t.Errorf("eventNames lists %s, which is not a declared string-valued event constant", name)
		}
	}
	for name := range declared {
		if !listed[name] {
			t.Errorf("constant %s is never listed in eventNames — add it there too", name)
		}
	}
}

// TestAccountSecurityEventsAreKnown pins the reconciliation list INSIDE the
// vocabulary: the staged-restore guard filters on accountSecurityEvents, so an
// entry that is not an emittable event would silently narrow it.
func TestAccountSecurityEventsAreKnown(t *testing.T) {
	t.Parallel()

	known := EventNames()
	for _, name := range AccountSecurityEvents() {
		if !slices.Contains(known, name) {
			t.Errorf("account-security event %q is not in EventNames — add it there too", name)
		}
	}
}

// auditRepoRoot walks up to the directory holding go.mod.
func auditRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root (go.mod) not found")
		}
		dir = parent
	}
}
