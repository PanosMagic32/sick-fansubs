-- 0002_sessions.sql — Opaque session storage.
--
-- Follows decision 0005 (Opaque SQLite-Backed Browser Sessions):
--   - 32-byte SHA-256 token digest stored as BLOB; the raw session token
--     is never persisted or logged.
--   - Independent 32-byte CSRF token per session for X-CSRF-Token validation.
--   - auth_version stamped at creation; SessionByDigest enforces
--     s.auth_version = u.auth_version so that auth-version bumps
--     (password change, logout-all, suspension) invalidate all sessions
--     at the database level without needing service-layer discipline.
--   - ON DELETE CASCADE: deleting a user removes all their sessions.
--   - UTC Unix-millisecond timestamps (decision 0006).
--   - STRICT table (decision 0006).

CREATE TABLE sessions (
    id              TEXT    PRIMARY KEY,
    user_id         TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_digest    BLOB    NOT NULL UNIQUE CHECK (length(token_digest) = 32),
    csrf_token      BLOB    NOT NULL CHECK (length(csrf_token) = 32),
    auth_version    INTEGER NOT NULL CHECK (auth_version > 0),
    created_at_ms   INTEGER NOT NULL CHECK (created_at_ms > 0),
    expires_at_ms   INTEGER NOT NULL CHECK (expires_at_ms > created_at_ms)
) STRICT;

CREATE INDEX idx_sessions_expires_at_ms ON sessions(expires_at_ms);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
