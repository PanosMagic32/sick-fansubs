package database

import (
	"context"
	"testing"

	"sick-fansubs/internal/search"
)

// TestFoldFunction_RegisteredBeforeOpen pins the registration the staff
// content filter depends on: the driver only applies a
// registered function to connections opened AFTER registration, so a pool
// opened by this package must already know sf_fold. This is the canary for a
// rename or a moved registration — without it the failure would surface as a
// runtime "no such function" on every filtered staff list query.
//
// The expectations are the fold rule itself, spelled out: case, Greek
// accents, and final sigma are folded; the ASCII path is untouched.
func TestFoldFunction_RegisteredBeforeOpen(t *testing.T) {
	t.Parallel()

	db, err := Open(Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(),
		`SELECT `+search.FunctionFold+`(?)`, "ΟΔΥΣΣΕΎΣ — ÁbC")
	if err != nil {
		t.Fatalf("function %q not usable on a new connection: %v", search.FunctionFold, err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	var got string
	if err := rows.Scan(&got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if want := "οδυσσευσ — ábc"; got != want {
		t.Errorf("%s(...) = %q, want %q", search.FunctionFold, got, want)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
}

// TestFoldFunction_NullArgument pins the defensive arm: a NULL argument
// answers NULL rather than an empty fold (every title column the function is
// used on is NOT NULL, so this only keeps the SQL shape ordinary).
func TestFoldFunction_NullArgument(t *testing.T) {
	t.Parallel()

	db, err := Open(Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var got *string
	if err := db.QueryRowContext(context.Background(),
		`SELECT `+search.FunctionFold+`(NULL)`).Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != nil {
		t.Errorf("fold(NULL) = %q, want NULL", *got)
	}
}
