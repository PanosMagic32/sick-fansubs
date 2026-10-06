package store

import (
	"errors"
	"testing"
)

// Push-store tests: subscription
// upsert/reassign/list/delete and the preference default/override identity.

const (
	fakeEndpointA = "https://push.example.com/endpoint/a"
	fakeEndpointB = "https://push.example.com/endpoint/b"
	// 'A' is base64 value 0 — these decode to zero-filled bytes of the
	// right LENGTHS (65/16); the store never validates key CONTENT (the
	// endpoint handler does, before the row exists).
	fakeP256dh = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	fakeAuth   = "AAAAAAAAAAAAAAAAAAAAAA"
)

func TestUpsertPushSubscription(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")

	// Fresh insert consumes the minted id.
	id, err := UpsertPushSubscription(ctx, db, "s1", "u1", fakeEndpointA, fakeP256dh, fakeAuth, 1000)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if id != "s1" {
		t.Errorf("fresh insert id: got %q, want %q", id, "s1")
	}

	// Same-endpoint resubscribe updates in place and returns the STORED id
	// (the minted id is discarded).
	id, err = UpsertPushSubscription(ctx, db, "s-new", "u1", fakeEndpointA, "BBBB", "CCCC", 2000)
	if err != nil {
		t.Fatalf("resubscribe: %v", err)
	}
	if id != "s1" {
		t.Errorf("resubscribe id: got %q, want %q (the existing row's id)", id, "s1")
	}
	var userID, p256dh, auth string
	var updatedAt int64
	if err := db.QueryRow(`SELECT user_id, p256dh, auth, updated_at_ms FROM push_subscriptions WHERE endpoint = ?`, fakeEndpointA).
		Scan(&userID, &p256dh, &auth, &updatedAt); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if userID != "u1" || p256dh != "BBBB" || auth != "CCCC" || updatedAt != 2000 {
		t.Errorf("row after resubscribe: user=%s p256dh=%s auth=%s updated=%d", userID, p256dh, auth, updatedAt)
	}

	// A different user subscribing the same endpoint REASSIGNS it (the
	// device's newest login owns it).
	id, err = UpsertPushSubscription(ctx, db, "s2", "u2", fakeEndpointA, fakeP256dh, fakeAuth, 3000)
	if err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if id != "s2" {
		t.Errorf("reassign id: got %q, want %q", id, "s2")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE endpoint = ?`, fakeEndpointA).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 1 {
		t.Errorf("endpoint rows after reassign: got %d, want 1", n)
	}

	// A deleted user's insert maps to ErrNotFound (the FK).
	if _, err := UpsertPushSubscription(ctx, db, "s3", "ghost", fakeEndpointB, fakeP256dh, fakeAuth, 4000); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted user: got %v, want ErrNotFound", err)
	}
}

func TestListAndDeletePushSubscriptions(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")

	if _, err := UpsertPushSubscription(ctx, db, "s1", "u1", fakeEndpointA, fakeP256dh, fakeAuth, 1000); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if _, err := UpsertPushSubscription(ctx, db, "s2", "u2", fakeEndpointB, fakeP256dh, fakeAuth, 1000); err != nil {
		t.Fatalf("seed b: %v", err)
	}

	subs, err := ListPushSubscriptions(ctx, db, "u1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 || subs[0].ID != "s1" || subs[0].Endpoint != fakeEndpointA {
		t.Errorf("u1 list: got %+v, want only s1", subs)
	}

	// Delete is user-scoped and idempotent; a foreign id changes nothing.
	if err := DeletePushSubscription(ctx, db, "u1", "s2"); err != nil {
		t.Fatalf("delete foreign: %v", err)
	}
	if err := DeletePushSubscription(ctx, db, "u1", "s1"); err != nil {
		t.Fatalf("delete own: %v", err)
	}
	if err := DeletePushSubscription(ctx, db, "u1", "s1"); err != nil {
		t.Fatalf("delete repeat: %v", err)
	}
	subs, err = ListPushSubscriptions(ctx, db, "u1")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("u1 list after delete: got %d rows, want 0", len(subs))
	}
	keys, err := PushSubscriptionsForUser(ctx, db, "u2")
	if err != nil {
		t.Fatalf("keys for u2: %v", err)
	}
	if len(keys) != 1 || keys[0].P256dh != fakeP256dh {
		t.Errorf("u2 keys: got %+v, want one row with the seeded p256dh", keys)
	}
}

func TestNotificationPreferences(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")

	kinds := []string{"heart", "comment_reply"}

	// The per-kind default predicate the handler passes: role default ON for
	// the audience kinds.
	alwaysOn := func(string) bool { return true }
	alwaysOff := func(string) bool { return false }

	// No rows → every kind falls back to the kind's default.
	prefs, err := EffectiveNotificationPreferences(ctx, db, "u1", kinds, alwaysOff)
	if err != nil {
		t.Fatalf("prefs default-off: %v", err)
	}
	if len(prefs) != 2 || prefs[0].PushEnabled || prefs[1].PushEnabled {
		t.Errorf("default-off prefs: got %+v, want both false", prefs)
	}
	prefs, err = EffectiveNotificationPreferences(ctx, db, "u1", kinds, alwaysOn)
	if err != nil {
		t.Fatalf("prefs default-on: %v", err)
	}
	if !prefs[0].PushEnabled || !prefs[1].PushEnabled {
		t.Errorf("default-on prefs: got %+v, want both true", prefs)
	}

	// The default is asked PER KIND — a mixed predicate must land per kind
	// (the draft notice's opt-in shape).
	onlyReply := func(kind string) bool { return kind == "comment_reply" }
	prefs, err = EffectiveNotificationPreferences(ctx, db, "u1", kinds, onlyReply)
	if err != nil {
		t.Fatalf("prefs mixed defaults: %v", err)
	}
	if prefs[0].PushEnabled || !prefs[1].PushEnabled {
		t.Errorf("mixed defaults: got %+v, want heart=false reply=true", prefs)
	}

	// An explicit override wins over the default; the other kind keeps it.
	if err := SetNotificationPreference(ctx, db, "u1", "heart", false, 2000); err != nil {
		t.Fatalf("set heart off: %v", err)
	}
	prefs, err = EffectiveNotificationPreferences(ctx, db, "u1", kinds, alwaysOn)
	if err != nil {
		t.Fatalf("prefs after override: %v", err)
	}
	if prefs[0].PushEnabled || !prefs[1].PushEnabled {
		t.Errorf("override prefs: got %+v, want heart=false reply=true", prefs)
	}

	// The override is repeatable (toggle back on).
	if err := SetNotificationPreference(ctx, db, "u1", "heart", true, 3000); err != nil {
		t.Fatalf("set heart on: %v", err)
	}
	enabled, err := EffectivePushEnabled(ctx, db, "u1", "heart", false)
	if err != nil {
		t.Fatalf("single-kind read: %v", err)
	}
	if !enabled {
		t.Error("heart should be enabled after the repeat override")
	}
	// The single-kind read also serves the default.
	enabled, err = EffectivePushEnabled(ctx, db, "u1", "comment_reply", false)
	if err != nil {
		t.Fatalf("single-kind default read: %v", err)
	}
	if enabled {
		t.Error("comment_reply should keep the default-off fallback")
	}

	// The CHECK rejects an unknown kind (the handler never sends one — the
	// store-side guard).
	if err := SetNotificationPreference(ctx, db, "u1", "bogus", true, 4000); err == nil {
		t.Error("unknown kind must fail the CHECK")
	}
}
