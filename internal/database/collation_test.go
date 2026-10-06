package database

import (
	"context"
	"testing"

	"sick-fansubs/internal/search"
)

// TestSearchCollation_RegisteredBeforeOpen pins the registration the search
// title sort depends on: the driver only
// applies registered collations to connections opened AFTER registration, so
// a pool opened by this package must already know sf_greek. This is the
// canary for a rename or a moved registration — without it the failure would
// surface as a runtime "no such collation sequence" in a search query.
func TestSearchCollation_RegisteredBeforeOpen(t *testing.T) {
	t.Parallel()

	db, err := Open(Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// The collation must resolve on a freshly opened connection, and it must
	// order by the search rule (Greek alphabet before Latin) rather than by
	// code point or input order. The fixture keeps every key DISTINCT: equal
	// keys have no defined order in SQLite, so a tie would make this pin
	// depend on sorter stability (the accent-folding ties are pinned by the
	// internal/search unit tests instead).
	rows, err := db.QueryContext(context.Background(),
		`SELECT w FROM (
			SELECT 'Ω' AS w UNION ALL SELECT 'α' UNION ALL SELECT 'B'
		) ORDER BY w COLLATE `+search.CollationGreek)
	if err != nil {
		t.Fatalf("collation %q not usable on a new connection: %v", search.CollationGreek, err)
	}
	defer rows.Close()

	var ordered []rune
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ordered = append(ordered, []rune(w)...)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if got := string(ordered); got != "αΩB" {
		t.Errorf("collated order = %q, want %q", got, "αΩB")
	}
}
