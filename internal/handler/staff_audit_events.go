package handler

import (
	"database/sql"
	"net/http"
	"slices"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// Super-admin audit browser: GET
// /api/v1/staff/audit-events — the last N audit-ledger rows, optionally
// narrowed to one event kind.
//
// This is the full-audit viewer: the forensic fields — request ID, client
// address, actor and target — ARE returned here. The notifications feed
// deliberately withholds them; the floor here is super-admin, and the ledger is
// the place staff look after an incident.
//
// Like the logs viewer it is a bounded TAIL read over an append-only table,
// so the envelope is items-only: no after, no pageInfo, no total, and createdAt
// descending with the id as tie-breaker is a total order. The precise wire
// contract lives in docs/api/openapi.yaml (operationId getStaffAuditEvents).

const (
	// The tail size, matching the dashboard's other staff reads: an operator
	// scanning recent activity wants more than the default page, and the
	// audit table is bounded by its 365-day retention. The default/maximum
	// pair is the accepted tail-read exception.
	defaultStaffAuditLimit = 50
	maxStaffAuditLimit     = 200
)

// staffAuditEvent is one ledger row on the wire. The identity fields are
// omitted when NULL (an unknown actor, an event without a target) rather than
// sent as empty strings — the store already maps them, and the client renders
// only what arrived (the staff-user email precedent).
type staffAuditEvent struct {
	ID         string `json:"id"`
	Event      string `json:"event"`
	Result     string `json:"result"`
	ActorID    string `json:"actorId,omitempty"`
	TargetID   string `json:"targetId,omitempty"`
	TargetRole string `json:"targetRole,omitempty"`
	RequestID  string `json:"requestId"`
	RemoteAddr string `json:"remoteAddr"`
	CreatedAt  string `json:"createdAt"`
}

// staffAuditEvents is the GET /api/v1/staff/audit-events response body.
// Items is always present — an empty ledger answers `{"items":[]}`.
type staffAuditEvents struct {
	Items []staffAuditEvent `json:"items"`
}

// StaffAuditEvents returns the handler for GET /api/v1/staff/audit-events.
//
// Outcomes:
//   - 200 with the newest matching rows for an authenticated super-admin.
//   - 401 generic problem for an unauthenticated request.
//   - 403 for any authenticated role below super-admin.
//   - 400 for an unknown/duplicated query parameter, a malformed query string,
//     or a non-integer `limit`.
//   - 422 for an unknown `event` or an out-of-range `limit`.
//   - 500 generic problem if the read fails.
//
// The floor check runs BEFORE the query is parsed and before any store call.
func StaffAuditEvents(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "staff audit events requires authentication")
		if !ok {
			return
		}
		if !identity.CanViewLogs(su.Role) {
			writeForbidden(w, r, "staff audit events requires super-admin")
			return
		}

		query, ok := strictQueryParams(w, r, "event", "limit")
		if !ok {
			return
		}

		event := query.Get("event")
		if event != "" && !slices.Contains(audit.EventNames(), event) {
			writeValidationErrors(w, r, []Violation{{Field: "event", Code: "invalidValue"}})
			return
		}

		limit, ok := parseLimitParam(w, r, query, defaultStaffAuditLimit, maxStaffAuditLimit)
		if !ok {
			return
		}

		rows, err := store.ListAuditEvents(r.Context(), db, store.AuditEventFilter{
			Event: event,
			Limit: limit,
		})
		if err != nil {
			writeInternalError(w, r, logger, "staff audit events read failed", "error", err)
			return
		}

		items := make([]staffAuditEvent, 0, len(rows))
		for _, row := range rows {
			items = append(items, staffAuditEvent{
				ID:         row.ID,
				Event:      row.Event,
				Result:     row.Result,
				ActorID:    row.ActorID,
				TargetID:   row.TargetID,
				TargetRole: row.TargetRole,
				RequestID:  row.RequestID,
				RemoteAddr: row.RemoteAddr,
				CreatedAt:  formatAPITime(row.CreatedAtMS),
			})
		}

		writeJSON(w, r, http.StatusOK, staffAuditEvents{Items: items})
	}
}
