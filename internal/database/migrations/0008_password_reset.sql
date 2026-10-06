-- 0008_password_reset.sql — Forced password change + audit target-role snapshot.
--
-- The migration runner owns the transaction; do not add BEGIN/COMMIT/ROLLBACK
-- statements here.
--
-- must_change_password (decision 0024 §4): the forced-change flag. An admin
-- reset (POST /api/v1/users/{id}/reset-password or cmd/resetpassword) sets it;
-- the authenticated chain then rejects every non-exempt path with 403 until
-- the user changes the password. Migrated users default 0. ALTER TABLE ADD
-- COLUMN requires a non-null default, which also backfills existing rows.
--
-- target_role (decision 0025 §3, landed early per the 13c brief): the
-- notifications feed's visibility snapshot — the target account's role at
-- emission time. The 13c–13e emitters load the target for the operation
-- anyway and pass its role; without the column now, those events would
-- persist NULL and the feed's role-weighted visibility rule would have no
-- data. NULL for events without a target. It is a SNAPSHOT, not a reference —
-- 0024 §3's no-FK rule for audit_events is unchanged.

ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0
    CHECK (must_change_password IN (0, 1));

ALTER TABLE audit_events ADD COLUMN target_role TEXT;
