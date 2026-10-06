package database

import (
	"database/sql/driver"
	"fmt"

	sqlite "modernc.org/sqlite"

	"sick-fansubs/internal/search"
)

// The folded-text SQL function lives here for the same reason the collation
// does (collation.go): registration makes this package depend on the text
// rules, and internal/search stays free of driver imports. The driver applies
// a registered function to every connection opened AFTER the registration, so
// this must run before any sql.Open — a package init does.
//
// sf_fold(text) is what makes the staff content filter's title match
// accent- and case-insensitive on the SQL side: the store
// splices the NAME (search.FunctionFold, a package constant) into the query
// and binds one folded needle per query word (search.FilterTokens does the
// split), so both sides of the match fold the same way.
// Pure SQLite cannot do it — `lower()` is ASCII-only and there is no
// built-in diacritic stripping — and a second fold rule in SQL would drift
// from the collation.
//
// A NULL argument returns NULL: every title column the function is used on is
// NOT NULL, so this is defensive, and it keeps the function's shape the
// ordinary SQL one.
func init() {
	if err := sqlite.RegisterDeterministicScalarFunction(
		search.FunctionFold,
		1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			s, ok := args[0].(string)
			if !ok {
				return nil, nil
			}
			return search.Fold(s), nil
		},
	); err != nil {
		// A duplicate name is a programming error (two registrations of one
		// function), so the panic is the correct failure — the alternative is
		// a process whose filter queries fail at runtime with "no such
		// function". The collation's registration documents the same call
		// (the accepted exception to the rule that setup errors return to main).
		panic(fmt.Sprintf("database: register %s: %v", search.FunctionFold, err))
	}
}
