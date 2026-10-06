-- 0012_email_verification.sql — registration email verification (decision 0035).
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- users.email_verified_at_ms: NULL = unverified; a positive UTC-ms value =
-- verified. The backfill marks every EXISTING row verified (R1): imported
-- community members and beta accounts came from real legacy data with real
-- emails — no banner for them. New registrations start NULL.
--
-- email_verification_tokens: the 0011 shape — SHA-256 digest PK, one active
-- token per user (UNIQUE user_id), FK cascade on user delete, UTC-ms
-- timestamps (0006). Single-use is enforced by the store's conditional
-- DELETE with a RowsAffected == 1 gate (0035 §3), not by schema.

ALTER TABLE users ADD COLUMN email_verified_at_ms INTEGER
    CHECK (email_verified_at_ms IS NULL OR email_verified_at_ms > 0);

UPDATE users SET email_verified_at_ms = created_at_ms;

CREATE TABLE email_verification_tokens (
    token_digest  BLOB    PRIMARY KEY CHECK (length(token_digest) = 32),
    user_id       TEXT    NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    created_at_ms INTEGER NOT NULL CHECK (created_at_ms > 0),
    expires_at_ms INTEGER NOT NULL CHECK (expires_at_ms > created_at_ms)
) STRICT;
