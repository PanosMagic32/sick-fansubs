// Package audit emits audit events through a dual-write path: every event is
// a structured slog record AND, when a writer is registered, a durable
// audit_events row. The writer is nil by default; cmd/api registers the SQLite
// writer once at startup before any request is served.
//
// Each event carries the event name, result, the ACTOR user ID (when known),
// the TARGET user ID for account operations (the security-relevant
// subject), the server-generated request ID, and the trusted client address.
//
// Result is "success" when the operation committed and "failure" when the
// sensitive part of the operation was attempted and did not complete (wrong
// current password, internal error). Request-level validation rejections
// (422) do not emit events — the audit trail tracks attempted state changes,
// not form typos. sign_in_failure is the one failure event that deliberately
// carries no actor: a failed sign-in must not disclose which identifier
// exists.
//
// The slog record is emitted first; a failed table write never fails the
// calling operation — it is logged as a distinct "audit write failed" error
// record, so the slog half of the trail always survives.
//
// Retention: 365 days — store.DeleteExpiredAuditEvents runs on the hourly
// cmd/api task.
//
// What is never logged: docs/patterns/go/logging.md rule 8 names the
// categories, and this package carries no exemption.
package audit

import (
	"context"
	"log/slog"
	"reflect"
	"sync/atomic"

	"sick-fansubs/internal/logging"
)

// Event names.
const (
	EventSignInSuccess   = "sign_in_success"
	EventSignInFailure   = "sign_in_failure"
	EventSignOut         = "sign_out"
	EventSignOutAll      = "sign_out_all"
	EventPasswordChanged = "password_changed"

	// Account events: emitted through EventWithTarget — they record the actor
	// and the security-relevant target account.
	EventPasswordReset   = "password_reset"
	EventRoleChanged     = "role_changed"
	EventUserSuspended   = "user_suspended"
	EventUserReactivated = "user_reactivated"
	EventUserDeleted     = "user_deleted"

	// Content events.
	EventContentCreated = "content_created"
	EventContentUpdated = "content_updated"
	EventContentDeleted = "content_deleted"

	// Comment moderation: emitted when a staff
	// member deletes ANOTHER user's comment (actor + the comment's author
	// as target). Ledger-only — not among the five feed-visible account
	// events (the feed's v1 scope is unchanged).
	EventCommentDeleted = "comment_deleted"

	// Self-service password reset:
	//   - EventPasswordResetRequested — a forgot-password request for a
	//     KNOWN account (Event, actor-only; success when the email was
	//     sent, failure on the 500 send path). Requests for unknown
	//     identifiers emit NO event — the identifier is never logged.
	//   - EventPasswordResetCompleted — a successful self-service reset
	//     (EventWithTarget, actor == target). It JOINS accountSecurityEvents
	//     (the reset bumps auth_version and swaps the verifier — the
	//     staged-restore reconciliation must see it, or an older backup
	//     restore would revive the old password), but stays ledger-only in
	//     the feed (the password_changed precedent).
	EventPasswordResetRequested = "password_reset_requested"
	EventPasswordResetCompleted = "password_reset_completed"

	// Self-service email verification:
	//   - EventEmailVerified — a successful verify (EventWithTarget, actor
	//     == target). Ledger-only; NOT among the account-security events:
	//     verification changes no password, role, status, or credential,
	//     so the staged-restore reconciliation has nothing to reconcile.
	EventEmailVerified = "email_verified"

	// Self-service email change:
	//   - EventEmailChanged — EventWithTarget (actor == target), result
	//     success on the committed swap, failure on a wrong current
	//     password (a recovery-reroute attempt). Ledger-only; NOT among
	//     the account-security events (an identifier swap, not a
	//     credential/role/status change). The email VALUE is never
	//     carried by the event — no field exists for it.
	EventEmailChanged = "email_changed"
	// Self-service session list:
	//   - EventSessionRevoked — one of the caller's own sessions was revoked
	//     from the account page (Event, actor-only; success when a row was
	//     actually deleted, failure when the delete was attempted and failed).
	//     No target: Record.TargetID means a target ACCOUNT, and a
	//     self-revocation has none. Deliberately NOT an account-security event
	//     — revoking a session changes no credential, role, or status, and the
	//     staged restore purges every session anyway.
	EventSessionRevoked = "session_revoked"
)

// Result values.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

// accountSecurityEvents are the event names that change durable account
// security state — a password, a role, a status, or the account's existence.
// The staged restore's security reconciliation
// (docs/patterns/go/sqlite.md rule 12) compares the newest of these between
// the live database and the staged backup: restoring over a newer one would
// revive accounts, credentials, or privileges the operator deliberately
// changed after the backup was taken.
var accountSecurityEvents = []string{
	EventPasswordChanged,
	EventPasswordReset,
	EventRoleChanged,
	EventUserSuspended,
	EventUserReactivated,
	EventUserDeleted,
	// Self-service reset: changes the password AND bumps
	// auth_version — restoring over it would revive the old credential.
	EventPasswordResetCompleted,
}

// AccountSecurityEvents returns a fresh copy of the account-security event
// names. The copy keeps the list immutable to callers — the reconciliation
// query's IN-list must never be reorderable or truncatable from outside.
func AccountSecurityEvents() []string {
	return append([]string(nil), accountSecurityEvents...)
}

// eventNames is the declared event vocabulary, in declaration order — the ONE
// list the audit browser's kind filter validates against and the pins compare
// the web and OpenAPI copies to. Adding an event means adding it here; emitter
// discipline is internal/audit/AGENTS.md's.
var eventNames = []string{
	EventSignInSuccess,
	EventSignInFailure,
	EventSignOut,
	EventSignOutAll,
	EventPasswordChanged,
	EventPasswordReset,
	EventRoleChanged,
	EventUserSuspended,
	EventUserReactivated,
	EventUserDeleted,
	EventContentCreated,
	EventContentUpdated,
	EventContentDeleted,
	EventCommentDeleted,
	EventPasswordResetRequested,
	EventPasswordResetCompleted,
	EventEmailVerified,
	EventEmailChanged,
	EventSessionRevoked,
}

// EventNames returns a fresh copy of every accepted event name (the copy
// convention of AccountSecurityEvents). A caller that filters or validates
// against the vocabulary must not be able to reorder or truncate it.
func EventNames() []string {
	return append([]string(nil), eventNames...)
}

// Record is the durable shape of one audit event — the same content emitted
// to slog and handed to the registered writer. ActorID is empty when the
// actor is unknown (sign_in_failure); TargetID is empty for events without a
// security-relevant target account; TargetRole is the target account's role
// SNAPSHOT at emission time (set by account-event emitters, empty otherwise).
type Record struct {
	Event      string
	Result     string
	ActorID    string
	TargetID   string
	TargetRole string
	RequestID  string
	RemoteAddr string
}

// Writer persists one audit record. Implementations must not panic and must
// return an error for any failure — the emitter logs the failure and
// continues, so a broken writer can never change the audited operation's
// outcome.
type Writer interface {
	Write(ctx context.Context, rec Record) error
}

// registered is the optional startup-registered writer:
// nil by default, set once by cmd/api before serving. The atomic pointer
// keeps late registrations in tests race-free against concurrent emitters.
var registered atomic.Pointer[Writer]

// SetWriter registers the durable writer. It is called once at startup,
// before any request is served; setting it later only affects subsequent
// events. Pass nil to disable durable writes (the default). A typed-nil
// writer is treated as nil.
func SetWriter(w Writer) {
	if w == nil {
		registered.Store(nil)
		return
	}
	v := reflect.ValueOf(w)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		registered.Store(nil)
		return
	}
	registered.Store(&w)
}

func writer() Writer {
	if p := registered.Load(); p != nil {
		return *p
	}
	return nil
}

// Event writes one audit event with the given actor through the dual-write
// path (slog first, then the registered writer). actorID, requestID, and
// remoteAddr come from the caller's request context helpers. See:
// internal/audit/AGENTS.md
func Event(ctx context.Context, event, result, actorID, requestID, remoteAddr string) {
	emit(ctx, Record{
		Event:      event,
		Result:     result,
		ActorID:    actorID,
		RequestID:  requestID,
		RemoteAddr: remoteAddr,
	})
}

// EventWithTarget is Event for account operations, adding the target account
// and its role snapshot. See: internal/audit/AGENTS.md
func EventWithTarget(ctx context.Context, event, result, actorID, targetID, targetRole, requestID, remoteAddr string) {
	emit(ctx, Record{
		Event:      event,
		Result:     result,
		ActorID:    actorID,
		TargetID:   targetID,
		TargetRole: targetRole,
		RequestID:  requestID,
		RemoteAddr: remoteAddr,
	})
}

// emit is the single dual-write path: the slog record first, then the
// registered writer (nil by default). A writer failure is logged and never
// propagated — the audit trail must not change the outcome of the audited
// operation.
func emit(ctx context.Context, rec Record) {
	logger := logging.From(ctx)
	attrs := []slog.Attr{
		slog.String("event", rec.Event),
		slog.String("result", rec.Result),
	}
	if rec.ActorID != "" {
		attrs = append(attrs, slog.String("actorId", rec.ActorID))
	}
	if rec.TargetID != "" {
		attrs = append(attrs, slog.String("targetId", rec.TargetID))
	}
	if rec.TargetRole != "" {
		attrs = append(attrs, slog.String("targetRole", rec.TargetRole))
	}
	// remoteAddr is always present: the durable column is NOT NULL and the
	// wire carries the field, so an empty value (the break-glass command) is
	// "no trusted client address", never an absent attribute.
	attrs = append(attrs,
		slog.String("remoteAddr", rec.RemoteAddr),
	)
	logger.LogAttrs(ctx, slog.LevelInfo, "audit", attrs...)

	if w := writer(); w != nil {
		if err := w.Write(ctx, rec); err != nil {
			// A distinct message (not "audit") so log tooling does not
			// mistake the failure notice for an event record.
			logger.LogAttrs(ctx, slog.LevelError, "audit write failed",
				slog.String("event", rec.Event),
				slog.String("error", err.Error()),
			)
		}
	}
}
