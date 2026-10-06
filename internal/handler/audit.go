package handler

import (
	"net/http"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/middleware"
)

// logAuditEvent emits one actor-only audit event for the
// current request: the event name, its result, the actor user ID (empty when
// unknown), the server-generated request ID, and the trusted client address.
//
// It is the ONE emitter for the actor-only shape. The content handlers (blog
// and project administration), the session revoke path, and every Auth call
// site (through the delegating Auth.logAudit method) share it, so a change to
// what an audit record carries cannot reach one surface and miss another.
//
// [logAuditTargetEvent] is the actor+target sibling.
func logAuditEvent(r *http.Request, event, result, userID string) {
	audit.Event(r.Context(), event, result, userID,
		middleware.GetRequestID(r.Context()), middleware.ClientAddr(r))
}

// logAuditTargetEvent emits one actor+target audit event for the current
// request: the event name, its result, the actor and target user IDs, the
// target's role snapshot, and the request identity. It is the ONE emitter
// for the account-administration shape (role changes, suspension, reset,
// deletion, email/verification, comment moderation).
func logAuditTargetEvent(r *http.Request, event, result, actorID, targetID, targetRole string) {
	audit.EventWithTarget(r.Context(), event, result, actorID, targetID, targetRole,
		middleware.GetRequestID(r.Context()), middleware.ClientAddr(r))
}
