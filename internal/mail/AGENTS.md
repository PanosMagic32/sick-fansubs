# internal/mail — Agent Navigation

Outbound transactional email: the stdlib `net/smtp` relay surface and the server-side user-facing copy.

## Open this file when

- changing SMTP transport, TLS mode selection, auth, or send timeouts.
- changing the reset-email copy (Greek, server-side) or link construction.

## Placement

- This package exists because the relay is infrastructure, not business policy and the email copy has no other server-side home — the Lit catalog cannot serve server-rendered email. The service layer (forgot/reset orchestration) calls the `Sender` interface.
- The reset LINK is built by the service from `PUBLIC_BASE_URL` + the token; this package renders the message around it.

## Folder-local conventions

- `Sender` is the producer-side one-method seam (`audit.Writer` precedent) — consumer tests inject a fake.
- TLS mode follows the PORT: 465/2465 implicit TLS (TLS dial first), 25/587/2587 STARTTLS (plain dial + `client.StartTLS` BEFORE auth — `smtp.PlainAuth` refuses credentials over non-TLS unless the server is localhost). `dialAddr`/`rootCAs` are test-only seams: the fake server listens on a dynamic port while the mode must follow the real port constants.
- The send deadline is set on the CONNECTION after dial — stdlib `net/smtp` has no context support, and only the dial consumed the ctx deadline. `tls.Conn` delegates `SetDeadline` to the underlying conn, so the deadline survives the STARTTLS wrap (no re-set).
- **Test seam:** the deadline lives on `Relay.sendTimeout` — `NewRelay` sets the production default (`defaultSendTimeout` = 10 s); the stalling-server test injects 200 ms so the deadline pin waits milliseconds instead of the real budget. The production contract is unchanged; `testRelay` must keep the field set (a zero-value timeout would expire every test send instantly).
- One attempt, no retry loop, no unbound goroutines.
- Server reply failures are flattened by `smtpReplyError`: a well-formed reply keeps the stage and the SMTP reply code, a malformed reply line keeps the stage alone — a relay's response text can echo credential or message material, and the handler logs send errors. RCPT is the strictest exception: the category only, no code, no server text, no recipient address (emails are PII); transport errors and the pre-credential TLS handshake/certificate failures keep their cause. CR/LF in the recipient is rejected at `Send` (migrated emails bypass the registration charset rule).
- `LogLink` is the loopback-dev-only sender (logs the message instead of sending — the ONLY path that ever logs a reset link). The guard lives in config validation (`ValidateMailRelay` + `cmd/api`), not here — never construct it outside that wiring. It logs the subject and the body (the link IS the dev affordance) but NOT the recipient: an address in a log field is what the log hygiene rule forbids, and this path is that rule's one audited exemption — reachable only when `APP_ENV != production` and the public base URL is loopback.
- The subject is RFC 2047 encoded-word (`mime.QEncoding`) — raw Greek in headers is invalid.
- No `Reply-To` header is emitted: the From is a no-reply sender and replies are meant to bounce. `SMTP_REPLY_TO` does not exist in config either.
- Email verification: `verify_message.go` holds `VerifyEmail(link)` — the registration/email-change verification message (polite register, 7-day expiry note), built exactly like the reset message; the service constructs the `/auth/verify?token=` link the same way.
- Email change notice: `email_changed_message.go` holds `EmailChangedNotice(newEmail)` — the plain-text security notice sent to the PREVIOUS address (polite register, the new address named, no link and no token), built like the reset message; the service sends it after the committed swap and never surfaces a failure.

## Authoritative docs

- [Environment variables and validation](../config/AGENTS.md)
- [Documentation conventions](../../docs/patterns/docs-conventions.md) — rule 10 is the recorded Greek email-copy exception
