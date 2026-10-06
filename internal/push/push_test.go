package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// Sender tests: the fan-out against a
// fake push service — preference gating, TTL/topic/VAPID headers, dead-
// endpoint deletion, and retry behavior. The payload body is RFC 8291
// ciphertext by design; tests pin that it is non-empty, never its content
// (decrypting would reimplement the library under test).

// TestRecordSizeFor pins the ECE record sizing:
// webpush-go pads the single record up to RecordSize,
// so the value IS the request-body size. The floor keeps small payloads
// padded, the overhead clears the framing, and — the whole point — the
// composed body stays under the ceiling Mozilla autopush enforces for BRIDGED
// (FCM/APNs) connections, whose 413 is what blacked out Firefox for Android.
func TestRecordSizeFor(t *testing.T) {
	t.Parallel()

	if got := recordSizeFor(0); got != minRecordSize {
		t.Errorf("recordSizeFor(0) = %d, want the %d floor", got, minRecordSize)
	}
	if got := recordSizeFor(minRecordSize); got != minRecordSize+recordOverhead {
		t.Errorf("recordSizeFor(%d) = %d, want %d (the floor must not cap a larger payload)", minRecordSize, got, minRecordSize+recordOverhead)
	}
	// The record must have room for the payload plus the framing: the library
	// errors (ErrMaxPadExceeded) when the payload does not fit. The invariant
	// is the constant relation below; the lengths are the binding cases where
	// the floor no longer governs.
	if recordOverhead <= payloadFraming {
		t.Errorf("recordOverhead = %d must exceed payloadFraming = %d, or a maximal payload cannot be framed", recordOverhead, payloadFraming)
	}
	for _, payloadLen := range []int{minRecordSize, 2000} {
		t.Run(fmt.Sprintf("payload %d", payloadLen), func(t *testing.T) {
			if got := int(recordSizeFor(payloadLen)); got < payloadLen+payloadFraming {
				t.Errorf("recordSizeFor(%d) = %d, too small to hold the payload + %d bytes of framing", payloadLen, got, payloadFraming)
			}
		})
	}

	// The worst realistic payload: a 200-rune title, the actor, and both ids —
	// each of them escaping-heavy, because json.Marshal turns every `<` into
	// `\u003c` (six bytes for one). If this ever crossed the bridged ceiling,
	// the composed body would start being rejected on Android again.
	title := strings.Repeat("<", 200)
	actor := strings.Repeat("<", 32)
	body, dropped, err := payloadFor(Event{
		Kind:          "comment_reply",
		ActorUsername: actor,
		ContentTitle:  title,
		ContentKind:   "blog-posts",
		ContentID:     strings.Repeat("<", 64),
		CommentID:     strings.Repeat("<", 64),
	})
	if err != nil {
		t.Fatalf("marshal worst case: %v", err)
	}
	if dropped != 0 {
		t.Errorf("the escaping-heavy worst case dropped %d bytes — the ceiling is too tight for real payloads", dropped)
	}
	if got := int(recordSizeFor(len(body))); got > maxBridgedRecordSize {
		t.Errorf("worst-case body %d exceeds the bridged ceiling %d", got, maxBridgedRecordSize)
	}
	if maxBridgedRecordSize >= 2744 {
		t.Errorf("maxBridgedRecordSize = %d — the ceiling must stay below Mozilla's ~2744-byte bridged limit", maxBridgedRecordSize)
	}

	// An IMPORTED legacy title is unbounded, so a payload can outgrow the
	// ceiling. payloadFor must cut the title to the LONGEST prefix that fits
	// rather than compose a body every push service refuses with 413 (the
	// silent-loss class), keep it valid UTF-8, and report the bytes it dropped.
	huge := strings.Repeat("τίτλος ", 3000)
	body, dropped, err = payloadFor(Event{
		Kind:          "heart",
		ActorUsername: "actor",
		ContentTitle:  huge,
		ContentKind:   "blog-posts",
		ContentID:     "b1",
		CommentID:     "c1",
	})
	if err != nil {
		t.Fatalf("marshal the oversized title: %v", err)
	}
	if dropped == 0 {
		t.Error("an oversized title must report the dropped bytes")
	}
	if got := int(recordSizeFor(len(body))); got > maxBridgedRecordSize {
		t.Errorf("trimmed body %d still exceeds the ceiling %d", got, maxBridgedRecordSize)
	}
	var decoded struct {
		ContentTitle string `json:"contentTitle"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("the trimmed body must stay valid JSON: %v", err)
	}
	if !utf8.ValidString(decoded.ContentTitle) || !strings.HasPrefix(huge, decoded.ContentTitle) {
		t.Errorf("the trimmed title must stay valid UTF-8 and be a prefix of the original, got %q", decoded.ContentTitle[:min(32, len(decoded.ContentTitle))])
	}
	// The cut is the LONGEST prefix that fits, not a coarse guess: one more rune
	// must break the budget again (measured with the raw marshaller — payloadFor
	// would simply cut it back).
	oneMore := Event{
		Kind:          "heart",
		ActorUsername: "actor",
		ContentTitle:  string([]rune(huge)[:len([]rune(decoded.ContentTitle))+1]),
		ContentKind:   "blog-posts",
		ContentID:     "b1",
		CommentID:     "c1",
	}
	grown, err := marshalPayload(oneMore)
	if err != nil {
		t.Fatalf("marshal the one-rune-longer title: %v", err)
	}
	if len(grown) <= maxPayloadBytes {
		t.Errorf("a title one rune longer still fits (%d bytes) — the trim is not maximal", len(grown))
	}
}

// TestPushServiceFamily pins the closed vocabulary the report and the logs
// use. The endpoint PATH and the key material never leave the sender; the
// family is what tells a human which push service refused a device.
func TestPushServiceFamily(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		endpoint string
		want     string
	}{
		{"mozilla current", "https://updates.push.services.mozilla.com/wpush/v2/gAAAA", "mozilla"},
		{"mozilla legacy", "https://updates-push.services.mozaws.net/push/abc", "mozilla"},
		{"fcm", "https://fcm.googleapis.com/fcm/send/abc", "fcm"},
		{"fcm legacy", "https://android.googleapis.com/gcm/send/abc", "fcm"},
		{"apple", "https://web.push.apple.com/abc", "apple"},
		{"other host", "https://push.example.com/e", "other"},
		{"unparseable", "://not a url", "other"},
		{"empty", "", "other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pushServiceFamily(tc.endpoint); got != tc.want {
				t.Errorf("pushServiceFamily(%q) = %q, want %q", tc.endpoint, got, tc.want)
			}
		})
	}
}

// TestTTLSeconds pins the per-kind TTL ladder:
// new_content carries its own 7-day number, every other kind
// keeps the 24 h default.
func TestTTLSeconds(t *testing.T) {
	t.Parallel()

	if got := ttlSeconds("new_content"); got != 604800 {
		t.Errorf("new_content TTL = %d, want 604800", got)
	}
	for _, kind := range []string{"heart", "comment_reply", "comment", "content_updated", "comment_removed", "draft_activity", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			if got := ttlSeconds(kind); got != 86400 {
				t.Errorf("%s TTL = %d, want 86400", kind, got)
			}
		})
	}
}

// TestDeliverSendsNewContentTTL pins the TTL HEADER a new_content fan-out
// actually emits (the fake service captures it) — the per-kind number
// reaches the push service, not just the switch.
func TestDeliverSendsNewContentTTL(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "moderator")

	fake, srv := newFakePushService(t, http.StatusCreated)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", srv.URL)

	sender.deliver(t.Context(), newContentEvent("u1"))
	if n := fake.count(); n != 1 {
		t.Fatalf("deliveries: got %d, want 1", n)
	}
	if ttl := fake.last().ttl; ttl != "604800" {
		t.Errorf("new_content TTL header = %q, want 604800", ttl)
	}
}

// TestDeliverDraftActivityDefaultsOff pins the kind-specific default:
// the SAME moderator account receives a heart (the
// audience kinds default ON for moderator+) but NOT a draft_activity event
// while no preference row exists — the staff-only notice is opt-in at every
// role. The explicit opt-in then flips it, and the other kinds keep working.
func TestDeliverDraftActivityDefaultsOff(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "moderator")

	fake, srv := newFakePushService(t, http.StatusCreated)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", srv.URL)

	sender.deliver(t.Context(), draftActivityEvent("u1"))
	if n := fake.count(); n != 0 {
		t.Fatalf("draft event with no preference row: sent %d, want 0 (the kind is opt-in)", n)
	}
	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != 1 {
		t.Fatalf("heart for the same moderator: sent %d total, want 1 (default ON)", n)
	}

	if err := store.SetNotificationPreference(t.Context(), db, "u1", "draft_activity", true, 2_000); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	sender.deliver(t.Context(), draftActivityEvent("u1"))
	if n := fake.count(); n != 2 {
		t.Fatalf("draft event after opt-in: sent %d total, want 2", n)
	}
}

func TestDeliverSendsWithHeaders(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "user")

	fake, srv := newFakePushService(t, http.StatusCreated)
	sender := newTestSender(t, db)

	// A regular user defaults OFF — no delivery without an explicit opt-in.
	subscribeTo(t, db, "u1", srv.URL)
	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != 0 {
		t.Fatalf("default-off user must not receive pushes, got %d requests", n)
	}

	// Opt in, then the same event delivers once.
	if err := store.SetNotificationPreference(t.Context(), db, "u1", "heart", true, 2000); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != 1 {
		t.Fatalf("opted-in user must receive one push, got %d", n)
	}
	req := fake.last()
	if !strings.HasPrefix(req.authorization, "vapid ") {
		t.Errorf("Authorization must carry the VAPID scheme, got %.30q", req.authorization)
	}
	if req.ttl != "86400" {
		t.Errorf("TTL = %q, want 86400", req.ttl)
	}
	if req.topic != "b1" {
		t.Errorf("Topic = %q, want the content id b1", req.topic)
	}
	if req.bodyLen == 0 {
		t.Error("encrypted payload must be non-empty")
	}
	// The request body IS the ECE record size:
	// leaving RecordSize unset padded every message to 4096 bytes, which
	// Mozilla autopush rejects with 413 for bridged (FCM/APNs) subscriptions —
	// the Firefox-for-Android blackout. Pin the exact size the sender asked
	// for, so a future edit cannot silently reintroduce the default.
	wantBody, _, err := payloadFor(heartEvent("u1"))
	if err != nil {
		t.Fatalf("marshal the expected payload: %v", err)
	}
	if want := int(recordSizeFor(len(wantBody))); req.bodyLen != want {
		t.Errorf("request body = %d bytes, want %d (the record size for a %d-byte payload)", req.bodyLen, want, len(wantBody))
	}
	// The VAPID JWT claims: sub = the contact URI, aud = the push
	// service's origin (derived from the endpoint, RFC 8292).
	claims := vapidClaims(t, req.authorization)
	if claims["sub"] != "https://sickfansubs.com" {
		t.Errorf("VAPID sub = %v, want https://sickfansubs.com", claims["sub"])
	}
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse fake service URL: %v", err)
	}
	if claims["aud"] != u.Scheme+"://"+u.Host {
		t.Errorf("VAPID aud = %v, want %s://%s (the endpoint origin)", claims["aud"], u.Scheme, u.Host)
	}
}

func TestDeliverModeratorDefaultsOn(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "moderator")

	fake, srv := newFakePushService(t, http.StatusCreated)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", srv.URL)

	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != 1 {
		t.Errorf("moderator default-on must receive pushes, got %d", n)
	}

	// An explicit opt-out overrides the default.
	if err := store.SetNotificationPreference(t.Context(), db, "u1", "heart", false, 2000); err != nil {
		t.Fatalf("opt out: %v", err)
	}
	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != 1 {
		t.Errorf("opted-out moderator must receive nothing new, got %d total", n)
	}
}

// TestDeliverSkipsSuspendedRecipient pins the send-time status re-check:
// the emitters already exclude a suspended recipient at commit time, and a
// window between a commit and this fan-out (or a retry) must not deliver
// either — suspension is a full revocation.
func TestDeliverSkipsSuspendedRecipient(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "moderator")
	if _, err := db.Exec(`UPDATE users SET status = 'suspended' WHERE id = 'u1'`); err != nil {
		t.Fatalf("suspend recipient: %v", err)
	}

	fake, srv := newFakePushService(t, http.StatusCreated)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", srv.URL)

	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != 0 {
		t.Errorf("suspended recipient received %d pushes, want 0", n)
	}

	// The rows were RETAINED, not deleted: reactivation resumes delivery.
	if _, err := db.Exec(`UPDATE users SET status = 'active' WHERE id = 'u1'`); err != nil {
		t.Fatalf("reactivate recipient: %v", err)
	}
	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != 1 {
		t.Errorf("reactivated recipient received %d total pushes, want 1", n)
	}
}

func TestDeliverDeletesDeadEndpoint(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "moderator")

	fake, srv := newFakePushService(t, http.StatusGone)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", srv.URL)

	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != 1 {
		t.Fatalf("deliver attempts = %d, want one", n)
	}
	subs, err := store.PushSubscriptionsForUser(t.Context(), db, "u1")
	if err != nil {
		t.Fatalf("list subs: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("410 must delete the subscription row, got %d left", len(subs))
	}
}

func TestDeliverRetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "moderator")

	fake, srv := newFakePushService(t, http.StatusTooManyRequests, http.StatusCreated)
	sender := newTestSender(t, db)
	sender.backoff = func(int) time.Duration { return 0 } // pin the retry COUNT, not the sleep
	subscribeTo(t, db, "u1", srv.URL)

	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != 2 {
		t.Fatalf("429 then 201 must take exactly 2 attempts, got %d", n)
	}
	subs, err := store.PushSubscriptionsForUser(t.Context(), db, "u1")
	if err != nil {
		t.Fatalf("list subs: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("a retried-then-successful send must keep the row, got %d", len(subs))
	}
}

func TestDeliverGivesUpOnPersistent5xx(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "moderator")

	fake, srv := newFakePushService(t, http.StatusInternalServerError)
	sender := newTestSender(t, db)
	sender.backoff = func(int) time.Duration { return 0 } // pin the retry COUNT, not the sleep
	subscribeTo(t, db, "u1", srv.URL)

	sender.deliver(t.Context(), heartEvent("u1"))
	if n := fake.count(); n != maxAttempts {
		t.Errorf("persistent 5xx must stop after maxAttempts (%d), got %d", maxAttempts, n)
	}
	subs, err := store.PushSubscriptionsForUser(t.Context(), db, "u1")
	if err != nil {
		t.Fatalf("list subs: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("a 5xx is not a dead endpoint — the row must survive, got %d", len(subs))
	}
}

func TestNotifyPushIgnoresEmptyEvent(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "moderator")

	fake, srv := newFakePushService(t, http.StatusCreated)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", srv.URL)

	sender.NotifyPush(t.Context(), Event{})
	// The goroutine spawns only for a real event; an empty event returns
	// before any work — nothing may ever reach the service.
	if n := fake.count(); n != 0 {
		t.Errorf("empty event must not deliver, got %d requests", n)
	}
}

// TestDefaultBackoffLadder pins the PRODUCTION retry delays without
// sleeping: the 1 s/2 s ladder is part of the delivery contract — the
// retry tests inject a zero backoff and this
// test keeps the default honest.
func TestDefaultBackoffLadder(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	sender := newTestSender(t, db)
	if got := sender.backoff(1); got != time.Second {
		t.Errorf("backoff(1) = %v, want 1s", got)
	}
	if got := sender.backoff(2); got != 2*time.Second {
		t.Errorf("backoff(2) = %v, want 2s", got)
	}
}

// TestPayloadJSONKeys pins the message-body key names — the service worker
// reads them by name, and a casing drift (contentID vs contentId) would
// ship a silent break behind the ciphertext.
func TestPayloadJSONKeys(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(payload{
		Kind:          "heart",
		ActorUsername: "actor",
		ContentTitle:  "title",
		ContentKind:   "blog-posts",
		ContentID:     "b1",
		CommentID:     "c1",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var keys map[string]any
	if err := json.Unmarshal(body, &keys); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"kind", "actorUsername", "contentTitle", "contentKind", "contentId", "commentId"}
	if len(keys) != len(want) {
		t.Fatalf("payload keys: got %v, want %v", keys, want)
	}
	for _, k := range want {
		if _, ok := keys[k]; !ok {
			t.Errorf("payload must carry the key %q (the worker's contract)", k)
		}
	}
}

// TestPayloadDraftAction pins the draft notice's own key by
// marshalling an EVENT, not a payload literal: the transfer from
// Event.DraftAction into the body is what the worker depends on, so a dropped
// assignment must fail here. Every other kind OMITS the key — the worker's
// unknown-action arm and the sentence switch both key off its presence.
func TestPayloadDraftAction(t *testing.T) {
	t.Parallel()

	body, err := marshalPayload(draftActivityEvent("u1"))
	if err != nil {
		t.Fatalf("marshal draft_activity: %v", err)
	}
	var keys map[string]any
	if err := json.Unmarshal(body, &keys); err != nil {
		t.Fatalf("unmarshal draft_activity: %v", err)
	}
	if keys["draftAction"] != "created" {
		t.Errorf("draftAction = %v, want the event's action (body %s)", keys["draftAction"], body)
	}
	for _, kind := range []string{"heart", "comment_reply", "comment", "content_updated", "new_content", "comment_removed"} {
		body, err := json.Marshal(payload{Kind: kind, ContentID: "b1"})
		if err != nil {
			t.Fatalf("marshal %s: %v", kind, err)
		}
		// A fresh map per kind: unmarshalling into the map above would MERGE
		// keys and make this pin vacuous.
		var other map[string]any
		if err := json.Unmarshal(body, &other); err != nil {
			t.Fatalf("unmarshal %s: %v", kind, err)
		}
		if _, ok := other["draftAction"]; ok {
			t.Errorf("%s payload must omit draftAction, got %s", kind, body)
		}
	}
}

// TestPayloadOmitsEmptyCommentID pins the omission rule for the content
// kinds:
// content_updated and new_content carry no comment, so the wire body must
// NOT carry a commentId key at all — the worker's deep-link logic keys off
// its absence.
func TestPayloadOmitsEmptyCommentID(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"content_updated", "new_content"} {
		body, err := json.Marshal(payload{
			Kind:          kind,
			ActorUsername: "actor",
			ContentTitle:  "title",
			ContentKind:   "blog-posts",
			ContentID:     "b1",
		})
		if err != nil {
			t.Fatalf("marshal %s: %v", kind, err)
		}
		var keys map[string]any
		if err := json.Unmarshal(body, &keys); err != nil {
			t.Fatalf("unmarshal %s: %v", kind, err)
		}
		if _, ok := keys["commentId"]; ok {
			t.Errorf("%s payload must omit commentId, got %s", kind, body)
		}
		if keys["contentId"] != "b1" {
			t.Errorf("%s contentId = %v, want b1", kind, keys["contentId"])
		}
	}
}

// TestPayloadTestKindBody pins the self-test payload's wire shape: the kind key the worker switches
// on, and empty strings for every content field — never a commentId key. The
// worker renders the sentence from its own template and links to /account,
// so it must tolerate the empty title/actor (a future refactor that omits
// the keys instead would break that contract silently).
func TestPayloadTestKindBody(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(payload{Kind: testKind})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var keys map[string]any
	if err := json.Unmarshal(body, &keys); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(keys) != 5 {
		t.Fatalf("payload keys: got %v, want the five content keys (no commentId)", keys)
	}
	if keys["kind"] != "test" {
		t.Errorf("kind = %v, want test", keys["kind"])
	}
	for _, k := range []string{"actorUsername", "contentTitle", "contentKind", "contentId"} {
		if v, ok := keys[k]; !ok || v != "" {
			t.Errorf("%s = %v (present: %v), want the empty string", k, v, ok)
		}
	}
	if _, ok := keys["commentId"]; ok {
		t.Errorf("the test payload must omit commentId, got %s", body)
	}
}

// TestDeliverCapsConcurrency pins the fan-out cap:
// eight subscriptions, one slow fake service — the peak in-flight count
// must never exceed concurrencyCap while every message still lands.
func TestDeliverCapsConcurrency(t *testing.T) {
	t.Parallel()
	db := pushFixture(t)
	seedUser(t, db, "u1", "moderator")

	var (
		inFlight atomic.Int64
		peak     atomic.Int64
		total    atomic.Int64
	)
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := inFlight.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		inFlight.Add(-1)
		total.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	sender := newTestSender(t, db)
	for i := range 8 {
		subscribeTo(t, db, "u1", srv.URL+"/device-"+string(rune('a'+i)))
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		sender.deliver(t.Context(), heartEvent("u1"))
	}()

	// Every held handler reports entry; with the cap reached no further send
	// can start, so the peak is observable instead of merely probable.
	for i := range concurrencyCap {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d sends entered the handler before the deadline", i, concurrencyCap)
		}
	}
	if got := peak.Load(); got > concurrencyCap {
		t.Errorf("peak in-flight = %d, must not exceed the cap %d", got, concurrencyCap)
	}
	released = true
	close(release)
	<-done

	if got := total.Load(); got != 8 {
		t.Fatalf("delivered %d messages, want 8", got)
	}
	if got := peak.Load(); got > concurrencyCap {
		t.Errorf("peak in-flight = %d, must not exceed the cap %d", got, concurrencyCap)
	}
}

// TestSendTestDeliversToOwnDevices pins the self-test path:
// one message per subscribed device of THAT user, the accepted
// count as the result, and NO preference gate — every kind is explicitly
// switched off below, and the test must still go out (the user asked
// for this message).
func TestSendTestDeliversToOwnDevices(t *testing.T) {
	t.Parallel()

	db := pushFixture(t)
	seedUser(t, db, "u1", "user")
	seedUser(t, db, "u2", "user")
	fake, srv := newFakePushService(t, http.StatusCreated)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", srv.URL+"/a")
	subscribeTo(t, db, "u1", srv.URL+"/b")
	subscribeTo(t, db, "u2", srv.URL+"/other")

	for _, kind := range []string{"heart", "comment_reply", "comment", "content_updated", "new_content", "comment_removed"} {
		if err := store.SetNotificationPreference(t.Context(), db, "u1", kind, false, 1000); err != nil {
			t.Fatalf("disable %s: %v", kind, err)
		}
	}

	report, err := sender.SendTest(t.Context(), "u1")
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if report.Delivered != 2 {
		t.Errorf("delivered = %d, want 2 (only this user's devices)", report.Delivered)
	}
	if len(report.Endpoints) != 2 {
		t.Fatalf("endpoints = %d, want 2 — one outcome per attempted device", len(report.Endpoints))
	}
	for i, o := range report.Endpoints {
		if !o.Accepted || o.Status != http.StatusCreated {
			t.Errorf("endpoint %d = %+v, want accepted with status 201", i, o)
		}
		if o.SubscriptionID == "" {
			t.Errorf("endpoint %d carries no subscription id", i)
		}
		// The fake service is an httptest server — no known push service, so
		// the family is the honest "other" rather than a guess.
		if o.Service != serviceOther {
			t.Errorf("endpoint %d service = %q, want %q", i, o.Service, serviceOther)
		}
	}
	if got := fake.count(); got != 2 {
		t.Errorf("push service saw %d requests, want 2", got)
	}
	// Every request, not just the last: the test rides the reaction-class TTL
	// and carries no topic (there is no content id to collapse on) — and its
	// body is the composed record size, never the library's 4096-byte default
	// (the Firefox-for-Android blackout).
	wantTestBody, _, err := payloadFor(Event{Kind: testKind})
	if err != nil {
		t.Fatalf("marshal the self-test payload: %v", err)
	}
	for i, req := range fake.snapshot() {
		if req.ttl != "86400" {
			t.Errorf("request %d TTL = %q, want the 24 h default", i, req.ttl)
		}
		if req.topic != "" {
			t.Errorf("request %d topic = %q, want none", i, req.topic)
		}
		if want := int(recordSizeFor(len(wantTestBody))); req.bodyLen != want {
			t.Errorf("request %d body = %d bytes, want %d (the record size, not the library default)", i, req.bodyLen, want)
		}
	}

	// No feed row, no preference write, no audit row:
	// the endpoint is not an event.
	var feedRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_notifications WHERE recipient_id = 'u1'`).Scan(&feedRows); err != nil {
		t.Fatalf("count feed rows: %v", err)
	}
	if feedRows != 0 {
		t.Errorf("user_notifications rows = %d, want 0", feedRows)
	}
	var auditRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE actor_id = 'u1'`).Scan(&auditRows); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditRows != 0 {
		t.Errorf("audit_events rows = %d, want 0", auditRows)
	}
}

// TestSendTestCountsOnlyAccepted: a rejected device (a 4xx that is not
// 404/410) does not count as delivered, and does not fail the call — the
// result reports how many endpoints ACCEPTED the message, and which of them
// was the refusal (a healthy device must not hide the refused one).
func TestSendTestCountsOnlyAccepted(t *testing.T) {
	t.Parallel()

	db := pushFixture(t)
	seedUser(t, db, "u1", "user")
	// One fake service per verdict: each subscription then has a deterministic
	// answer, so the assertion never depends on scan order.
	_, acceptedSrv := newFakePushService(t, http.StatusCreated)
	_, refusedSrv := newFakePushService(t, http.StatusBadRequest)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", acceptedSrv.URL+"/ok")
	subscribeTo(t, db, "u1", refusedSrv.URL+"/rejected")

	report, err := sender.SendTest(t.Context(), "u1")
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if report.Delivered != 1 {
		t.Errorf("delivered = %d, want 1 accepted", report.Delivered)
	}

	// Map the outcomes by the row ids the store holds, so the assertion never
	// depends on scan order.
	subs, err := store.PushSubscriptionsForUser(t.Context(), db, "u1")
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	byEndpoint := map[string]string{} // endpoint → row id
	for _, s := range subs {
		byEndpoint[s.Endpoint] = s.ID
	}
	outcomes := map[string]DeliveryOutcome{}
	for _, o := range report.Endpoints {
		outcomes[o.SubscriptionID] = o
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %d, want 2 distinct rows", len(outcomes))
	}
	ok := outcomes[byEndpoint[acceptedSrv.URL+"/ok"]]
	if !ok.Accepted || ok.Status != http.StatusCreated {
		t.Errorf("the accepted endpoint = %+v, want accepted 201", ok)
	}
	refused := outcomes[byEndpoint[refusedSrv.URL+"/rejected"]]
	if refused.Accepted || refused.Status != http.StatusBadRequest {
		t.Errorf("the refused endpoint = %+v, want refused 400", refused)
	}
	if refused.Removed {
		t.Errorf("a 400 must not delete the row: %+v", refused)
	}
}

// TestSendTestCleansDeadEndpoints: the self-test rides the same send path,
// so a 410 removes the row exactly as a fan-out would — and the cleanup
// stays scoped to the CALLER: a second user's subscription must survive.
func TestSendTestCleansDeadEndpoints(t *testing.T) {
	t.Parallel()

	db := pushFixture(t)
	seedUser(t, db, "u1", "user")
	seedUser(t, db, "u2", "user")
	_, srv := newFakePushService(t, http.StatusGone)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", srv.URL+"/dead")
	subscribeTo(t, db, "u2", srv.URL+"/other")

	report, err := sender.SendTest(t.Context(), "u1")
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if report.Delivered != 0 {
		t.Errorf("delivered = %d, want 0 (the endpoint was gone)", report.Delivered)
	}
	if len(report.Endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(report.Endpoints))
	}
	// The dead endpoint is reported as REMOVED — the settings list uses this
	// to explain why the device disappeared.
	if got := report.Endpoints[0]; !got.Removed || got.Accepted || got.Status != http.StatusGone {
		t.Errorf("outcome = %+v, want removed on 410", got)
	}
	subs, err := store.PushSubscriptionsForUser(t.Context(), db, "u1")
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("dead subscription survived: %d rows", len(subs))
	}
	other, err := store.PushSubscriptionsForUser(t.Context(), db, "u2")
	if err != nil {
		t.Fatalf("list the other user's subscriptions: %v", err)
	}
	if len(other) != 1 {
		t.Errorf("the other user's rows = %d, want 1 — the cleanup must be user-scoped", len(other))
	}
}

// TestSendTestHonoursACancelledContext: the send runs inside the caller's
// request, so a client that goes away stops it before any delivery. The
// cancellation surfaces as the store error — the caller is gone, so nothing
// reads the result; the point of the pin is that NO message was accepted.
func TestSendTestHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	db := pushFixture(t)
	seedUser(t, db, "u1", "user")
	fake, srv := newFakePushService(t, http.StatusCreated)
	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", srv.URL+"/a")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	report, err := sender.SendTest(ctx, "u1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the context cancellation to propagate", err)
	}
	if report.Delivered != 0 {
		t.Errorf("delivered = %d, want 0 on a cancelled context", report.Delivered)
	}
	if got := fake.count(); got != 0 {
		t.Errorf("push service saw %d requests, want 0", got)
	}
}

// TestSendTestReportOrder pins that the report follows the store's
// `created_at_ms, id` order: the OpenAPI
// promises creation order and the client matches by id, so the guarantee must
// be observable, not incidental.
func TestSendTestReportOrder(t *testing.T) {
	t.Parallel()

	db := pushFixture(t)
	seedUser(t, db, "u1", "user")

	// Two devices, oldest first, each with its own real key material (the
	// encryption path validates the EC point before any request leaves).
	newKeyMaterial := func() (string, string) {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate subscription key: %v", err)
		}
		p256dh := base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), priv.PublicKey.X, priv.PublicKey.Y))
		authBytes := make([]byte, 16)
		if _, err := rand.Read(authBytes); err != nil {
			t.Fatalf("generate auth secret: %v", err)
		}
		return p256dh, base64.RawURLEncoding.EncodeToString(authBytes)
	}

	_, srv := newFakePushService(t, http.StatusCreated)
	for _, row := range []struct {
		id        string
		createdAt int64
	}{
		{"sub-older", 1000},
		{"sub-newer", 2000},
	} {
		p256dh, auth := newKeyMaterial()
		if _, err := store.UpsertPushSubscription(t.Context(), db,
			row.id, "u1", srv.URL+"/"+row.id, p256dh, auth, row.createdAt); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}

	sender := newTestSender(t, db)
	report, err := sender.SendTest(t.Context(), "u1")
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if len(report.Endpoints) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(report.Endpoints))
	}
	if report.Endpoints[0].SubscriptionID != "sub-older" || report.Endpoints[1].SubscriptionID != "sub-newer" {
		t.Errorf("report order = [%s %s], want [sub-older sub-newer] (created_at_ms ASC)",
			report.Endpoints[0].SubscriptionID, report.Endpoints[1].SubscriptionID)
	}
}

// TestSendOneLogsByClassWithoutEndpoint: the endpoint is capability material;
// a network failure must surface as the class only, with the
// log-discipline field names and no endpoint text.
func TestSendOneLogsByClassWithoutEndpoint(t *testing.T) {
	t.Parallel()

	db := pushFixture(t)
	seedUser(t, db, "u1", "user")
	if err := store.SetNotificationPreference(t.Context(), db, "u1", "heart", true, 2000); err != nil {
		t.Fatalf("opt in: %v", err)
	}
	const endpoint = "http://127.0.0.1:1/unreachable"
	subscribeTo(t, db, "u1", endpoint)

	sender := newTestSender(t, db)
	sender.backoff = func(int) time.Duration { return 0 }

	var buf bytes.Buffer
	logCtx := logging.With(t.Context(), slog.New(slog.NewJSONHandler(&buf, nil)))
	sender.deliver(logCtx, heartEvent("u1"))

	record := map[string]any{}
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		var candidate map[string]any
		if err := json.Unmarshal([]byte(line), &candidate); err != nil {
			t.Fatalf("decode log record %q: %v", line, err)
		}
		if candidate["class"] == "network" {
			record = candidate
		}
	}
	if len(record) == 0 {
		t.Fatalf("no network-class record in %s", buf.String())
	}
	if got, _ := record["subscriptionId"].(string); got == "" {
		t.Errorf("subscriptionId missing from the record: %v", record)
	}
	for key, value := range record {
		if s, ok := value.(string); ok && strings.Contains(s, "127.0.0.1:1") {
			t.Errorf("field %q carries the endpoint: %q", key, s)
		}
	}
}

func TestPushErrorClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"network", errors.New("dial tcp: connection refused"), "network"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pushErrorClass(tc.err); got != tc.want {
				t.Errorf("pushErrorClass(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestSendOneRefusesRedirects: a user-supplied endpoint must not be able to
// aim the send at a second host. A 307 stays a 3xx — the redirect target is
// never contacted and the report reads the refusal.
func TestSendOneRefusesRedirects(t *testing.T) {
	t.Parallel()

	db := pushFixture(t)
	seedUser(t, db, "u1", "user")

	target, targetSrv := newFakePushService(t, http.StatusCreated)
	var redirectHits atomic.Int32
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectHits.Add(1)
		http.Redirect(w, r, targetSrv.URL+"/internal", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	sender := newTestSender(t, db)
	subscribeTo(t, db, "u1", redirect.URL+"/endpoint")

	report, err := sender.SendTest(t.Context(), "u1")
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if report.Delivered != 0 {
		t.Errorf("delivered = %d, want 0 (a redirect is not a delivery)", report.Delivered)
	}
	if len(report.Endpoints) != 1 || report.Endpoints[0].Status != http.StatusTemporaryRedirect {
		t.Fatalf("outcomes = %+v, want one 307 refusal", report.Endpoints)
	}
	if got := redirectHits.Load(); got != 1 {
		t.Errorf("the endpoint saw %d attempts, want exactly 1 (a 3xx must not retry)", got)
	}
	if got := target.count(); got != 0 {
		t.Errorf("the redirect target saw %d requests, want 0", got)
	}
}

// TestSendTestWithoutSubscriptions: nothing subscribed is a quiet zero, not
// an error, and nothing is sent.
func TestSendTestWithoutSubscriptions(t *testing.T) {
	t.Parallel()

	db := pushFixture(t)
	seedUser(t, db, "u1", "user")
	fake, _ := newFakePushService(t, http.StatusCreated)
	sender := newTestSender(t, db)

	report, err := sender.SendTest(t.Context(), "u1")
	if err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if report.Delivered != 0 {
		t.Errorf("delivered = %d, want 0", report.Delivered)
	}
	// Never nil: the handler iterates the list and the wire shape is an
	// EMPTY ARRAY, not null.
	if report.Endpoints == nil || len(report.Endpoints) != 0 {
		t.Errorf("endpoints = %v, want an empty non-nil list", report.Endpoints)
	}
	if got := fake.count(); got != 0 {
		t.Errorf("push service saw %d requests, want 0", got)
	}
}
