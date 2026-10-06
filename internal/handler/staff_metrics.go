package handler

import (
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// Staff dashboard metrics: GET
// /api/v1/staff/metrics — the live community totals plus the last-30-days
// activity, for the dashboard's "Στατιστικά" tab.
//
// The response is a pure read of existing storage: no counter column, no
// summary table, no migration. The precise wire contract lives in
// docs/api/openapi.yaml (operationId getStaffMetrics).
//
// The aggregates are NOT masked per role: the floor is moderator+, and every
// metric describes the community as a whole (the per-role feed
// visibility rule governs the notifications feed, not this
// dashboard).
//
// The route chain is the shared authenticated chain RequestID →
// TrustedOrigin → Session → ForcePasswordChange → CSRF (routes/staff.go);
// CSRF and origin only validate unsafe methods, so the GET passes through
// untouched (the staff-list precedent).

// metricsActivityWindow is the activity window ("last-30-days"): a
// rolling 30×24h duration, not a calendar month. The response echoes the
// opening instant as `since`, so the client states the window honestly
// instead of hardcoding "30 days".
const metricsActivityWindow = 30 * 24 * time.Hour

// staffMetricsRoleCounts is the per-role partition. Keys are camelCase
// (`superAdmin`) while the role VALUE stays the schema string `super-admin`
// — the wire spells JSON keys, not identifiers.
type staffMetricsRoleCounts struct {
	User       int64 `json:"user"`
	Moderator  int64 `json:"moderator"`
	Admin      int64 `json:"admin"`
	SuperAdmin int64 `json:"superAdmin"`
}

// staffMetricsStatusCounts is the per-status partition.
type staffMetricsStatusCounts struct {
	Active    int64 `json:"active"`
	Suspended int64 `json:"suspended"`
}

// staffMetricsUserTotals is the users section of the totals.
type staffMetricsUserTotals struct {
	Total    int64                    `json:"total"`
	ByRole   staffMetricsRoleCounts   `json:"byRole"`
	ByStatus staffMetricsStatusCounts `json:"byStatus"`
}

// staffMetricsContentTotals is one content type's status breakdown. Archived
// ships alongside total/published/draft so total is self-explanatory on
// imported data: the beta carries archived rows, and a total that does not
// equal the sum of its parts would read as a bug.
type staffMetricsContentTotals struct {
	Total     int64 `json:"total"`
	Published int64 `json:"published"`
	Draft     int64 `json:"draft"`
	Archived  int64 `json:"archived"`
}

// staffMetricsTotals is the response's totals section.
type staffMetricsTotals struct {
	Users     staffMetricsUserTotals    `json:"users"`
	BlogPosts staffMetricsContentTotals `json:"blogPosts"`
	Projects  staffMetricsContentTotals `json:"projects"`
	// Comments and Favorites sum both content types (blog posts + projects).
	Comments  int64 `json:"comments"`
	Favorites int64 `json:"favorites"`
}

// staffMetricsActivity is the response's windowed section. Since is the
// window's opening instant in the API time format; the change counts
// cover SUCCESS events only (a blocked attempt is not activity), except
// signInFailures — the event name is itself the failure signal.
type staffMetricsActivity struct {
	Since            string `json:"since"`
	Registrations    int64  `json:"registrations"`
	ActiveUsers      int64  `json:"activeUsers"`
	SignInFailures   int64  `json:"signInFailures"`
	ContentCreated   int64  `json:"contentCreated"`
	ContentUpdated   int64  `json:"contentUpdated"`
	ContentDeleted   int64  `json:"contentDeleted"`
	CommentDeletions int64  `json:"commentDeletions"`
	UsersSuspended   int64  `json:"usersSuspended"`
	UsersReactivated int64  `json:"usersReactivated"`
	UsersDeleted     int64  `json:"usersDeleted"`
	RoleChanges      int64  `json:"roleChanges"`
}

// staffMetrics is the GET /api/v1/staff/metrics response body.
type staffMetrics struct {
	Totals      staffMetricsTotals   `json:"totals"`
	Activity30d staffMetricsActivity `json:"activity30d"`
}

// StaffMetrics returns the handler for GET /api/v1/staff/metrics.
//
// Outcomes:
//   - 200 with the totals + activity sections for an authenticated
//     moderator+.
//   - 401 generic problem for an unauthenticated request.
//   - 403 for an authenticated user (role=user) — the floor is moderator+.
//   - 400 for any query parameter (the strict no-parameters contract) or a
//     malformed query string.
//   - 500 generic problem if either read fails.
//
// The floor check runs BEFORE any store call, so a below-floor request never
// touches the database.
func StaffMetrics(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "staff metrics requires authentication")
		if !ok {
			return
		}
		if !identity.CanViewMetrics(su.Role) {
			writeForbidden(w, r, "staff metrics requires moderator or above")
			return
		}
		if !allowNoQueryParams(w, r) {
			return
		}

		totals, err := store.GetMetricsTotals(r.Context(), db)
		if err != nil {
			writeInternalError(w, r, logger, "staff metrics totals failed", "error", err)
			return
		}

		// The window is computed once, per request: both the SQL boundary and
		// the echoed `since` come from the same instant, so the payload can
		// never describe a different window than the one it counted.
		since := time.Now().Add(-metricsActivityWindow)
		activity, err := store.GetMetricsActivity(r.Context(), db, since.UnixMilli())
		if err != nil {
			writeInternalError(w, r, logger, "staff metrics activity failed", "error", err)
			return
		}

		writeJSON(w, r, http.StatusOK, staffMetrics{
			Totals: staffMetricsTotals{
				Users: staffMetricsUserTotals{
					Total: totals.Users.Total,
					ByRole: staffMetricsRoleCounts{
						User:       totals.Users.RoleUser,
						Moderator:  totals.Users.RoleModerator,
						Admin:      totals.Users.RoleAdmin,
						SuperAdmin: totals.Users.RoleSuperAdmin,
					},
					ByStatus: staffMetricsStatusCounts{
						Active:    totals.Users.StatusActive,
						Suspended: totals.Users.StatusSuspended,
					},
				},
				BlogPosts: staffMetricsContentTotals{
					Total:     totals.BlogPosts.Total,
					Published: totals.BlogPosts.Published,
					Draft:     totals.BlogPosts.Draft,
					Archived:  totals.BlogPosts.Archived,
				},
				Projects: staffMetricsContentTotals{
					Total:     totals.Projects.Total,
					Published: totals.Projects.Published,
					Draft:     totals.Projects.Draft,
					Archived:  totals.Projects.Archived,
				},
				Comments:  totals.Comments,
				Favorites: totals.Favorites,
			},
			Activity30d: staffMetricsActivity{
				Since:            formatAPITime(since.UnixMilli()),
				Registrations:    activity.Registrations,
				ActiveUsers:      activity.ActiveUsers,
				SignInFailures:   activity.SignInFailures,
				ContentCreated:   activity.ContentCreated,
				ContentUpdated:   activity.ContentUpdated,
				ContentDeleted:   activity.ContentDeleted,
				CommentDeletions: activity.CommentDeletions,
				UsersSuspended:   activity.UsersSuspended,
				UsersReactivated: activity.UsersReactivated,
				UsersDeleted:     activity.UsersDeleted,
				RoleChanges:      activity.RoleChanges,
			},
		})
	}
}
