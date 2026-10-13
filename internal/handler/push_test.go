package handler_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// Push endpoint handler tests. The
// handlers read the session from the request context (the notifications
// precedent) — the middleware chain is covered by the routes suite.

func setupPushHandlers(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	db, err := database.Open(database.Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	for _, u := range []storetest.UserSpec{
		{ID: "viewer", Username: "Viewer", Role: "moderator"},
		{ID: "plain", Username: "Plain"},
	} {
		storetest.InsertUser(t, db, u)
	}

	mux := http.NewServeMux()
	now := func() time.Time { return time.Unix(5, 0) }
	mux.HandleFunc("POST /api/v1/push-subscriptions", handler.PushSubscriptionCreate(db, now))
	mux.HandleFunc("GET /api/v1/push-subscriptions", handler.PushSubscriptionsList(db))
	mux.HandleFunc("DELETE /api/v1/push-subscriptions/{id}", handler.PushSubscriptionDelete(db))
	mux.HandleFunc("PATCH /api/v1/push-subscriptions/{id}", handler.PushSubscriptionRename(db))
	mux.HandleFunc("GET /api/v1/notification-preferences", handler.NotificationPreferencesGet(db))
	mux.HandleFunc("PUT /api/v1/notification-preferences/{kind}", handler.NotificationPreferenceSet(db, now))
	return db, logContext(nil, mux)
}

// pushAuthedReq builds a request with a synthetic session — the viewer is
// "viewer" (moderator) or "plain" (user).
func pushAuthedReq(method, path, userID, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if userID != "" {
		role := "moderator"
		if userID == "plain" {
			role = "user"
		}
		req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
			SessionID: "s1",
			UserID:    userID,
			Username:  userID,
			Role:      role,
		}))
	}
	return req
}

// realSubscriptionBody returns a body with real-shaped key material.
func realSubscriptionBody() string {
	// Zero-filled bytes of the right LENGTHS (65/16) — the handler only checks
	// decode lengths, so any base64 of that size is fine (the store never
	// validates key content either).
	p256dh := base64.RawURLEncoding.EncodeToString(make([]byte, 65))
	auth := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	return `{"endpoint":"https://push.example.com/e","keys":{"p256dh":"` + p256dh + `","auth":"` + auth + `"}}`
}

func servePush(mux http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// uaChromeAndroid is a real-shaped User-Agent the classifier reads as
// "Chrome · Android" (the clientlabel tests' fixture UA).
const uaChromeAndroid = "Mozilla/5.0 (Linux; Android 13; SM-A536B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

func TestPushSubscriptionCreateAndList(t *testing.T) {
	_, mux := setupPushHandlers(t)

	// Anonymous → 401.
	if rec := servePush(mux, pushAuthedReq("POST", "/api/v1/push-subscriptions", "", realSubscriptionBody())); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous create: status = %d, want 401", rec.Code)
	}

	// The create request's User-Agent IS the row's label source.
	createReq := pushAuthedReq("POST", "/api/v1/push-subscriptions", "viewer", realSubscriptionBody())
	createReq.Header.Set("User-Agent", uaChromeAndroid)
	rec := servePush(mux, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("create body: %v %q", err, rec.Body.String())
	}

	// The list shows the row (own rows only — seeded by the create above)
	// with the classifier's label.
	rec = servePush(mux, pushAuthedReq("GET", "/api/v1/push-subscriptions", "viewer", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200", rec.Code)
	}
	var list struct {
		Subscriptions []struct {
			ID       string  `json:"id"`
			Endpoint string  `json:"endpoint"`
			Label    *string `json:"label"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Subscriptions) != 1 || list.Subscriptions[0].ID != created.ID {
		t.Errorf("list: got %+v, want one row with id %s", list.Subscriptions, created.ID)
	}
	if got := list.Subscriptions[0].Label; got == nil || *got != "Chrome · Android" {
		t.Errorf("list label: got %v, want %q", got, "Chrome · Android")
	}

	// A request with no User-Agent stores SQL NULL — an explicit JSON null on
	// the list, never an empty string.
	noUA := pushAuthedReq("POST", "/api/v1/push-subscriptions", "viewer",
		strings.Replace(realSubscriptionBody(), "push.example.com/e", "push.example.com/e2", 1))
	rec = servePush(mux, noUA)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create without UA: status = %d, want 201", rec.Code)
	}
	rec = servePush(mux, pushAuthedReq("GET", "/api/v1/push-subscriptions", "viewer", ""))
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, row := range list.Subscriptions {
		if row.Endpoint != "https://push.example.com/e2" {
			continue
		}
		found = true
		if row.Label != nil {
			t.Errorf("no-UA row label: got %q, want null", *row.Label)
		}
	}
	if !found {
		t.Error("no-UA row missing from the list")
	}

	// Another user's list stays empty (rows are user-scoped).
	rec = servePush(mux, pushAuthedReq("GET", "/api/v1/push-subscriptions", "plain", ""))
	var plainList struct {
		Subscriptions []json.RawMessage `json:"subscriptions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &plainList); err != nil {
		t.Fatalf("decode plain list: %v", err)
	}
	if len(plainList.Subscriptions) != 0 {
		t.Errorf("plain user must see no rows, got %d", len(plainList.Subscriptions))
	}

	// No query parameters are accepted (the documented 400).
	if rec := servePush(mux, pushAuthedReq("GET", "/api/v1/push-subscriptions?limit=1", "viewer", "")); rec.Code != http.StatusBadRequest {
		t.Errorf("list with a query: status = %d, want 400", rec.Code)
	}
}

func TestPushSubscriptionCreateValidation(t *testing.T) {
	_, mux := setupPushHandlers(t)

	cases := []struct {
		name string
		body string
		code int
	}{
		{"unknown field", `{"endpoint":"https://push.example.com/e","keys":{"p256dh":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 65)) + `","auth":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 16)) + `"},"extra":1}`, http.StatusBadRequest},
		{"missing endpoint", `{"keys":{"p256dh":"AAAA","auth":"AAAA"}}`, http.StatusUnprocessableEntity},
		{"non-https endpoint", `{"endpoint":"http://push.example.com/e","keys":{"p256dh":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 65)) + `","auth":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 16)) + `"}}`, http.StatusUnprocessableEntity},
		{"wrong p256dh length", `{"endpoint":"https://push.example.com/e","keys":{"p256dh":"AAAA","auth":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 16)) + `"}}`, http.StatusUnprocessableEntity},
		{"wrong auth length", `{"endpoint":"https://push.example.com/e","keys":{"p256dh":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 65)) + `","auth":"AAAA"}}`, http.StatusUnprocessableEntity},
		{"non-base64 key", `{"endpoint":"https://push.example.com/e","keys":{"p256dh":"!!!","auth":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 16)) + `"}}`, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := servePush(mux, pushAuthedReq("POST", "/api/v1/push-subscriptions", "viewer", tc.body))
			if rec.Code != tc.code {
				t.Errorf("%s: status = %d, want %d (body %s)", tc.name, rec.Code, tc.code, rec.Body.String())
			}
		})
	}
}

func TestPushSubscriptionDelete(t *testing.T) {
	db, mux := setupPushHandlers(t)
	if _, err := store.UpsertPushSubscription(t.Context(), db, "s1", "viewer",
		"https://push.example.com/e", base64.RawURLEncoding.EncodeToString(make([]byte, 65)),
		base64.RawURLEncoding.EncodeToString(make([]byte, 16)), "", 1000); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Own delete → 204, row gone.
	if rec := servePush(mux, pushAuthedReq("DELETE", "/api/v1/push-subscriptions/s1", "viewer", "")); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", rec.Code)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("rows after delete: got %d, want 0", n)
	}

	// Repeat and foreign ids are masked 204s with no state change.
	for _, id := range []string{"s1", "s-other"} {
		if rec := servePush(mux, pushAuthedReq("DELETE", "/api/v1/push-subscriptions/"+id, "viewer", "")); rec.Code != http.StatusNoContent {
			t.Errorf("delete %s: status = %d, want 204", id, rec.Code)
		}
	}
	// A malformed id is a 400 (the public-id pattern).
	if rec := servePush(mux, pushAuthedReq("DELETE", "/api/v1/push-subscriptions/!!!", "viewer", "")); rec.Code != http.StatusBadRequest {
		t.Errorf("delete malformed id: status = %d, want 400", rec.Code)
	}
}

func TestPushSubscriptionRename(t *testing.T) {
	db, mux := setupPushHandlers(t)
	if _, err := store.UpsertPushSubscription(t.Context(), db, "s1", "viewer",
		"https://push.example.com/e", base64.RawURLEncoding.EncodeToString(make([]byte, 65)),
		base64.RawURLEncoding.EncodeToString(make([]byte, 16)), "", 1000); err != nil {
		t.Fatalf("seed own: %v", err)
	}
	if _, err := store.UpsertPushSubscription(t.Context(), db, "s2", "plain",
		"https://push.example.com/e2", base64.RawURLEncoding.EncodeToString(make([]byte, 65)),
		base64.RawURLEncoding.EncodeToString(make([]byte, 16)), "", 1000); err != nil {
		t.Fatalf("seed foreign: %v", err)
	}

	// Own rename → 204 and the trimmed label lands in the row.
	rec := servePush(mux, pushAuthedReq("PATCH", "/api/v1/push-subscriptions/s1", "viewer", `{"label":"  Το κινητό  "}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("rename: status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	var label string
	if err := db.QueryRow(`SELECT COALESCE(label, '') FROM push_subscriptions WHERE id = 's1'`).Scan(&label); err != nil {
		t.Fatalf("read label: %v", err)
	}
	if label != "Το κινητό" {
		t.Errorf("stored label: got %q, want the trimmed %q", label, "Το κινητό")
	}

	// Unknown and foreign ids answer the masked 404 — the SAME answer — and a
	// foreign row is never touched.
	for _, id := range []string{"ghost", "s2"} {
		if rec := servePush(mux, pushAuthedReq("PATCH", "/api/v1/push-subscriptions/"+id, "viewer", `{"label":"x"}`)); rec.Code != http.StatusNotFound {
			t.Errorf("rename %s: status = %d, want 404", id, rec.Code)
		}
	}
	var foreignLabel int
	if err := db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE id = 's2' AND label IS NOT NULL`).Scan(&foreignLabel); err != nil {
		t.Fatalf("count foreign label: %v", err)
	}
	if foreignLabel != 0 {
		t.Error("a foreign rename must not touch the row")
	}

	// A malformed id and an unknown body field are 400s; every label
	// violation is a 422 carrying exactly its {field, code}.
	cases := []struct {
		name string
		path string
		body string
		code int
		// field and rule are the expected single violation on a 422.
		field string
		rule  string
	}{
		{name: "malformed id", path: "/api/v1/push-subscriptions/!!!", body: `{"label":"x"}`, code: http.StatusBadRequest},
		{name: "unknown field", path: "/api/v1/push-subscriptions/s1", body: `{"label":"x","extra":1}`, code: http.StatusBadRequest},
		{name: "absent label", path: "/api/v1/push-subscriptions/s1", body: `{}`, code: http.StatusUnprocessableEntity, field: "label", rule: "required"},
		{name: "empty label", path: "/api/v1/push-subscriptions/s1", body: `{"label":"   "}`, code: http.StatusUnprocessableEntity, field: "label", rule: "required"},
		{name: "over-long label", path: "/api/v1/push-subscriptions/s1", body: `{"label":"` + strings.Repeat("α", 33) + `"}`, code: http.StatusUnprocessableEntity, field: "label", rule: "maxLength"},
		{name: "control character", path: "/api/v1/push-subscriptions/s1", body: `{"label":"a\u0000b"}`, code: http.StatusUnprocessableEntity, field: "label", rule: "invalidFormat"},
		{name: "zero-width space label", path: "/api/v1/push-subscriptions/s1", body: `{"label":"\u200b"}`, code: http.StatusUnprocessableEntity, field: "label", rule: "invalidFormat"},
		{name: "bidi override label", path: "/api/v1/push-subscriptions/s1", body: `{"label":"a\u202eb"}`, code: http.StatusUnprocessableEntity, field: "label", rule: "invalidFormat"},
		{name: "NBSP-only label", path: "/api/v1/push-subscriptions/s1", body: `{"label":"\u00a0"}`, code: http.StatusUnprocessableEntity, field: "label", rule: "required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := servePush(mux, pushAuthedReq("PATCH", tc.path, "viewer", tc.body))
			if rec.Code != tc.code {
				t.Errorf("%s: status = %d, want %d (body %s)", tc.name, rec.Code, tc.code, rec.Body.String())
				return
			}
			if tc.code != http.StatusUnprocessableEntity {
				return
			}
			var body struct {
				Violations []struct {
					Field string `json:"field"`
					Code  string `json:"code"`
				} `json:"violations"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s: decode violations: %v (body %s)", tc.name, err, rec.Body.String())
			}
			if len(body.Violations) != 1 || body.Violations[0].Field != tc.field || body.Violations[0].Code != tc.rule {
				t.Errorf("%s: violations = %+v, want one {%s %s}", tc.name, body.Violations, tc.field, tc.rule)
			}
		})
	}

	// A 32-rune Greek label is accepted — the bound counts RUNES, not bytes.
	exact := strings.Repeat("α", 32)
	rec = servePush(mux, pushAuthedReq("PATCH", "/api/v1/push-subscriptions/s1", "viewer", `{"label":"`+exact+`"}`))
	if rec.Code != http.StatusNoContent {
		t.Errorf("32-rune label: status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}

	// A non-breaking space is a space (Zs), not a format character: an
	// interior one rides along (a lone one trims away — the empty case above).
	rec = servePush(mux, pushAuthedReq("PATCH", "/api/v1/push-subscriptions/s1", "viewer", `{"label":"a\u00a0b"}`))
	if rec.Code != http.StatusNoContent {
		t.Errorf("NBSP label: status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestNotificationPreferencesRead(t *testing.T) {
	_, mux := setupPushHandlers(t)

	decode := func(rec *httptest.ResponseRecorder) []struct {
		Kind        string `json:"kind"`
		PushEnabled bool   `json:"pushEnabled"`
	} {
		var body struct {
			Preferences []struct {
				Kind        string `json:"kind"`
				PushEnabled bool   `json:"pushEnabled"`
			} `json:"preferences"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode preferences: %v", err)
		}
		return body.Preferences
	}

	// A regular user defaults OFF on every kind it is offered, and the
	// staff-only draft notice is not offered at all.
	rec := servePush(mux, pushAuthedReq("GET", "/api/v1/notification-preferences", "plain", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("plain read: status = %d", rec.Code)
	}
	for _, p := range decode(rec) {
		if p.PushEnabled {
			t.Errorf("plain user %s: want default off", p.Kind)
		}
		if p.Kind == "draft_activity" {
			t.Error("plain user must not be offered the staff-only kind")
		}
	}

	// A moderator defaults ON on every kind EXCEPT the draft notice, which is
	// opt-in at every role.
	rec = servePush(mux, pushAuthedReq("GET", "/api/v1/notification-preferences", "viewer", ""))
	sawDraft := false
	for _, p := range decode(rec) {
		if p.Kind == "draft_activity" {
			sawDraft = true
			if p.PushEnabled {
				t.Error("moderator draft_activity default: want off (opt-in)")
			}
			continue
		}
		if !p.PushEnabled {
			t.Errorf("moderator %s: want default on", p.Kind)
		}
	}
	if !sawDraft {
		t.Error("moderator read must offer the staff-only draft notice")
	}

	// No query parameters are accepted (the documented 400).
	if rec := servePush(mux, pushAuthedReq("GET", "/api/v1/notification-preferences?limit=1", "viewer", "")); rec.Code != http.StatusBadRequest {
		t.Errorf("preferences with a query: status = %d, want 400", rec.Code)
	}

	// The staff-only kind is a masked 404 below the floor — the same answer an
	// unknown kind gets — and a normal 204 for a moderator.
	if rec := servePush(mux, pushAuthedReq("PUT", "/api/v1/notification-preferences/draft_activity", "plain", `{"pushEnabled":true}`)); rec.Code != http.StatusNotFound {
		t.Errorf("plain draft_activity write: status = %d, want 404", rec.Code)
	}
	if rec := servePush(mux, pushAuthedReq("PUT", "/api/v1/notification-preferences/draft_activity", "viewer", `{"pushEnabled":true}`)); rec.Code != http.StatusNoContent {
		t.Fatalf("moderator draft_activity write: status = %d, want 204", rec.Code)
	}
	rec = servePush(mux, pushAuthedReq("GET", "/api/v1/notification-preferences", "viewer", ""))
	for _, p := range decode(rec) {
		if p.Kind == "draft_activity" && !p.PushEnabled {
			t.Error("draft_activity should be ON after the explicit opt-in")
		}
	}

	// An explicit override flips the effective value.
	if rec := servePush(mux, pushAuthedReq("PUT", "/api/v1/notification-preferences/heart", "viewer", `{"pushEnabled":false}`)); rec.Code != http.StatusNoContent {
		t.Fatalf("override: status = %d, want 204", rec.Code)
	}
	rec = servePush(mux, pushAuthedReq("GET", "/api/v1/notification-preferences", "viewer", ""))
	for _, p := range decode(rec) {
		if p.Kind == "heart" && p.PushEnabled {
			t.Error("heart should be off after the explicit override")
		}
		if p.Kind == "comment_reply" && !p.PushEnabled {
			t.Error("comment_reply should keep the moderator default after the heart override")
		}
	}

	// Unknown kind → masked 404; unknown field → 400.
	if rec := servePush(mux, pushAuthedReq("PUT", "/api/v1/notification-preferences/bogus", "viewer", `{"pushEnabled":true}`)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown kind: status = %d, want 404", rec.Code)
	}
	if rec := servePush(mux, pushAuthedReq("PUT", "/api/v1/notification-preferences/heart", "viewer", `{"pushEnabled":true,"extra":1}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: status = %d, want 400", rec.Code)
	}

	// Anonymous → 401 on both reads and writes.
	if rec := servePush(mux, pushAuthedReq("GET", "/api/v1/notification-preferences", "", "")); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous read: status = %d, want 401", rec.Code)
	}
	if rec := servePush(mux, pushAuthedReq("PUT", "/api/v1/notification-preferences/heart", "", `{"pushEnabled":true}`)); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous write: status = %d, want 401", rec.Code)
	}

	// A missing pushEnabled is a 422 {pushEnabled, required} (the strict
	// write-body convention) — {} must never silently write false.
	if rec := servePush(mux, pushAuthedReq("PUT", "/api/v1/notification-preferences/heart", "viewer", `{}`)); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty body: status = %d, want 422", rec.Code)
	}
}

// TestNotificationPreferencesKinds pins the known-kind set: the read returns
// every known kind, and the newest kinds accept explicit toggles.
func TestNotificationPreferencesKinds(t *testing.T) {
	t.Parallel()

	_, mux := setupPushHandlers(t)

	rec := servePush(mux, pushAuthedReq("GET", "/api/v1/notification-preferences", "viewer", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("read: status = %d", rec.Code)
	}
	var body struct {
		Preferences []struct {
			Kind string `json:"kind"`
		} `json:"preferences"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var kinds []string
	for _, p := range body.Preferences {
		kinds = append(kinds, p.Kind)
	}
	want := []string{"heart", "comment_reply", "comment", "content_updated", "new_content", "comment_removed", "draft_activity"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds: got %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("kind %d: got %q, want %q", i, kinds[i], want[i])
		}
	}

	// The newest kinds accept explicit toggles (the 0017/0019 CHECKs admit
	// them); draft_activity is offered to this moderator (the staff floor).
	for _, kind := range []string{"comment", "content_updated", "new_content", "comment_removed", "draft_activity"} {
		if rec := servePush(mux, pushAuthedReq("PUT", "/api/v1/notification-preferences/"+kind, "viewer", `{"pushEnabled":false}`)); rec.Code != http.StatusNoContent {
			t.Errorf("PUT %s: status = %d, want 204", kind, rec.Code)
		}
	}
}

// fakeTester is the self-test seam double: it records the user ids it was
// asked to notify and answers a fixed report or error.
type fakeTester struct {
	delivered int
	outcomes  []push.DeliveryOutcome
	err       error
	calls     []string
}

func (f *fakeTester) SendTest(_ context.Context, userID string) (push.TestReport, error) {
	f.calls = append(f.calls, userID)
	if f.err != nil {
		return push.TestReport{}, f.err
	}
	outcomes := f.outcomes
	if outcomes == nil {
		outcomes = []push.DeliveryOutcome{}
	}
	return push.TestReport{Delivered: f.delivered, Endpoints: outcomes}, nil
}

// TestPushTestSend pins the self-test handler, including the
// bridged-body-size revision: the 200
// {delivered, endpoints} shape, the seam receiving the SESSION user, the error
// path, and the nil seam (delivery disabled) answering zero.
func TestPushTestSend(t *testing.T) {
	t.Parallel()

	// One limiter per sub-test: a shared instance would turn a later
	// sub-test's 200 into a 429 as soon as the presses pile up.
	newMux := func(tester push.TestSender) *http.ServeMux {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /api/v1/push-subscriptions/test",
			handler.PushTestSend(tester, middleware.NewRateLimiter(5, 10*time.Minute, time.Minute)))
		return mux
	}

	readDelivered := func(t *testing.T, rec *httptest.ResponseRecorder) int {
		t.Helper()
		var got struct {
			Delivered int `json:"delivered"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response %q: %v", rec.Body.String(), err)
		}
		return got.Delivered
	}

	t.Run("delivers to the session user's devices", func(t *testing.T) {
		tester := &fakeTester{delivered: 2}
		mux := newMux(tester)

		rec := servePush(mux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", "plain", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		if got := readDelivered(t, rec); got != 2 {
			t.Errorf("delivered = %d, want 2", got)
		}
		if len(tester.calls) != 1 || tester.calls[0] != "plain" {
			t.Errorf("seam calls = %v, want one call for the session user", tester.calls)
		}
		// Nothing attempted → an EMPTY list, never a null: the client iterates it.
		var wire struct {
			Endpoints []any `json:"endpoints"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
		if wire.Endpoints == nil || len(wire.Endpoints) != 0 {
			t.Errorf("endpoints = %v, want an empty array", wire.Endpoints)
		}
	})

	// The per-endpoint report: the caller must
	// see WHICH of their rows was refused. It carries the row id, the
	// push-service family and the transport verdict only — the pin asserts the
	// exact key set, so a later edit cannot start leaking the endpoint URL or
	// the key material.
	t.Run("reports one outcome per endpoint", func(t *testing.T) {
		tester := &fakeTester{
			delivered: 1,
			outcomes: []push.DeliveryOutcome{
				{SubscriptionID: "sub-a", Service: "mozilla", Accepted: false, Status: 413},
				{SubscriptionID: "sub-b", Service: "fcm", Accepted: true, Status: 201},
				{SubscriptionID: "sub-c", Service: "apple", Accepted: false, Removed: true, Status: 410},
				// No response ever arrived: the status key must be ABSENT, not 0.
				{SubscriptionID: "sub-d", Service: "other"},
			},
		}
		mux := newMux(tester)

		rec := servePush(mux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", "plain", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		var got struct {
			Delivered int              `json:"delivered"`
			Endpoints []map[string]any `json:"endpoints"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
		if got.Delivered != 1 || len(got.Endpoints) != 4 {
			t.Fatalf("delivered = %d, endpoints = %d; want 1 and 4", got.Delivered, len(got.Endpoints))
		}
		for i, want := range []map[string]any{
			{"id": "sub-a", "service": "mozilla", "accepted": false, "status": float64(413)},
			{"id": "sub-b", "service": "fcm", "accepted": true, "status": float64(201)},
			{"id": "sub-c", "service": "apple", "accepted": false, "status": float64(410), "removed": true},
			{"id": "sub-d", "service": "other", "accepted": false},
		} {
			if !maps.Equal(got.Endpoints[i], want) {
				t.Errorf("endpoint %d = %v, want %v", i, got.Endpoints[i], want)
			}
		}
	})

	t.Run("a store failure is a 500 problem", func(t *testing.T) {
		tester := &fakeTester{err: errors.New("boom")}
		mux := newMux(tester)

		rec := servePush(mux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", "plain", ""))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
			t.Errorf("content type = %q, want a problem+json document", ct)
		}
	})

	t.Run("no seam reports zero (delivery disabled)", func(t *testing.T) {
		mux := newMux(nil)

		rec := servePush(mux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", "plain", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := readDelivered(t, rec); got != 0 {
			t.Errorf("delivered = %d, want 0", got)
		}
	})

	t.Run("anonymous is unauthorized", func(t *testing.T) {
		mux := newMux(&fakeTester{})

		rec := servePush(mux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", "", ""))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("the user-keyed bucket answers 429 and does not send", func(t *testing.T) {
		tester := &fakeTester{delivered: 1}
		tightMux := http.NewServeMux()
		tight := middleware.NewRateLimiter(1, time.Minute, time.Minute)
		tightMux.HandleFunc("POST /api/v1/push-subscriptions/test", handler.PushTestSend(tester, tight))

		first := servePush(tightMux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", "plain", ""))
		if first.Code != http.StatusOK {
			t.Fatalf("first press: status = %d, want 200", first.Code)
		}
		second := servePush(tightMux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", "plain", ""))
		if second.Code != http.StatusTooManyRequests {
			t.Fatalf("second press: status = %d, want 429", second.Code)
		}
		if second.Header().Get("Retry-After") == "" {
			t.Error("429 without a Retry-After header")
		}
		if ct := second.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
			t.Errorf("content type = %q, want a problem+json 429", ct)
		}
		if len(tester.calls) != 1 {
			t.Errorf("seam calls = %d, want 1 — the limiter must run before the send", len(tester.calls))
		}
	})

	t.Run("query parameters are rejected", func(t *testing.T) {
		mux := newMux(&fakeTester{delivered: 1})

		rec := servePush(mux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test?force=1", "plain", ""))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", rec.Code, rec.Body.String())
		}

		// A malformed press must not spend the bucket: five rejections in a
		// row, then a clean press — still 200, never 429 (the bucket runs
		// after the cheap validation, the comment-write convention).
		for range 4 {
			servePush(mux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test?force=1", "plain", ""))
		}
		ok := servePush(mux, pushAuthedReq(http.MethodPost, "/api/v1/push-subscriptions/test", "plain", ""))
		if ok.Code != http.StatusOK {
			t.Fatalf("clean press after five rejected ones: status = %d, want 200", ok.Code)
		}
	})
}
