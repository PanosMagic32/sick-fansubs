# cmd/resetpassword — Agent Navigation

Operator-run account password reset: the break-glass path for a lone super-admin and any operator-driven reset (`make reset-password-local USERNAME=<name>`).

On the VPS, run the binary that ships INSIDE the app image while the application is stopped:

```sh
docker compose stop app
docker run --rm \
  -v sick-fansubs_sick-data:/app/data \
  --entrypoint /app/resetpassword \
  ghcr.io/panosmagic32/sick-fansubs:<TAG> -username <name>
docker compose up -d app
```

The image tag is the deployed application tag, so the strict opener sees the exact schema the running app uses. The temporary password prints once to stdout; logs go to stderr. Do not redirect the combined output to a file.

## Open this file when

- changing the reset mechanics, the temp-password generation, the maintenance-actor audit event, or the command's exit/stdout contract.

## Folder-local conventions

- Opens through the strict application opener (`database.OpenApplication`) — NEVER applies migrations; run `make migrate-local` first (migration 0008 carries the columns the reset writes).
- Run while the application is stopped (the `cmd/rebuildsearch` maintenance pattern). On the VPS the binary is `/app/resetpassword` inside the app image; there is no separate reset image.
- The reset logic lives in `auth.Service.ResetPassword` — this command is lookup + wiring + output only; do not duplicate password policy here.
- The temp password prints ONCE to stdout for out-of-band handover — never to logs; the audit event uses actor `"maintenance"` with empty request-id/address, and the command attaches its own logger to the context (`logging.With`) so the audit record lands where the rest of its output goes.
- It registers the durable audit writer (`audit.SetWriter`) so the `password_reset` event persists to `audit_events`; the registration is restored to nil on exit (the package-global discipline).
- Requires the same SSH/filesystem trust as backup restore — it is NOT a runtime backdoor.

## Authoritative docs

- [The password-reset mechanics](../../internal/auth/AGENTS.md)
- [The SQLite foundation rules](../../docs/patterns/go/sqlite.md)
