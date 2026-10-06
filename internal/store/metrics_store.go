package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/identity"
)

// Staff dashboard metrics — LIVE aggregates over existing tables and the
// audit ledger.
//
// Nothing in this file writes: there are no counter columns, no summary
// table, and no migration behind these numbers ("Storage: none").
// Every value is a fresh COUNT/GROUP BY over the same rows the rest of the
// application reads, so a metric cannot outlive the data it counts. The two
// readers run as separate statements (no shared snapshot), so one response
// may combine reads taken microseconds apart; each value is individually
// exact, and the bounded skew is accepted (a dashboard number, not a
// transaction).
//
// Content tables and status values come from the ContentKind descriptors and
// the status CHECK values (migration 0003/0004).

// MetricsUserTotals is the live `users` breakdown — total plus the per-role
// and per-status partitions. Every role/status value is
// CHECK-constrained (migration 0001), so unknown values cannot exist.
type MetricsUserTotals struct {
	Total           int64
	RoleUser        int64
	RoleModerator   int64
	RoleAdmin       int64
	RoleSuperAdmin  int64
	StatusActive    int64
	StatusSuspended int64
}

// MetricsContentTotals is one content type's status breakdown. Total is the
// live row count, not the sum of the status buckets — for a migrated row the
// status is always one of the three CHECK values, so the numbers agree.
type MetricsContentTotals struct {
	Total     int64
	Published int64
	Draft     int64
	Archived  int64
}

// MetricsTotals is the dashboard's totals section.
type MetricsTotals struct {
	Users     MetricsUserTotals
	BlogPosts MetricsContentTotals
	Projects  MetricsContentTotals
	// Comments and Favorites sum both content types (blog posts + projects).
	Comments  int64
	Favorites int64
}

// MetricsActivity is the dashboard's last-30-days section. The
// change counts are SUCCESS rows only — a blocked 403 or a failed write is
// not activity (the audit ledger carries both; the metrics view counts what
// actually happened). SignInFailures is the one deliberate failure metric:
// the event name itself is the failure signal.
type MetricsActivity struct {
	Registrations    int64
	ActiveUsers      int64
	SignInFailures   int64
	ContentCreated   int64
	ContentUpdated   int64
	ContentDeleted   int64
	CommentDeletions int64
	UsersSuspended   int64
	UsersReactivated int64
	UsersDeleted     int64
	RoleChanges      int64
}

// GetMetricsTotals reads every live total in one call.
func GetMetricsTotals(ctx context.Context, db *sql.DB) (MetricsTotals, error) {
	var out MetricsTotals

	userRows, err := db.QueryContext(ctx,
		`SELECT role, status, COUNT(*) FROM users GROUP BY role, status`)
	if err != nil {
		return out, fmt.Errorf("store: metrics user totals: %w", err)
	}
	defer userRows.Close()
	for userRows.Next() {
		var role, status string
		var n int64
		if err := userRows.Scan(&role, &status, &n); err != nil {
			return out, fmt.Errorf("store: metrics user totals scan: %w", err)
		}
		out.Users.Total += n
		switch role {
		case identity.RoleUser:
			out.Users.RoleUser += n
		case identity.RoleModerator:
			out.Users.RoleModerator += n
		case identity.RoleAdmin:
			out.Users.RoleAdmin += n
		case identity.RoleSuperAdmin:
			out.Users.RoleSuperAdmin += n
		}
		switch status {
		case identity.StatusActive:
			out.Users.StatusActive += n
		case identity.StatusSuspended:
			out.Users.StatusSuspended += n
		}
	}
	if err := userRows.Err(); err != nil {
		return out, fmt.Errorf("store: metrics user totals rows: %w", err)
	}

	if out.BlogPosts, err = metricsContentTotals(ctx, db, BlogContent); err != nil {
		return out, err
	}
	if out.Projects, err = metricsContentTotals(ctx, db, ProjectContent); err != nil {
		return out, err
	}

	// The cross-kind sums sweep the descriptor list, so a new kind joins them
	// without touching this file.
	commentTerms := make([]string, 0, len(allContentKinds))
	favoriteTerms := make([]string, 0, len(allContentKinds))
	for _, k := range allContentKinds {
		commentTerms = append(commentTerms, `(SELECT COUNT(*) FROM `+k.comments+`)`)
		favoriteTerms = append(favoriteTerms, `(SELECT COUNT(*) FROM `+k.favorites+`)`)
	}
	if err := db.QueryRowContext(ctx, `SELECT `+strings.Join(commentTerms, ` + `)).Scan(&out.Comments); err != nil {
		return out, fmt.Errorf("store: metrics comment total: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT `+strings.Join(favoriteTerms, ` + `)).Scan(&out.Favorites); err != nil {
		return out, fmt.Errorf("store: metrics favorite total: %w", err)
	}

	return out, nil
}

// metricsContentTotals runs one status GROUP BY over a content table.
func metricsContentTotals(ctx context.Context, db *sql.DB, k ContentKind) (MetricsContentTotals, error) {
	var out MetricsContentTotals
	rows, err := db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM `+k.table+` GROUP BY status`)
	if err != nil {
		return out, fmt.Errorf("store: metrics %ss totals: %w", k.noun, err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return out, fmt.Errorf("store: metrics %ss totals scan: %w", k.noun, err)
		}
		out.Total += n
		switch status {
		case "published":
			out.Published += n
		case "draft":
			out.Draft += n
		case "archived":
			out.Archived += n
		}
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("store: metrics %ss totals rows: %w", k.noun, err)
	}
	return out, nil
}

// GetMetricsActivity counts the last-30-days activity from the audit ledger and
// one live table read. sinceMS is the window's opening instant
// (UTC milliseconds) — supplied by the caller so the boundary is explicit and
// testable, never recomputed here.
//
// Registrations counts users.created_at_ms: a registration is not an audit
// event, and a deleted account leaves no row to count — the metric
// describes current accounts, not historical sign-up attempts.
//
// ActiveUsers counts DISTINCT sign_in_success actors. Sessions carry no
// last-touch column, so a successful sign-in is the accepted activity
// proxy.
func GetMetricsActivity(ctx context.Context, db *sql.DB, sinceMS int64) (MetricsActivity, error) {
	var out MetricsActivity

	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE created_at_ms >= ?`, sinceMS).Scan(&out.Registrations); err != nil {
		return out, fmt.Errorf("store: metrics registrations: %w", err)
	}

	if err := db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT actor_id) FROM audit_events
			WHERE event = ? AND created_at_ms >= ?`,
		audit.EventSignInSuccess, sinceMS).Scan(&out.ActiveUsers); err != nil {
		return out, fmt.Errorf("store: metrics active users: %w", err)
	}

	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events
			WHERE event = ? AND created_at_ms >= ?`,
		audit.EventSignInFailure, sinceMS).Scan(&out.SignInFailures); err != nil {
		return out, fmt.Errorf("store: metrics sign-in failures: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT event, COUNT(*) FROM audit_events
			WHERE result = ? AND created_at_ms >= ?
			  AND event IN (?, ?, ?, ?, ?, ?, ?, ?)
			GROUP BY event`,
		audit.ResultSuccess, sinceMS,
		audit.EventContentCreated,
		audit.EventContentUpdated,
		audit.EventContentDeleted,
		audit.EventCommentDeleted,
		audit.EventUserSuspended,
		audit.EventUserReactivated,
		audit.EventUserDeleted,
		audit.EventRoleChanged)
	if err != nil {
		return out, fmt.Errorf("store: metrics change events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event string
		var n int64
		if err := rows.Scan(&event, &n); err != nil {
			return out, fmt.Errorf("store: metrics change events scan: %w", err)
		}
		switch event {
		case audit.EventContentCreated:
			out.ContentCreated = n
		case audit.EventContentUpdated:
			out.ContentUpdated = n
		case audit.EventContentDeleted:
			out.ContentDeleted = n
		case audit.EventCommentDeleted:
			out.CommentDeletions = n
		case audit.EventUserSuspended:
			out.UsersSuspended = n
		case audit.EventUserReactivated:
			out.UsersReactivated = n
		case audit.EventUserDeleted:
			out.UsersDeleted = n
		case audit.EventRoleChanged:
			out.RoleChanges = n
		}
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("store: metrics change events rows: %w", err)
	}

	return out, nil
}
