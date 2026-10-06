-- 0011_password_reset_tokens.sql — self-service password reset tokens (decision 0032).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- One active token per user (UNIQUE(user_id)): a new forgot-password request
-- deletes the user's existing rows in the same transaction as the insert.
-- Only the SHA-256 digest of the 32-byte random token is stored — the raw
-- token never touches the database (0032 §5, the sessions.token_digest
-- precedent). Single-use is enforced by the store's conditional DELETE with
-- a RowsAffected == 1 gate, not by schema (0032 revision 2).
--
-- UTC Unix-millisecond timestamps (decision 0006); STRICT table (0006 §6).

CREATE TABLE password_reset_tokens (
    token_digest  BLOB    PRIMARY KEY CHECK (length(token_digest) = 32),
    user_id       TEXT    NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    expires_at_ms INTEGER NOT NULL CHECK (expires_at_ms > created_at_ms)
) STRICT;
