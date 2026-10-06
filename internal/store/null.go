package store

import "database/sql"

// The store's null* family: the one place a Go value crosses to or from SQL
// NULL. See: docs/patterns/go/sql-mapping.md

// nullableString maps the empty string to SQL NULL on the write side: an
// absent value is absence, not a zero-length one.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullStringPtr is the pointer read half: NULL stays nil so a JSON
// projection emits null.
func nullStringPtr(ns sql.NullString) *string {
	if ns.Valid {
		s := ns.String
		return &s
	}
	return nil
}

// nullStringValue is the plain-string read half: NULL becomes "".
func nullStringValue(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
