package store

import (
	"errors"
	"sync"
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
	id, err := UpsertPushSubscription(ctx, db, "s1", "u1", fakeEndpointA, fakeP256dh, fakeAuth, "Chrome · Android", 1000)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if id != "s1" {
		t.Errorf("fresh insert id: got %q, want %q", id, "s1")
	}

	// Same-endpoint resubscribe updates in place and returns the STORED id
	// (the minted id is discarded). A DIFFERENT derived label must not
	// overwrite the stored one — a rename (or an earlier capture) is kept.
	id, err = UpsertPushSubscription(ctx, db, "s-new", "u1", fakeEndpointA, "BBBB", "CCCC", "Chrome · Windows", 2000)
	if err != nil {
		t.Fatalf("resubscribe: %v", err)
	}
	if id != "s1" {
		t.Errorf("resubscribe id: got %q, want %q (the existing row's id)", id, "s1")
	}
	var userID, p256dh, auth, label string
	var updatedAt int64
	if err := db.QueryRow(`SELECT user_id, p256dh, auth, COALESCE(label, ''), updated_at_ms FROM push_subscriptions WHERE endpoint = ?`, fakeEndpointA).
		Scan(&userID, &p256dh, &auth, &label, &updatedAt); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if userID != "u1" || p256dh != "BBBB" || auth != "CCCC" || updatedAt != 2000 {
		t.Errorf("row after resubscribe: user=%s p256dh=%s auth=%s updated=%d", userID, p256dh, auth, updatedAt)
	}
	if label != "Chrome · Android" {
		t.Errorf("resubscribe label: got %q, want the preserved %q", label, "Chrome · Android")
	}

	// A NULL label is FILLED by the incoming derived one (the first capture
	// may arrive later than the row — an unrecognized first User-Agent).
	if err := RenamePushSubscription(ctx, db, "u1", "s1", ""); err != nil {
		t.Fatalf("clear label: %v", err)
	}
	if _, err := UpsertPushSubscription(ctx, db, "s-x", "u1", fakeEndpointA, "BBBB", "CCCC", "Firefox · Linux", 2500); err != nil {
		t.Fatalf("resubscribe with null label: %v", err)
	}
	if err := db.QueryRow(`SELECT COALESCE(label, '') FROM push_subscriptions WHERE endpoint = ?`, fakeEndpointA).Scan(&label); err != nil {
		t.Fatalf("read filled label: %v", err)
	}
	if label != "Firefox · Linux" {
		t.Errorf("null label fill: got %q, want %q", label, "Firefox · Linux")
	}

	// A different user subscribing the same endpoint through the TAKEOVER
	// form reassigns it — with the NEW user's label.
	id, err = UpsertPushSubscription(ctx, db, "s2", "u2", fakeEndpointA, fakeP256dh, fakeAuth, "Chrome · Windows", 3000)
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
	var reassignedLabel string
	if err := db.QueryRow(`SELECT COALESCE(label, '') FROM push_subscriptions WHERE endpoint = ?`, fakeEndpointA).Scan(&reassignedLabel); err != nil {
		t.Fatalf("read reassigned label: %v", err)
	}
	if reassignedLabel != "Chrome · Windows" {
		t.Errorf("reassigned label: got %q, want the new user's %q", reassignedLabel, "Chrome · Windows")
	}

	// A deleted user's insert maps to ErrNotFound (the FK).
	if _, err := UpsertPushSubscription(ctx, db, "s3", "ghost", fakeEndpointB, fakeP256dh, fakeAuth, "", 4000); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted user: got %v, want ErrNotFound", err)
	}
}

func TestUpsertPushSubscriptionForSession_RejectsRevokedSession(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")

	digest, csrf := make([]byte, 32), make([]byte, 32)
	digest[0] = 1
	mustCreateSession(t, db, "sess1", "u1", digest, csrf, 1, 1000, 5000)

	// A live session writes normally.
	if _, err := UpsertPushSubscriptionForSession(ctx, db, "s1", "u1", "sess1", fakeEndpointA, fakeP256dh, fakeAuth, "", 1000); err != nil {
		t.Fatalf("live session upsert: %v", err)
	}

	// An expired/revoked session must not write a row.
	if _, err := UpsertPushSubscriptionForSession(ctx, db, "s2", "u1", "sess1", fakeEndpointB, fakeP256dh, fakeAuth, "", 6000); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session: got %v, want ErrNotFound", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE endpoint = ?`, fakeEndpointB).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 0 {
		t.Errorf("expired session wrote %d rows, want 0", n)
	}

	// A session owned by another user must not write for this user.
	if _, err := UpsertPushSubscriptionForSession(ctx, db, "s3", "u2", "sess1", fakeEndpointB, fakeP256dh, fakeAuth, "", 1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign session: got %v, want ErrNotFound", err)
	}
}

// TestUpsertPushSubscriptionNoTakeover_RefusesForeignEndpoint pins the
// background-heal contract: a write that did not ask for an explicit
// takeover may not move another account's endpoint, and the conditional
// write leaves that row untouched.
func TestUpsertPushSubscriptionNoTakeover_RefusesForeignEndpoint(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")

	if _, err := UpsertPushSubscription(ctx, db, "s1", "u1", fakeEndpointA, fakeP256dh, fakeAuth, "Chrome · Android", 1000); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := UpsertPushSubscriptionNoTakeover(ctx, db, "s2", "u2", fakeEndpointA, "BBBB", "CCCC", "Firefox · Linux", 2000)
	if !errors.Is(err, ErrPushEndpointOwned) {
		t.Fatalf("foreign endpoint: err = %v, want ErrPushEndpointOwned", err)
	}

	var owner, p256dh, label string
	var updatedAt int64
	if err := db.QueryRow(`SELECT user_id, p256dh, COALESCE(label, ''), updated_at_ms FROM push_subscriptions WHERE endpoint = ?`, fakeEndpointA).
		Scan(&owner, &p256dh, &label, &updatedAt); err != nil {
		t.Fatalf("read row after refusal: %v", err)
	}
	if owner != "u1" || p256dh != fakeP256dh || label != "Chrome · Android" || updatedAt != 1000 {
		t.Errorf("row after refusal = {%q %q %q %d}, want the untouched u1 row", owner, p256dh, label, updatedAt)
	}

	// The same-owner resubscribe and a fresh endpoint still pass the strict
	// form — the refusal is only about claiming another account's row.
	if id, err := UpsertPushSubscriptionNoTakeover(ctx, db, "s-new", "u1", fakeEndpointA, "BBBB", "CCCC", "", 3000); err != nil || id != "s1" {
		t.Errorf("own resubscribe = (%q, %v), want (\"s1\", nil)", id, err)
	}
	if id, err := UpsertPushSubscriptionNoTakeover(ctx, db, "s3", "u2", fakeEndpointB, fakeP256dh, fakeAuth, "", 4000); err != nil || id != "s3" {
		t.Errorf("fresh endpoint = (%q, %v), want (\"s3\", nil)", id, err)
	}
}

// TestUpsertPushSubscriptionNoTakeover_ConcurrentClaim pins the
// serialization the strict form relies on: two accounts racing for the same
// fresh endpoint produce one owner and one refusal, never two claimers — and
// the stored key material is the winner's.
func TestUpsertPushSubscriptionNoTakeover_ConcurrentClaim(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")

	type attempt struct {
		id, userID, p256dh, auth string
	}
	attempts := []attempt{
		{"s1", "u1", "AAAA", "BBBB"},
		{"s2", "u2", "CCCC", "DDDD"},
	}
	type result struct {
		attempt attempt
		err     error
	}
	results := make(chan result, len(attempts))
	var wg sync.WaitGroup
	for _, a := range attempts {
		wg.Go(func() {
			_, err := UpsertPushSubscriptionNoTakeover(ctx, db, a.id, a.userID, fakeEndpointA, a.p256dh, a.auth, "", 1000)
			results <- result{attempt: a, err: err}
		})
	}
	wg.Wait()
	close(results)

	winner := attempt{}
	refused := 0
	for res := range results {
		switch {
		case res.err == nil:
			if winner.userID != "" {
				t.Fatalf("both accounts claimed the endpoint: %q and %q", winner.userID, res.attempt.userID)
			}
			winner = res.attempt
		case errors.Is(res.err, ErrPushEndpointOwned):
			refused++
		default:
			t.Fatalf("unexpected error: %v", res.err)
		}
	}
	if winner.userID == "" || refused != 1 {
		t.Fatalf("winner = %+v, refused = %d; want one claim and one refusal", winner, refused)
	}

	var owner, p256dh, auth string
	var rows int
	if err := db.QueryRow(`SELECT user_id, p256dh, auth, COUNT(*) FROM push_subscriptions WHERE endpoint = ?`, fakeEndpointA).
		Scan(&owner, &p256dh, &auth, &rows); err != nil {
		t.Fatalf("read endpoint: %v", err)
	}
	if rows != 1 || owner != winner.userID || p256dh != winner.p256dh || auth != winner.auth {
		t.Errorf("stored row = {%q %q %q, %d rows}, want the winner's {%q %q %q, one row}",
			owner, p256dh, auth, rows, winner.userID, winner.p256dh, winner.auth)
	}
}

// TestUpsertPushSubscription_DuplicateIDMapsToErrDuplicate pins the id
// contract: an endpoint conflict is absorbed by the upsert, while a
// primary-key collision surfaces as the sentinel the handler retries.
func TestUpsertPushSubscription_DuplicateIDMapsToErrDuplicate(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")

	if _, err := UpsertPushSubscription(ctx, db, "s1", "u1", fakeEndpointA, fakeP256dh, fakeAuth, "", 1000); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	_, err := UpsertPushSubscription(ctx, db, "s1", "u1", fakeEndpointB, fakeP256dh, fakeAuth, "", 2000)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate id: got %v, want ErrDuplicate", err)
	}
}

func TestRenamePushSubscription(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")

	if _, err := UpsertPushSubscription(ctx, db, "s1", "u1", fakeEndpointA, fakeP256dh, fakeAuth, "", 1000); err != nil {
		t.Fatalf("seed own: %v", err)
	}
	if _, err := UpsertPushSubscription(ctx, db, "s2", "u2", fakeEndpointB, fakeP256dh, fakeAuth, "", 1000); err != nil {
		t.Fatalf("seed foreign: %v", err)
	}

	// A NULL-label row renames to the user's name; the list projects it.
	if err := RenamePushSubscription(ctx, db, "u1", "s1", "Το κινητό"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	subs, err := ListPushSubscriptions(ctx, db, "u1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 || subs[0].Label != "Το κινητό" {
		t.Errorf("list after rename: got %+v, want the renamed label", subs)
	}
	// A rename touches only the label: updated_at_ms tracks the subscription's
	// last refresh, not the row's edits.
	var updatedAt int64
	if err := db.QueryRow(`SELECT updated_at_ms FROM push_subscriptions WHERE id = 's1'`).Scan(&updatedAt); err != nil {
		t.Fatalf("read updated_at_ms: %v", err)
	}
	if updatedAt != 1000 {
		t.Errorf("rename touched updated_at_ms: got %d, want 1000", updatedAt)
	}

	// Rewriting a label replaces it; clearing it stores SQL NULL (empty on the
	// projection).
	if err := RenamePushSubscription(ctx, db, "u1", "s1", "Το tablet"); err != nil {
		t.Fatalf("second rename: %v", err)
	}
	if err := RenamePushSubscription(ctx, db, "u1", "s1", ""); err != nil {
		t.Fatalf("clear rename: %v", err)
	}
	var nullLabel int
	if err := db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE id = 's1' AND label IS NULL`).Scan(&nullLabel); err != nil {
		t.Fatalf("count null label: %v", err)
	}
	if nullLabel != 1 {
		t.Error("an empty rename must store SQL NULL, not an empty string")
	}

	// Unknown and foreign ids answer ErrNotFound and change nothing.
	if err := RenamePushSubscription(ctx, db, "u1", "ghost", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}
	if err := RenamePushSubscription(ctx, db, "u1", "s2", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign id: got %v, want ErrNotFound", err)
	}
	var foreignLabel int
	if err := db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE id = 's2' AND label IS NOT NULL`).Scan(&foreignLabel); err != nil {
		t.Fatalf("count foreign label: %v", err)
	}
	if foreignLabel != 0 {
		t.Error("a foreign id must not touch the row")
	}
}

func TestListAndDeletePushSubscriptions(t *testing.T) {
	t.Parallel()
	db := openStoreDB(t)
	ctx := t.Context()
	mustCreateUser(t, db, "u1", "alice", "alice", "alice@example.com")
	mustCreateUser(t, db, "u2", "bob", "bob", "bob@example.com")

	if _, err := UpsertPushSubscription(ctx, db, "s1", "u1", fakeEndpointA, fakeP256dh, fakeAuth, "", 1000); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if _, err := UpsertPushSubscription(ctx, db, "s2", "u2", fakeEndpointB, fakeP256dh, fakeAuth, "", 1000); err != nil {
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
