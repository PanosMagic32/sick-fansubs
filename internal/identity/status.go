package identity

// Account status values (the users.status CHECK).
//
// The strings appear verbatim in the staff user list,
// so the constants keep handler/store comparisons honest against the
// schema, the same way role.go's constants do for roles.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// IsValidStatus reports whether status is one of the accepted values.
func IsValidStatus(status string) bool {
	return status == StatusActive || status == StatusSuspended
}
