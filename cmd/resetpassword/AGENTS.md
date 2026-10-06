# cmd/resetpassword — Agent Navigation

Operator-run account password reset: the break-glass path for a lone super-admin and any operator-driven reset (`make reset-password-local USERNAME=<name>`).

## Open this file when

- changing the reset mechanics, the temp-password generation, the maintenance-actor audit event, or the command's exit/stdout contract.

## Folder-local conventions

- Opens through the strict application opener (`database.OpenApplication`) — NEVER applies migrations; run `make migrate-local` first (migration 0008 carries the columns the reset writes).
- Run while the application is stopped (the `cmd/rebuildsearch` maintenance pattern).
- The reset logic lives in `auth.Service.ResetPassword` — this command is lookup + wiring + output only; do not duplicate password policy here.
- The temp password prints ONCE to stdout for out-of-band handover — never to logs; the audit event uses actor `"maintenance"` with empty request-id/address, and the command attaches its own logger to the context (`logging.With`) so the audit record lands where the rest of its output goes.
- It registers the durable audit writer (`audit.SetWriter`) so the `password_reset` event persists to `audit_events`; the registration is restored to nil on exit (the package-global discipline).
- Requires the same SSH/filesystem trust as backup restore — it is NOT a runtime backdoor.

## Authoritative docs

- [The password-reset mechanics](../../internal/auth/AGENTS.md)
- [The SQLite foundation rules](../../docs/patterns/go/sqlite.md)
