package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Web push persistence: per-device subscription rows and per-user per-kind
// preference toggles.
//
// Preferences follow the "absence = default" model: a missing row means the
// caller's role default (moderator+ on, everyone else off — the handler
// derives it from the session role); only an explicit user toggle writes a
// row. Every function here is a single statement or a tiny transaction —
// one row's worth of work, no cross-table invariants.

// NotificationEvent is the fan-out event a comment write returns to its
// handler when a user_notifications row actually landed: the recipient, the
// kind, and everything the push payload needs. CommentID names the deep-link
// target (the hearted comment for `heart`, the new reply for `comment_reply`).
// The actor is NOT part of the event — the handler already holds the session
// user (the actor IS the requester), so the field would be dead weight (no
// dead columns).
type NotificationEvent struct {
	RecipientID string
	// Kind is one of the user_notifications kinds ("heart" | "comment_reply" |
	// "comment" | "content_updated" | "new_content" | "comment_removed" |
	// "draft_activity").
	Kind         string
	ContentKind  string // the content kind discriminator
	ContentID    string
	CommentID    string
	ContentTitle string
	// DraftAction is the unpublished transition a draft_activity event
	// reports ("created" | "updated" | "unpublished"); empty for every other
	// kind.
	DraftAction string
}

// PushSubscription is the settings-list projection of one subscription row.
type PushSubscription struct {
	ID          string
	Endpoint    string
	CreatedAtMS int64
}

// PushSubscriptionKeys is the sender's row projection — the endpoint and
// the RFC 8291 key material. The KEY MATERIAL never leaves the sender; the
// settings list projection carries the endpoint, which is the device's own.
type PushSubscriptionKeys struct {
	ID       string
	Endpoint string
	P256dh   string
	Auth     string
}

// UpsertPushSubscription stores or refreshes one device subscription and
// returns the stored row id. A resubscribe for the same endpoint updates
// the key material and updated_at_ms in place (the row id is unchanged —
// the minted id is only consumed by a fresh insert); an endpoint owned by
// ANOTHER user is reassigned (deleted there, inserted here — the device's
// newest login owns the endpoint).
func UpsertPushSubscription(ctx context.Context, db *sql.DB, id, userID, endpoint, p256dh, auth string, createdAtMS int64) (string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("store: begin push subscription upsert: %w", err)
	}
	defer tx.Rollback()

	// Reassign before the insert so the UNIQUE endpoint can never collide
	// with another user's row inside this transaction.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE endpoint = ? AND user_id <> ?`,
		endpoint, userID); err != nil {
		return "", fmt.Errorf("store: reassign push subscription: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			updated_at_ms = excluded.updated_at_ms`,
		id, userID, endpoint, p256dh, auth, createdAtMS, createdAtMS); err != nil {
		if isForeignKeyViolation(err) {
			// A user deleted between the session lookup and this insert —
			// the client's dead session heals through its own 401 paths.
			return "", ErrNotFound
		}
		return "", fmt.Errorf("store: upsert push subscription: %w", err)
	}

	var storedID string
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM push_subscriptions WHERE endpoint = ?`, endpoint).Scan(&storedID); err != nil {
		return "", fmt.Errorf("store: read push subscription id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("store: commit push subscription upsert: %w", err)
	}
	return storedID, nil
}

// ListPushSubscriptions returns the user's subscription rows, oldest first
// (a deterministic order for the settings list; the list is small — device
// count, not a paged feed).
func ListPushSubscriptions(ctx context.Context, db *sql.DB, userID string) ([]PushSubscription, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, endpoint, created_at_ms
		FROM push_subscriptions
		WHERE user_id = ?
		ORDER BY created_at_ms ASC, id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list push subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []PushSubscription
	for rows.Next() {
		var s PushSubscription
		if err := rows.Scan(&s.ID, &s.Endpoint, &s.CreatedAtMS); err != nil {
			return nil, fmt.Errorf("store: scan push subscription: %w", err)
		}
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate push subscriptions: %w", err)
	}
	return subs, nil
}

// DeletePushSubscription removes one of the user's subscription rows.
// Idempotent: an unknown or foreign id affects no row and is success (the
// masked-204 contract).
func DeletePushSubscription(ctx context.Context, db *sql.DB, userID, id string) error {
	if _, err := db.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?`,
		id, userID); err != nil {
		return fmt.Errorf("store: delete push subscription: %w", err)
	}
	return nil
}

// PushSubscriptionsForUser returns every subscription row with its key
// material — the sender's fan-out input. Deleted users' rows are gone via
// the FK cascade, so the recipient always exists at read time. The order is
// `created_at_ms, id` (the settings-list order) so the per-endpoint self-test
// report is DETERMINISTIC — the report exposes it as "the order the store
// returned", which must mean something.
func PushSubscriptionsForUser(ctx context.Context, db *sql.DB, userID string) ([]PushSubscriptionKeys, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, endpoint, p256dh, auth
		FROM push_subscriptions
		WHERE user_id = ?
		ORDER BY created_at_ms ASC, id ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: load push subscriptions for user: %w", err)
	}
	defer rows.Close()

	var subs []PushSubscriptionKeys
	for rows.Next() {
		var s PushSubscriptionKeys
		if err := rows.Scan(&s.ID, &s.Endpoint, &s.P256dh, &s.Auth); err != nil {
			return nil, fmt.Errorf("store: scan push subscription keys: %w", err)
		}
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate push subscription keys: %w", err)
	}
	return subs, nil
}

// NotificationPreference is one kind's EFFECTIVE push toggle — a stored row
// or the caller's role default.
type NotificationPreference struct {
	Kind        string
	PushEnabled bool
}

// EffectiveNotificationPreferences returns the effective push toggle for
// every kind in kinds, in the caller's order. A missing row means the kind's
// DEFAULT (defaultOn — the handler derives it from the session role AND the
// kind: the draft notice is opt-in for everyone).
func EffectiveNotificationPreferences(ctx context.Context, db *sql.DB, userID string, kinds []string, defaultOn func(kind string) bool) ([]NotificationPreference, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT kind, push_enabled
		FROM notification_preferences
		WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: load notification preferences: %w", err)
	}
	defer rows.Close()

	stored := make(map[string]bool, len(kinds))
	for rows.Next() {
		var (
			kind    string
			enabled bool
		)
		if err := rows.Scan(&kind, &enabled); err != nil {
			return nil, fmt.Errorf("store: scan notification preference: %w", err)
		}
		stored[kind] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate notification preferences: %w", err)
	}

	prefs := make([]NotificationPreference, 0, len(kinds))
	for _, kind := range kinds {
		enabled, ok := stored[kind]
		if !ok {
			enabled = defaultOn(kind)
		}
		prefs = append(prefs, NotificationPreference{Kind: kind, PushEnabled: enabled})
	}
	return prefs, nil
}

// EffectivePushEnabled returns the effective push toggle for one kind —
// the stored row or the role default (defaultOn — the sender derives it
// from the recipient's role). The sender's single-kind companion to
// EffectiveNotificationPreferences (the handler's list read).
func EffectivePushEnabled(ctx context.Context, db *sql.DB, userID, kind string, defaultOn bool) (bool, error) {
	var stored sql.NullBool
	if err := db.QueryRowContext(ctx, `
		SELECT push_enabled
		FROM notification_preferences
		WHERE user_id = ? AND kind = ?`, userID, kind).Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return defaultOn, nil
		}
		return false, fmt.Errorf("store: read push enabled: %w", err)
	}
	return stored.Bool, nil
}

// SetNotificationPreference writes one explicit per-kind toggle (a row is an
// override; absence stays the role default).
// The kind CHECK is the store-side guard — the handler validates against
// the known-kinds list first, so a CHECK failure here is a programming
// error, surfaced as an ordinary error (500).
func SetNotificationPreference(ctx context.Context, db *sql.DB, userID, kind string, pushEnabled bool, updatedAtMS int64) error {
	if _, err := db.ExecContext(ctx, `
		INSERT INTO notification_preferences (user_id, kind, push_enabled, updated_at_ms)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, kind) DO UPDATE SET
			push_enabled = excluded.push_enabled,
			updated_at_ms = excluded.updated_at_ms`,
		userID, kind, pushEnabled, updatedAtMS); err != nil {
		if isForeignKeyViolation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("store: set notification preference: %w", err)
	}
	return nil
}
