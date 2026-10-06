package database

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"
)

// VerifySchema checks that the live schema matches schemaSpec exactly:
// every expected table exists as STRICT with the declared columns
// (types/nullability/primary keys), every expected index exists with the
// declared shape, and no unexpected tables, columns, or indexes are present.
// Internal SQLite objects (sqlite_*) are ignored.
//
// It is the structural half of readiness and runs after
// every explicit migration (docs/patterns/go/sqlite.md). It is NOT an
// integrity scan — data-dependent checks (foreign_key_check, quick_check) run
// elsewhere.
func VerifySchema(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return verifySchemaCtx(ctx, db)
}

// verifySchemaCtx is VerifySchema under the caller's context — the readiness
// probe threads its own bounded context here.
func verifySchemaCtx(ctx context.Context, db *sql.DB) error {
	tables, err := readTableList(ctx, db)
	if err != nil {
		return err
	}

	for name, spec := range schemaSpec {
		info, ok := tables[name]
		if !ok {
			return fmt.Errorf("schema: expected table %q not found", name)
		}
		if spec.fts {
			// FTS5 virtual tables are not STRICT and expose hidden
			// bookkeeping columns; presence + virtual-ness is the contract.
			if !info.virtual {
				return fmt.Errorf("schema: table %q is not a virtual FTS5 table", name)
			}
			continue
		}
		if info.virtual {
			return fmt.Errorf("schema: table %q is virtual but expected STRICT", name)
		}
		if !info.strict {
			return fmt.Errorf("schema: table %q is not STRICT", name)
		}
		if err := verifyTableColumns(ctx, db, name, spec); err != nil {
			return err
		}
		if err := verifyIndexes(ctx, db, name, spec); err != nil {
			return err
		}
	}

	// Reject unexpected tables (SQLite-internal sqlite_% tables are allowed).
	for name := range tables {
		if _, expected := schemaSpec[name]; expected {
			continue
		}
		if strings.HasPrefix(name, "sqlite_") {
			continue
		}
		return fmt.Errorf("schema: unexpected table %q", name)
	}

	return nil
}

// tableInfo is the subset of PRAGMA table_list we validate.
type tableInfo struct {
	strict  bool
	virtual bool // type "virtual" = an FTS5 virtual table
}

// readTableList returns every user table with its STRICT status.
func readTableList(ctx context.Context, db *sql.DB) (map[string]tableInfo, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_list")
	if err != nil {
		return nil, fmt.Errorf("schema: query table_list: %w", err)
	}
	defer rows.Close()

	tables := make(map[string]tableInfo)
	for rows.Next() {
		// Columns: schema, name, type, ncol, wr, strict.
		var schema, name, typ string
		var ncol, wr, strict int
		if err := rows.Scan(&schema, &name, &typ, &ncol, &wr, &strict); err != nil {
			return nil, fmt.Errorf("schema: scan table_list row: %w", err)
		}
		switch typ {
		case "table":
			tables[name] = tableInfo{strict: strict == 1}
		case "virtual":
			// FTS5 virtual tables and their "shadow" aux tables appear as
			// virtual/shadow respectively; shadow tables are intentionally
			// NOT captured — SQLite owns their shape.
			tables[name] = tableInfo{virtual: true}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schema: iterate table_list: %w", err)
	}
	return tables, nil
}

// verifyTableColumns checks one table's columns against the spec using
// PRAGMA table_xinfo (cid, name, type, notnull, dflt_value, pk, hidden).
func verifyTableColumns(ctx context.Context, db *sql.DB, table string, spec tableSpec) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_xinfo('%s')", table))
	if err != nil {
		return fmt.Errorf("schema: table %q: query table_xinfo: %w", table, err)
	}
	defer rows.Close()

	found := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, pk, hidden int
		var name, colType string
		var dfltValue *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk, &hidden); err != nil {
			return fmt.Errorf("schema: table %q: scan table_xinfo row: %w", table, err)
		}
		// Hidden columns are engine internals (e.g. rowid) — not part of the contract.
		if hidden != 0 {
			continue
		}
		exp, ok := spec.columns[name]
		if !ok {
			return fmt.Errorf("schema: table %q: unexpected column %q", table, name)
		}
		if colType != exp.typ {
			return fmt.Errorf("schema: table %q column %q: expected type %q, got %q", table, name, exp.typ, colType)
		}
		// PRIMARY KEY columns report notnull=0 (NOT NULL is implicit) — skip.
		if !exp.pk && (notNull != 0) != exp.notNull {
			return fmt.Errorf("schema: table %q column %q: expected NOT NULL=%v, got %v", table, name, exp.notNull, notNull != 0)
		}
		if (pk != 0) != exp.pk {
			return fmt.Errorf("schema: table %q column %q: expected PK=%v, got %v", table, name, exp.pk, pk != 0)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("schema: table %q: iterate table_xinfo: %w", table, err)
	}

	for name := range spec.columns {
		if !found[name] {
			return fmt.Errorf("schema: table %q: missing expected column %q", table, name)
		}
	}
	return nil
}

// foundIndex is one live index of a table.
type foundIndex struct {
	name    string
	unique  bool
	origin  string
	partial bool
	columns []string
}

// verifyIndexes checks one table's indexes against the spec using
// PRAGMA index_list + PRAGMA index_xinfo. Declared indexes match by name;
// auto-indexes (PK and UNIQUE constraints) match by origin + uniqueness +
// column set because their generated names are not part of the contract.
func verifyIndexes(ctx context.Context, db *sql.DB, table string, spec tableSpec) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_list('%s')", table))
	if err != nil {
		return fmt.Errorf("schema: table %q: query index_list: %w", table, err)
	}

	type listedIndex struct {
		name    string
		unique  bool
		origin  string
		partial bool
	}
	var listed []listedIndex
	for rows.Next() {
		// Columns: seq, name, unique, origin, partial.
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return fmt.Errorf("schema: table %q: scan index_list row: %w", table, err)
		}
		listed = append(listed, listedIndex{name: name, unique: unique == 1, origin: origin, partial: partial == 1})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("schema: table %q: iterate index_list: %w", table, err)
	}
	rows.Close()

	// Read each index's columns.
	found := make([]foundIndex, 0, len(listed))
	for _, li := range listed {
		cols, err := readIndexColumns(ctx, db, li.name)
		if err != nil {
			return err
		}
		found = append(found, foundIndex{name: li.name, unique: li.unique, origin: li.origin, partial: li.partial, columns: cols})
	}

	// Every expected index must exist with the expected shape. For a
	// rowid-alias PK there is no index entry to match — the pk flag on the
	// table_xinfo columns already pins the primary key.
	for _, exp := range spec.indexes {
		if exp.origin == "pk" && spec.rowidAlias {
			continue
		}
		if !indexMatches(found, exp) {
			return fmt.Errorf("schema: table %q: expected index %s%s%v not found",
				table, exp.name, exp.origin, exp.columns)
		}
	}

	// Every live index must be expected. A partial index is only ever
	// satisfied by an expectation that declares partial — an undeclared
	// partial index fails as "unexpected" (migration 0010 declares the one
	// accepted partial index).
	for _, fi := range found {
		if indexExpected(spec.indexes, fi) {
			continue
		}
		return fmt.Errorf("schema: table %q: unexpected index %q (origin %q, partial %v)", table, fi.name, fi.origin, fi.partial)
	}

	return nil
}

// readIndexColumns returns the key columns of one index in order, with a
// " DESC" suffix on descending columns (SQLite's index_xinfo desc flag) so
// direction drift fails verification.
func readIndexColumns(ctx context.Context, db *sql.DB, index string) ([]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_xinfo('%s')", index))
	if err != nil {
		return nil, fmt.Errorf("schema: index %q: query index_xinfo: %w", index, err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		// Columns: seqno, cid, name, desc, coll, key.
		var seqno, cid, desc, key int
		var name *string
		var coll *string
		if err := rows.Scan(&seqno, &cid, &name, &desc, &coll, &key); err != nil {
			return nil, fmt.Errorf("schema: index %q: scan index_xinfo row: %w", index, err)
		}
		// Rowid-table primary-key indexes list the implicit rowid alias as a
		// trailing NULL-named entry — only real columns are part of the contract.
		if name != nil {
			if desc == 1 {
				cols = append(cols, *name+" DESC")
			} else {
				cols = append(cols, *name)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schema: index %q: iterate index_xinfo: %w", index, err)
	}
	return cols, nil
}

// indexMatches reports whether a live index satisfies an expectation.
func indexMatches(found []foundIndex, exp indexSpec) bool {
	for _, fi := range found {
		if indexSatisfies(fi, exp) {
			return true
		}
	}
	return false
}

// indexExpected reports whether a live index matches any expectation.
func indexExpected(expected []indexSpec, fi foundIndex) bool {
	for _, exp := range expected {
		if indexSatisfies(fi, exp) {
			return true
		}
	}
	return false
}

// indexSatisfies is the single match predicate both directions share.
func indexSatisfies(fi foundIndex, exp indexSpec) bool {
	if exp.origin != "" && fi.origin != exp.origin {
		return false
	}
	if fi.unique != exp.unique {
		return false
	}
	if fi.partial != exp.partial {
		return false
	}
	if exp.name != "" && fi.name != exp.name {
		return false
	}
	return slices.Equal(fi.columns, exp.columns)
}

// VerifyForeignKeys requires zero rows from PRAGMA foreign_key_check.
//
// This is a data-dependent relation check, deliberately NOT part of the
// readiness path (docs/patterns/go/sqlite.md: readiness does not run integrity
// scans). It runs after explicit migration operations and will run in the
// restore flow.
func VerifyForeignKeys(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&count); err != nil {
		return fmt.Errorf("schema: foreign key check: %w", err)
	}
	if count != 0 {
		return fmt.Errorf("schema: foreign key check found %d violating rows", count)
	}
	return nil
}
