-- 0001_users.sql — Initial user accounts table.
--
-- This is the first application-domain migration. The migration runner owns the
-- transaction; do not add BEGIN/COMMIT/ROLLBACK statements here.
--
-- Uses STRICT table mode which enforces column type checking at insert/update
-- time (rejecting values that don't match the declared column type), unlike
-- SQLite's default flexible typing.
--
-- Follows decision 0008 (User Identity and Account Schema):
--   - ASCII (American Standard Code for Information Interchange) usernames
--   - Email stored lowercased with UNIQUE constraint
--   - bcrypt cost 12 for password verifiers
--   - auth_version for session revocation (decision 0005)
--   - UTC (Coordinated Universal Time) Unix-millisecond timestamps (decision 0006)
--   - STRICT table (decision 0006)
--   - Opaque TEXT IDs: preserved ObjectId strings for migrated users,
--     new 32-char hex random IDs for registrations

CREATE TABLE users (
    id              TEXT    PRIMARY KEY,
    username        TEXT    NOT NULL,
    username_canon  TEXT    NOT NULL UNIQUE,
    email           TEXT    NOT NULL UNIQUE,
    password        TEXT    NOT NULL,
    role            TEXT    NOT NULL DEFAULT 'user'
                            CHECK (role IN ('super-admin', 'admin', 'moderator', 'user')),
    status          TEXT    NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'suspended')),
    auth_version    INTEGER NOT NULL DEFAULT 1 CHECK (auth_version > 0),
    avatar_url      TEXT,
    created_at_ms   INTEGER NOT NULL CHECK (created_at_ms > 0),
    updated_at_ms   INTEGER NOT NULL CHECK (updated_at_ms >= created_at_ms)
) STRICT;
