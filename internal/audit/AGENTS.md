# internal/audit — Agent Navigation

Audit events: dual-write to slog AND the durable `audit_events` table (when a writer is registered).

## Open this file when

- adding or changing audit events anywhere (auth, content, and account events);
- touching the writer contract, the registration point, or the event/record shapes.

## Folder-local conventions

- `audit.Event` and `audit.EventWithTarget` are the ONLY emission paths — both funnel through one internal `emit`; emitting audit events from anywhere else is a violation. Both take the request `context.Context` and log through the request-scoped logger (`logging.From`), so a record inherits `requestId` without being handed one; the durable `Record` still carries `RequestID` explicitly. `Event` (existing call sites unchanged) carries the actor only; `EventWithTarget` carries actor + target + **targetRole** (the role snapshot — every account-operation emitter loads the target anyway; pass "" only for events with no target role; for `role_changed` it is the role AFTER the change). The result/actor contract (incl. no actor on `sign_in_failure`, no events for 422 rejections) lives in the package doc.
- The durable writer is startup-registered via `audit.SetWriter` (nil by default; `cmd/api` registers `store.AuditWriter` once before serving). A writer failure is logged as a distinct "audit write failed" record and NEVER propagated — the audited operation's outcome is unchanged.
- The slog field names are `actorId`/`targetId`/`targetRole`/`remoteAddr`, and the request ID arrives as `requestId` from the request-scoped logger (the durable `Record` stores it); the event record message stays the constant "audit", so one filter selects every event.
- The account event-name constants (`password_reset`, `role_changed`, `user_suspended`, `user_reactivated`, `user_deleted`) live here, and the content events (`content_created/updated/deleted`) and `comment_deleted` (emitted by the comment-delete handler — staff deleting ANOTHER user's comment, actor + the comment's author as target, no target-role snapshot) are `audit.Event` / `audit.EventWithTarget` call sites in the handler package; `comment_deleted` is ledger-only (not among the five feed-visible kinds).
- Self-service reset events: `password_reset_requested` (`audit.Event`, actor-only; success when the email went out, failure on the 500 send path — the auth handler emits it ONLY when the account was identified, never for unknown identifiers) and `password_reset_completed` (`audit.EventWithTarget`, actor == target, role snapshot — the auth reset handler emits it). `password_reset_completed` JOINS `accountSecurityEvents` (the staged-restore reconciliation list — a restore over a newer self-reset must be refused, or the old password revives); `password_reset_requested` stays OUT (no durable state change). Both stay ledger-only in the feed.
- Retention: 365 days — `store.DeleteExpiredAuditEvents` (bounded batches; the loop lives in `cmd/api`'s `cleanupAuditEvents`) runs on the hourly `cmd/api` task; `notification_reads` follows via its FK cascade.
- The browser's vocabulary: `EventNames()` is the accepted-event list the super-admin audit browser validates its `event` filter against, and `eventNames` is the slice every event must join — a new event means adding the constant, the `eventNames` entry, the frontend `AUDIT_EVENTS` list, a Greek label in `el.admin.auditEventNames`, and the OpenAPI enum together. `internal/audit/event_names_pin_test.go` pins the Go list equal to the frontend list AND the OpenAPI enum (the VAPID-key precedent), keeps `accountSecurityEvents` inside `eventNames`, and checks every declared event constant joins `eventNames`; the Greek labels' completeness is pinned by the web suite. The pins cover declarations and copies; emitters must pass these constants — a literal name at a call site is a review matter, not a pinned one.
- The writer is a package-global: tests that register it must run SERIALLY and restore nil (the audit package's tests, the `cmd/api` wiring tests, and the handler's audit-row tests do).

## Authoritative docs

- The event/result contract and what is never logged: the package doc in [audit.go](audit.go).
- The error-mapping and one-record logging rules: [../../docs/patterns/go/errors.md](../../docs/patterns/go/errors.md)
- The staged-restore reconciliation list: [../../docs/patterns/go/sqlite.md](../../docs/patterns/go/sqlite.md) rule 12.
- The wire contract (`/api/v1/staff/audit-events`): [../../docs/api/openapi.yaml](../../docs/api/openapi.yaml)
- Comment and doc rules: [../../docs/patterns/docs-conventions.md](../../docs/patterns/docs-conventions.md)
- Index: [../../docs/README.md](../../docs/README.md)
