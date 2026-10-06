# internal/config — Agent Navigation

Typed, validated environment configuration.

## Open this file when

- adding, renaming, or re-validating an environment variable or default.

## Folder-local conventions

- `Load()` runs in `main` only — no package-init side effects. It is the single reader for application runtime configuration. Two command families additionally own variables: `cmd/backup` (`DATA_DIR`, `BACKUP_DIR`, `AGE_RECIPIENT`, `APP_ENV`) and `cmd/fetchmedia` (`IMPORT_FILE`, `MEDIA_SOURCE_DIR`) read them without calling `Load()`; the import tools (`importdata`, `importmedia`, `importavatars`) call `Load()` for the shared `DATA_DIR` contract and read only `IMPORT_FILE`/`MEDIA_SOURCE_DIR` themselves.
- **Variable inventory.** Every command that calls `Load()` enforces the environment arms below, break-glass tools included; the mail-relay requirements are `ValidateMailRelay`'s and bind `cmd/api` only, and the two secret rows bind the commands that load them — `cmd/api` today.

  | Variable                     | Default                                      | Refused / required                                                                                                                       |
  | ---------------------------- | -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
  | `PORT`                       | `8080`                                       | outside 1–65535                                                                                                                          |
  | `DATA_DIR`                   | `./data` (resolved to absolute)              | surrounding whitespace                                                                                                                   |
  | `PUBLIC_BASE_URL`            | `http://localhost:3000`                      | relative, non-http(s), missing host, userinfo, query, fragment; production requires https                                                |
  | `APP_ENV`                    | `development`                                | anything but `development`/`production`                                                                                                  |
  | `TRUSTED_ORIGIN`             | `http://localhost:3000` in development       | origin-only shape (no path/query/fragment/userinfo/default port/trailing-dot host); production requires an explicit https value          |
  | `SMTP_HOST`                  | empty (dev log-link fallback, loopback-only) | production requires it (`ValidateMailRelay`)                                                                                             |
  | `SMTP_PORT`                  | `587`                                        | outside 25/465/587/2465/2587                                                                                                             |
  | `SMTP_USERNAME`              | `resend`                                     | — (surrounding whitespace trimmed; whitespace-only means the default)                                                                    |
  | `SMTP_FROM`                  | `noreply@sickfansubs.com` in development     | CR/LF; production requires it (`ValidateMailRelay`)                                                                                      |
  | `VAPID_PUBLIC_KEY`           | the committed production public key          | not URL-safe base64 decoding to 65 bytes                                                                                                 |
  | `smtp_api_key` (secret)      | —                                            | missing/empty in production; empty with `SMTP_HOST` set refuses startup (`cmd/api`); group/other-readable; non-regular file; over 64 KiB |
  | `vapid_private_key` (secret) | —                                            | the same checks, plus a pair mismatch with `VAPID_PUBLIC_KEY` at startup (`ValidateVAPIDPair`)                                           |

- **Secrets:** secret material arrives as Compose file secrets at `/run/secrets/<name>` and is read only through `(*Config).LoadSecret` (secrets.go) — fail-closed in production (missing/empty = error), optional in dev, group/other-readable files refused, symlinks and non-regular files refused, files over 64 KiB refused, surrounding whitespace trimmed (inner whitespace kept). Secret names are plain Compose names (`[A-Za-z0-9._-]`).
- `PUBLIC_BASE_URL` is the single source for absolute media URLs: absolute http(s), optional path prefix, no query/fragment, trailing `/` normalized. Dev default `http://localhost:3000` pairs with the Vite `/media` proxy — change both together.
- `APP_ENV` (`development` | `production`) + `TRUSTED_ORIGIN`. Contracts: production enables Secure session cookies, has NO `TRUSTED_ORIGIN` default (must be an explicit https origin — no userinfo, path, query, fragment, default port, or trailing-dot host), and fails closed unless both `TRUSTED_ORIGIN` and `PUBLIC_BASE_URL` are https. `(*Config).SecureCookies()` is the one APP_ENV-to-Secure mapping every route registration reads. Names and shape are stable; the fail-closed behavior must not be silently relaxed.
- **SMTP:** `Load()` enforces only the universal hygiene (port whitelist 25/465/587/2465/2587; CR/LF in From — no Reply-To header exists). The ENVIRONMENT requirements live in `(*Config).ValidateMailRelay()` — called by `cmd/api` ONLY, because the break-glass maintenance commands share `Load()` under `APP_ENV=production` and must never fail on mail they do not send. The dev log-link fallback is loopback-only (`PUBLIC_BASE_URL == http://localhost:3000`).
- **VAPID:** `VAPIDPublicKey` (Env `VAPID_PUBLIC_KEY`) is PUBLIC material — the same value the web bundle bakes in (`web/src/shared/config/vapid.ts`), `.env.example` documents, and the compose `VAPID_PUBLIC_KEY:-…` fallback carries; all four pinned equal by `vapid_pin_test.go`. The committed value is the PRODUCTION keypair's public half; rotation = regenerate both halves + swap every copy. `validateVAPIDPublicKey` enforces URL-safe base64 decoding to 65 bytes (length only — point-on-curve is the library's job at send time). The PRIVATE half is the compose secret `vapid_private_key` via `LoadSecret` — production fail-closed at startup (the `cmd/api` wiring), development disables delivery only.

## Authoritative docs

- Go structure, comments, testing, errors, logging: [structure.md](../../docs/patterns/go/structure.md), [comments.md](../../docs/patterns/go/comments.md), [testing.md](../../docs/patterns/go/testing.md), [errors.md](../../docs/patterns/go/errors.md), [logging.md](../../docs/patterns/go/logging.md)
- The SQLite foundation (the one absolute `DATA_DIR`, owner-only rules): [sqlite.md](../../docs/patterns/go/sqlite.md)
- Secret topology, rotation, and the Compose mounts: [deploy.md](../../docs/ops/deploy.md)
- Comment and doc rules: [docs-conventions.md](../../docs/patterns/docs-conventions.md)
- Index: [docs/README.md](../../docs/README.md)
