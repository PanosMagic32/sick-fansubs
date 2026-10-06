# internal/identity — Agent Navigation

Domain values shared across layers: session identities (`SessionUser`), public projections (`PublicUser`), and the role model (`role.go`). No connections, no HTTP.

## Open this file when

- changing session/user shapes, expiry semantics, what the API exposes about a user, or role values/capability checks.

## Folder-local conventions

- Keep one struct per role: never force the legacy import shape, the SQLite row, the business value, and the public JSON into one type.
- `PublicUser` is the wire-safe projection — password hashes, session internals, and CSRF material never appear in it.
- `role.go`: role constants mirror the `users.role` CHECK; `RoleWeight` feeds the draft-visibility gate (strict inequality); capability checks are explicit per operation — the role strings are the API's public projection, so the constants keep the handler, store, and auth comparisons honest. `IsValidRole` accepts exactly the four role constants. `CanCreateContent` (admin+) and `CanDeleteContent` (admin+ — moderators edit only; the moderator-delete option stays open for a later ruling) cover create and hard delete; `CanModerateContent` (moderator+) covers the staff content reads, content updates, and staff comment deletion. `CanUploadMedia` (admin+) and `CanDeleteMedia` (admin+ — the same media-lifecycle floor) cover the media surfaces. `CanViewStaffList` (moderator+) and `CanViewStaffEmails` (admin+ — a below-floor staff list OMITS the email key) are the staff-list floors. `CanResetPassword(actor, target)`: super-admins reset anyone incl. self; admins reset moderators/users only — the reset endpoint's floor + peer protection. `CanChangeRole(actor, target, newRole)` — super-admin: any target incl. self → any of the four roles; admin: moderator/user targets → moderator ↔ user only — and `CanDeleteUser(actor, target)` (the deletion peer matrix, written EXPLICITLY rather than as an alias of `CanResetPassword` so the two policies can never drift silently). `CanChangeUserStatus(actor, target)` — the one capability below the admin floor: moderators manage users only, admins manage moderators/users, super-admins manage anyone incl. self. `CanViewMetrics` (moderator+) — the dashboard-metrics floor, the same tier as the dashboard tabs (the `CanViewStaffList` precedent); `CanViewLogs` (super-admin only) — the log-viewer and audit-browser floor, deliberately NARROWER than the dashboard because those surfaces carry client addresses and forensic detail. The last-active-super-admin guard is deliberately NOT here: it needs the target's live status and the active super-admin count, which only the store transaction can see.
- `status.go`: the `users.status` CHECK values as constants + `IsValidStatus` — the role.go rationale applied to account status (the handler's filter validation uses it, never inline literals).
- Forced change: `User.MustChangePassword` and `SessionUser.MustChangePassword` carry the flag; `SessionByDigest` projects it so the middleware gate reads it from the session, not a second query.

## Authoritative docs

- SQL value and identifier mapping (the role/status CHECK values): [../../docs/patterns/go/sql-mapping.md](../../docs/patterns/go/sql-mapping.md)
- Doc-comment rules for exported symbols: [../../docs/patterns/go/comments.md](../../docs/patterns/go/comments.md)
- Test conventions: [../../docs/patterns/go/testing.md](../../docs/patterns/go/testing.md)
- Index: [../../docs/README.md](../../docs/README.md)
