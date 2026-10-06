package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The feed's account-event vocabulary lives in TWO places by design: the SQL
// clause built from the audit constants, and the frontend's
// `isAccountEvent` guard. That guard decides which rows may render a delete
// control (only the EVENT half is deletable), so a drift
// would render a control the server answers with a masked 404. Pinned here,
// the cheap place to learn about it (the audit event-names pin precedent).

// isAccountEventPattern extracts the body of the frontend guard.
var isAccountEventPattern = regexp.MustCompile(
	`(?s)export function isAccountEvent\([^)]*\): boolean \{(.*?)\n\}`)

// feedKindPattern matches ONE `item.kind === "name"` comparison in that body.
var feedKindPattern = regexp.MustCompile(`item\.kind === "([a-z_]+)"`)

func TestFeedEventsMatchFrontend(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(storeRepoRoot(t),
		"web", "src", "features", "notifications", "data-access", "notifications-api.ts"))
	if err != nil {
		t.Fatalf("read the frontend feed-event guard: %v", err)
	}

	body := isAccountEventPattern.FindSubmatch(data)
	if body == nil {
		t.Fatal("web/src/features/notifications/data-access/notifications-api.ts must declare `export function isAccountEvent(...): boolean { return ( … ); }`")
	}
	frontend := make([]string, 0, len(feedEventNames))
	for _, m := range feedKindPattern.FindAllSubmatch(body[1], -1) {
		frontend = append(frontend, string(m[1]))
	}

	if len(frontend) != len(feedEventNames) {
		t.Fatalf("frontend guards %d events (%v), the store's clause %d (%v)",
			len(frontend), frontend, len(feedEventNames), feedEventNames)
	}
	for i, name := range feedEventNames {
		if frontend[i] != name {
			t.Errorf("event %d: frontend %q, store %q — the two lists must stay identical and in the same order",
				i, frontend[i], name)
		}
	}
}

// TestFeedEventsMatchClause pins the clause against the event set it is built
// from, so the SQL text can never drop an event the guard still hides a
// delete control for (or the reverse).
func TestFeedEventsMatchClause(t *testing.T) {
	t.Parallel()

	for _, name := range feedEventNames {
		if !strings.Contains(feedEventsClause, "'"+name+"'") {
			t.Errorf("feedEventsClause is missing %q: %s", name, feedEventsClause)
		}
	}
}

// storeRepoRoot walks up to the directory holding go.mod.
func storeRepoRoot(t *testing.T) string {
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
