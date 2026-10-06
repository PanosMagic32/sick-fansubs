package database

import (
	sqlite "modernc.org/sqlite"

	"sick-fansubs/internal/search"
)

// The Greek search collation is registered here, in the package that owns
// the driver, rather than in internal/search: registration makes this
// package depend on the text rules, never the other way round (search stays
// free of database and driver imports). The driver applies registered
// collations to every connection opened after registration, so this must
// run before any sql.Open — a package init does.
func init() {
	// A duplicate name is a programming error (two registrations of one
	// collation), so the panic is the correct failure: the alternative is a
	// process whose sort queries fail at runtime with "no such collation
	// sequence".
	sqlite.MustRegisterCollationUtf8(search.CollationGreek, search.Compare)
}
