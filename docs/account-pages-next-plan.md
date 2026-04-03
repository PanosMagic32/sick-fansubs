# Account Pages Plan (Active)

## Scope

Continue account UX work now that auth/session hardening and media upload delivery are complete.

## Active Next Steps

### P1 — Account UX and Safety Enhancements

1. Add unsaved-changes route protection for account edits.
2. Add account deletion flow with explicit confirmation and forced logout.
3. Add optional favorites sort controls (newest/oldest).

### P2 — Optional Security Extension

1. Add optional email verification during registration (SMTP-backed).

## Validation Checklist For Next Work

1. Keep all user-facing strings in Greek.
2. Preserve `httpResource` + signals architecture.
3. Keep account changes backward-safe for authenticated sessions.
4. Keep media handling on the managed `/api/media/images` flow.
