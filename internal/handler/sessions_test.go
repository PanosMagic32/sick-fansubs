package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// Session-list handler tests. The handlers read the session
// from the request context, so tests attach a synthetic SessionUser directly
// — the session/cookie/CSRF middleware is covered by the routes and
// middleware suites.

// sessionListNowMS is the injected clock: sessions seeded "after" it are
// expired for the handler's purposes.
const sessionListNowMS = 100_000

// sessionItemWire mirrors the handler's sessionItemDTO.
type sessionItemWire struct {
	ID          string  `json:"id"`
	ClientLabel *string `json:"clientLabel"`
	CreatedAt   string  `json:"createdAt"`
	ExpiresAt   string  `json:"expiresAt"`
	Current     bool    `json:"current"`
}

type sessionListWire struct {
	Items    []sessionItemWire `json:"items"`
	PageInfo struct {
		HasNextPage bool    `json:"hasNextPage"`
		EndCursor   *string `json:"endCursor"`
	} `json:"pageInfo"`
}

// setupSessionHandlers creates a migrated database seeded with two users and
// four sessions — one expired, one owned by the other user, one labeled, one
// unlabeled — plus a mux with the two session handlers registered at their
// production paths and a fixed clock.
func setupSessionHandlers(t *testing.T, logger *slog.Logger) (*sql.DB, http.Handler) {
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

	for _, u := range []struct{ id, username string }{
		{"u1", "Katakuri"},
		{"u2", "Other"},
	} {
		storetest.InsertUser(t, db, storetest.UserSpec{
			ID:       u.id,
			Username: u.username,
		})
	}

	const active = sessionListNowMS + 1_000
	seed := func(id, userID string, seedByte byte, label string, createdMS, expiresMS int64) {
		t.Helper()
		if err := store.CreateSession(t.Context(), db, store.CreateSessionParams{
			ID:          id,
			UserID:      userID,
			TokenDigest: bytes.Repeat([]byte{seedByte}, 32),
			CSRF:        bytes.Repeat([]byte{seedByte + 100}, 32),
			AuthVersion: 1,
			ClientLabel: label,
			CreatedAtMS: createdMS,
			ExpiresAtMS: expiresMS,
		}); err != nil {
			t.Fatalf("seed session %q: %v", id, err)
		}
	}
	// s1: the requesting session (unlabeled). s2: newer, labeled.
	// s3: expired. s4: another user's live session.
	seed("s1", "u1", 1, "", 1_000, active)
	seed("s2", "u1", 2, "Chrome · Android", 2_000, active)
	seed("s3", "u1", 3, "Firefox · Windows", 250, 500)
	seed("s4", "u2", 4, "Safari · iOS", 4_000, active)

	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	}
	now := func() time.Time { return time.UnixMilli(sessionListNowMS) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/users/me/sessions", handler.SessionsList(db, now))
	mux.HandleFunc("DELETE /api/v1/users/me/sessions/{id}", handler.SessionRevoke(db, false))

	return db, logContext(logger, middleware.RequestID()(mux))
}

// sessionRequest builds a request with the s1 session attached (or without
// one when withSession is false).
func sessionRequest(method, path string, withSession bool) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if withSession {
		req = req.WithContext(middleware.SetSession(req.Context(), &identity.SessionUser{
			SessionID: "s1",
			UserID:    "u1",
			Username:  "Katakuri",
			Role:      "user",
		}))
	}
	return req
}

// TestSessionsList_Unauthenticated pins the 401 for both endpoints.
func TestSessionsList_Unauthenticated(t *testing.T) {
	t.Parallel()

	_, mux := setupSessionHandlers(t, nil)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/me/sessions"},
		{http.MethodDelete, "/api/v1/users/me/sessions/s2"},
	} {
		rec := serve(mux, sessionRequest(tc.method, tc.path, false))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// TestSessionsList_ShapeWindowAndCurrent pins the wire
// contract: only the caller's live sessions, newest first, canonical
// instants, the server-derived current flag, an explicit null label, and the
// baseline headers.
func TestSessionsList_ShapeWindowAndCurrent(t *testing.T) {
	t.Parallel()

	_, mux := setupSessionHandlers(t, nil)

	rec := serve(mux, sessionRequest(http.MethodGet, "/api/v1/users/me/sessions", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("missing X-Request-ID header")
	}
	// A null label is an explicit null, not an omitted key.
	if !strings.Contains(rec.Body.String(), `"clientLabel":null`) {
		t.Errorf("body does not carry an explicit null clientLabel: %s", rec.Body.String())
	}

	var body sessionListWire
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %d, want 2 (the expired and the foreign session stay out): %+v", len(body.Items), body.Items)
	}

	newest, current := body.Items[0], body.Items[1]
	if newest.ID != "s2" || current.ID != "s1" {
		t.Fatalf("order = [%s %s], want [s2 s1] (created_at_ms DESC)", newest.ID, current.ID)
	}
	if newest.ClientLabel == nil || *newest.ClientLabel != "Chrome · Android" {
		t.Errorf("s2 clientLabel = %v, want %q", newest.ClientLabel, "Chrome · Android")
	}
	if current.ClientLabel != nil {
		t.Errorf("s1 clientLabel = %v, want null", *current.ClientLabel)
	}
	if !current.Current || newest.Current {
		t.Errorf("current flags = [%s:%t %s:%t], want only s1 true", newest.ID, newest.Current, current.ID, current.Current)
	}
	if newest.CreatedAt != "1970-01-01T00:00:02.000Z" {
		t.Errorf("s2 createdAt = %q, want the canonical millisecond instant", newest.CreatedAt)
	}
	if newest.ExpiresAt != "1970-01-01T00:01:41.000Z" {
		t.Errorf("s2 expiresAt = %q, want the canonical millisecond instant", newest.ExpiresAt)
	}
	if body.PageInfo.HasNextPage {
		t.Error("hasNextPage = true, want false")
	}
	if body.PageInfo.EndCursor != nil {
		t.Errorf("endCursor = %q, want null on the last page", *body.PageInfo.EndCursor)
	}
}

// TestSessionsList_QueryContract pins the strict query surface and the
// endpoint-bound cursor: limit bounds, unknown/duplicate parameters, and a
// cursor minted for another endpoint.
func TestSessionsList_QueryContract(t *testing.T) {
	t.Parallel()

	_, mux := setupSessionHandlers(t, nil)

	first := serve(mux, sessionRequest(http.MethodGet, "/api/v1/users/me/sessions?limit=1", true))
	if first.Code != http.StatusOK {
		t.Fatalf("limit=1: status = %d, want 200 (body %s)", first.Code, first.Body.String())
	}
	var page sessionListWire
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode paged body: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "s2" || !page.PageInfo.HasNextPage || page.PageInfo.EndCursor == nil {
		t.Fatalf("first page = %+v, want one item (s2) with a cursor", page)
	}
	if !strings.HasPrefix(*page.PageInfo.EndCursor, "ss1.") {
		t.Errorf("endCursor = %q, want the ss1. namespace", *page.PageInfo.EndCursor)
	}

	// The minted cursor continues the walk.
	next := serve(mux, sessionRequest(http.MethodGet, "/api/v1/users/me/sessions?limit=1&after="+*page.PageInfo.EndCursor, true))
	if next.Code != http.StatusOK {
		t.Fatalf("after continuation: status = %d, want 200 (body %s)", next.Code, next.Body.String())
	}
	var page2 sessionListWire
	if err := json.Unmarshal(next.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode continuation body: %v", err)
	}
	if len(page2.Items) != 1 || page2.Items[0].ID != "s1" {
		t.Fatalf("second page = %+v, want the s1 item", page2.Items)
	}

	// A cursor minted for another endpoint (same payload, foreign namespace)
	// is rejected — the namespace IS the endpoint binding.
	foreign := "nv1." + strings.TrimPrefix(*page.PageInfo.EndCursor, "ss1.")
	cases := []struct {
		name, path string
		want       int
	}{
		{"foreign-namespace cursor", "/api/v1/users/me/sessions?after=" + foreign, http.StatusBadRequest},
		{"garbage cursor", "/api/v1/users/me/sessions?after=not-a-cursor", http.StatusBadRequest},
		{"unknown parameter", "/api/v1/users/me/sessions?page=2", http.StatusBadRequest},
		{"duplicate limit", "/api/v1/users/me/sessions?limit=1&limit=2", http.StatusBadRequest},
		{"non-integer limit", "/api/v1/users/me/sessions?limit=abc", http.StatusBadRequest},
		{"out-of-range limit", "/api/v1/users/me/sessions?limit=0", http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := serve(mux, sessionRequest(http.MethodGet, tc.path, true))
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestSessionRevoke_OwnForeignRepeatAndCurrent pins the contract: the
// owner-scoped idempotent 204, no cross-user effect, the invalid-id 400, and
// the cookie clear when the caller revokes its own current session.
func TestSessionRevoke_OwnForeignRepeatAndCurrent(t *testing.T) {
	t.Parallel()

	db, mux := setupSessionHandlers(t, nil)

	// Own, non-current session: deleted, no cookie is touched.
	rec := serve(mux, sessionRequest(http.MethodDelete, "/api/v1/users/me/sessions/s2", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("own revoke: status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("own revoke set cookies: %+v", cookies)
	}
	if n := sessionRowsFor(t, db, "u1"); n != 2 {
		t.Errorf("u1 rows after revoke = %d, want 2 (s1 live, s3 expired)", n)
	}

	// Repeat: the same 204, nothing else changes.
	if rec := serve(mux, sessionRequest(http.MethodDelete, "/api/v1/users/me/sessions/s2", true)); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat revoke: status = %d, want 204", rec.Code)
	}

	// Another user's session id: no cross-user effect, same 204.
	if rec := serve(mux, sessionRequest(http.MethodDelete, "/api/v1/users/me/sessions/s4", true)); rec.Code != http.StatusNoContent {
		t.Fatalf("foreign revoke: status = %d, want 204", rec.Code)
	}
	if n := sessionRowsFor(t, db, "u2"); n != 1 {
		t.Errorf("u2 rows after a foreign revoke = %d, want 1 (untouched)", n)
	}

	// Invalid id shape: a malformed request, not a masked absence.
	if rec := serve(mux, sessionRequest(http.MethodDelete, "/api/v1/users/me/sessions/bad%20id", true)); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid id: status = %d, want 400", rec.Code)
	}

	// The current session: revoked like any other, and the cookie is cleared
	// after the delete commits.
	rec = serve(mux, sessionRequest(http.MethodDelete, "/api/v1/users/me/sessions/s1", true))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("current revoke: status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	var cleared *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CookieName {
			cleared = c
		}
	}
	if cleared == nil {
		t.Fatal("current revoke did not clear the session cookie")
	}
	if cleared.Value != "" || cleared.MaxAge > 0 || !cleared.HttpOnly {
		t.Errorf("cleared cookie = %+v, want an empty deletion cookie", cleared)
	}
	if n := sessionRowsFor(t, db, "u1"); n != 1 {
		t.Errorf("u1 rows after revoking the current session = %d, want 1 (the expired s3)", n)
	}
}

// TestSessionRevoke_Audit pins the audit rows: a success row
// only when a row was actually deleted, and a failure row when an attempted
// delete fails.
func TestSessionRevoke_Audit(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	db, mux := setupSessionHandlers(t, logger)

	if rec := serve(mux, sessionRequest(http.MethodDelete, "/api/v1/users/me/sessions/s2", true)); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d, want 204", rec.Code)
	}
	// A no-op second call (and the foreign no-op) must not add rows.
	serve(mux, sessionRequest(http.MethodDelete, "/api/v1/users/me/sessions/s2", true))
	serve(mux, sessionRequest(http.MethodDelete, "/api/v1/users/me/sessions/s4", true))

	records := parseAuditEvents(t, &buf)
	if len(records) != 1 {
		t.Fatalf("audit records = %d, want exactly 1 (the committed revoke): %+v", len(records), records)
	}
	if records[0].Event != audit.EventSessionRevoked || records[0].Result != audit.ResultSuccess {
		t.Errorf("audit record = %+v, want session_revoked/success", records[0])
	}
	if records[0].ActorID != "u1" {
		t.Errorf("actorID = %q, want u1", records[0].ActorID)
	}
	if records[0].TargetID != "" {
		t.Errorf("targetID = %q, want empty (self-revocation has no target account)", records[0].TargetID)
	}

	// A failed delete is audited as a failure and answers 500.
	buf.Reset()
	db.Close()
	rec := serve(mux, sessionRequest(http.MethodDelete, "/api/v1/users/me/sessions/s1", true))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed revoke: status = %d, want 500", rec.Code)
	}
	records = parseAuditEvents(t, &buf)
	if len(records) != 1 || records[0].Event != audit.EventSessionRevoked || records[0].Result != audit.ResultFailure {
		t.Fatalf("failure audit records = %+v, want one session_revoked/failure", records)
	}
}

// sessionRowsFor counts a user's session rows directly — the list window
// would hide the expired one, and these assertions are about storage.
func sessionRowsFor(t *testing.T, db *sql.DB, userID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, userID).Scan(&n); err != nil {
		t.Fatalf("count sessions for %q: %v", userID, err)
	}
	return n
}

// TestSessions_ClientLabelReachesStorage pins acceptance criterion 8 at the
// HTTP boundary: the auth handlers pass the REAL request's
// User-Agent to the service, and only the classifier's label lands in the
// column. Dropping r.UserAgent() at either call site fails here — the
// service-level classifier test cannot see that boundary.
func TestSessions_ClientLabelReachesStorage(t *testing.T) {
	t.Parallel()

	_, db, mux := setupAuth(t)
	srv := withMiddleware(mux, db)

	const chromeUA = "Mozilla/5.0 (Linux; Android 13; SM-A536B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
	const firefoxUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0"

	// Registration creates the first session.
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		jsonBody(t, map[string]string{
			"username": "Labeled", "email": "labeled@example.com", "password": "password123",
		}))
	regReq.Header.Set("Content-Type", "application/json")
	regReq.Header.Set("User-Agent", chromeUA)
	setOrigin(regReq)
	regRec := httptest.NewRecorder()
	srv.ServeHTTP(regRec, regReq)
	if regRec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201 (body %s)", regRec.Code, regRec.Body.String())
	}

	// A SECOND sign-in (no cookie on this request) creates another session.
	signReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sign-in",
		jsonBody(t, map[string]string{"identifier": "Labeled", "password": "password123"}))
	signReq.Header.Set("Content-Type", "application/json")
	signReq.Header.Set("User-Agent", firefoxUA)
	setOrigin(signReq)
	signRec := httptest.NewRecorder()
	srv.ServeHTTP(signRec, signReq)
	if signRec.Code != http.StatusOK {
		t.Fatalf("sign-in: status = %d, want 200 (body %s)", signRec.Code, signRec.Body.String())
	}

	rows, err := db.Query(`SELECT client_label FROM sessions WHERE client_label IS NOT NULL`)
	if err != nil {
		t.Fatalf("read labels: %v", err)
	}
	defer rows.Close()
	var labels []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			t.Fatalf("scan label: %v", err)
		}
		labels = append(labels, label)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate labels: %v", err)
	}
	slices.Sort(labels)
	want := []string{"Chrome · Android", "Firefox · Windows"}
	if !slices.Equal(labels, want) {
		t.Fatalf("stored labels = %v, want %v (the classifier output, never the raw header)", labels, want)
	}

	// …and the raw headers never reached storage.
	var leaked int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE client_label LIKE '%Mozilla%'`).Scan(&leaked); err != nil {
		t.Fatalf("scan for leaked user-agent: %v", err)
	}
	if leaked != 0 {
		t.Errorf("%d rows carry raw user-agent text", leaked)
	}
}
