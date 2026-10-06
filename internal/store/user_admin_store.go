package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// StaffUser is the SQLite row projection for the staff user list.
//
// It is a projection, not the users table: the password verifier, canonical
// keys, auth_version, and the last-edit stamp stay out (row shape and
// public JSON are separate). Email is always selected here
// and the handler decides whether the viewer's role may see it (admin+
// only) — over-fetching one column keeps the store free of a
// caller-dependent projection. AvatarURL is the STORED reference (the
// 4-segment storage form, NULL when the account has no avatar, scanned
// through nullStringPtr) — projecting the absolute served URL is the
// handler's job, and unlike email it is not role-gated.
type StaffUser struct {
	ID            string
	Username      string
	Email         string
	AvatarURL     *string
	Role          string
	Status        string
	EmailVerified bool
	CreatedAtMS   int64
}

// UserPageKey is the keyset continuation tuple of the staff user list:
// (created_at_ms, id). The shared PageKey names its timestamp slot
// published_at_ms (the content ordering); the user list
// sorts by account creation, so it owns a honestly-named tuple the same
// way AdminPageKey{UpdatedAtMS} and FavoritePageKey do (the repo's
// per-store keyset precedent — the shared codec's payload stays
// structurally identical).
type UserPageKey struct {
	CreatedAtMS int64
	ID          string
}

// staffUsersFromWhere builds the staff user list's FROM/filter source and
// (when after is set) the keyset continuation, with its bind args — the one
// source the page read and the count share.
func staffUsersFromWhere(role, status string, after *UserPageKey) (string, []any) {
	var where strings.Builder
	where.WriteString(` FROM users WHERE 1 = 1`)
	args := make([]any, 0, 4)

	if role != "" {
		where.WriteString(` AND role = ?`)
		args = append(args, role)
	}
	if status != "" {
		where.WriteString(` AND status = ?`)
		args = append(args, status)
	}
	if after != nil {
		where.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id > ?))`)
		args = append(args, after.CreatedAtMS, after.CreatedAtMS, after.ID)
	}
	return where.String(), args
}

// ListStaffUsers returns one keyset page of the staff user list, newest
// first (created_at_ms DESC, id ASC — "strictly after" means an earlier
// creation time or the same time with a larger id), plus whether another page
// exists; it fetches limit+1 rows so hasNextPage needs no count query. role
// and status are optional filters validated by the caller.
//
// The continuation predicate makes four query shapes; a small WHERE builder
// keeps the keyset predicate visible without duplicating the filter clauses.
// All values are bound parameters — nothing is interpolated.
func ListStaffUsers(ctx context.Context, db *sql.DB, limit int, after *UserPageKey, role, status string) ([]StaffUser, bool, error) {
	fromWhere, args := staffUsersFromWhere(role, status, after)

	query := `SELECT id, username, email, avatar_url, role, status, email_verified_at_ms, created_at_ms` + fromWhere + ` ORDER BY created_at_ms DESC, id ASC LIMIT ?`
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list staff users: %w", err)
	}
	defer rows.Close()

	var users []StaffUser
	for rows.Next() {
		var u StaffUser
		var avatarURL sql.NullString
		var emailVerified sql.NullInt64
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &avatarURL, &u.Role, &u.Status,
			&emailVerified, &u.CreatedAtMS); err != nil {
			return nil, false, fmt.Errorf("store: scan staff user: %w", err)
		}
		u.AvatarURL = nullStringPtr(avatarURL)
		u.EmailVerified = emailVerified.Valid
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: iterate staff users: %w", err)
	}

	// The limit+1th row only proves a next page exists; it is not part of
	// this page's items.
	hasNext := len(users) > limit
	if hasNext {
		users = users[:limit]
	}
	return users, hasNext, nil
}

// CountStaffUsers returns the staff user row count the list walks — the
// pager's "of N" number, under the same role/status filters.
func CountStaffUsers(ctx context.Context, db *sql.DB, role, status string) (int, error) {
	fromWhere, args := staffUsersFromWhere(role, status, nil)
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*)`+fromWhere, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count staff users: %w", err)
	}
	return n, nil
}
