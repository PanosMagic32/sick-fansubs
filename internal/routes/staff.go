package routes

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/handler"
)

// Staff registers the staff-only operational endpoints under
// /api/v1/staff.
//
// Routes:
//
//	GET /api/v1/staff/metrics      — live dashboard metrics
//	GET /api/v1/staff/logs         — the rotated log tail
//	GET /api/v1/staff/audit-events — the audit-ledger tail
//
// Chain: RequestID → TrustedOrigin → Session → ForcePasswordChange → CSRF —
// the authenticated chain every staff read uses. CSRF and origin only
// validate unsafe methods, so the GET passes through untouched. No rate
// limit — an authenticated staff read (the staff user list precedent).
//
// The floors are enforced in the handlers: identity.CanViewMetrics (moderator+)
// for the metrics, identity.CanViewLogs (super-admin) for the logs and the audit
// ledger. logDir is the resolved log sink directory (data/logs), not the data
// directory itself — the logging package owns the subdirectory name.
//
// Unknown paths and methods inside the namespace answer the JSON 404 problem,
// never the SPA HTML response (the users precedent).
func Staff(mux *http.ServeMux, db *sql.DB, secure bool, trustedOrigin, logDir string) {

	mux.Handle("GET /api/v1/staff/metrics",
		authenticatedChain(handler.StaffMetrics(db), db, secure, trustedOrigin))

	mux.Handle("GET /api/v1/staff/logs",
		authenticatedChain(handler.StaffLogs(logDir), db, secure, trustedOrigin))

	mux.Handle("GET /api/v1/staff/audit-events",
		authenticatedChain(handler.StaffAuditEvents(db), db, secure, trustedOrigin))

	// The exact root and the subtree are both registered: the exact pattern
	// catches /api/v1/staff itself, which the subtree pattern does not match
	// (mirrors routes.Users).
	mux.Handle("/api/v1/staff", notFound())
	mux.Handle("/api/v1/staff/", notFound())
}
