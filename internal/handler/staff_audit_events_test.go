package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
)

// Staff audit browser handler tests: the
// super-admin floor, the kind filter's allowlist, the limit bounds, the
// forensic wire shape, and the degraded cases.

// auditBody mirrors the wire contract in docs/api/openapi.yaml.
type auditBody struct {
	Items []struct {
		ID         string `json:"id"`
		Event      string `json:"event"`
		Result     string `json:"result"`
		ActorID    string `json:"actorId"`
		TargetID   string `json:"targetId"`
		TargetRole string `json:"targetRole"`
		RequestID  string `json:"requestId"`
		RemoteAddr string `json:"remoteAddr"`
		CreatedAt  string `json:"createdAt"`
	} `json:"items"`
}

// setupStaffAuditHandler creates a fresh migrated database and the audit
// browser behind the session chain at GET /api/v1/staff/audit-events.
func setupStaffAuditHandler(t *testing.T) (*sql.DB, http.Handler) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/staff/audit-events",
		handler.StaffAuditEvents(db))

	var h http.Handler = mux
	h = middleware.Session(db, false)(h)
	h = middleware.TrustedOrigin("http://localhost:5173")(h)
	h = middleware.RequestID()(h)

	return db, h
}

// seedAuditHandlerRow inserts one ledger row with an explicit creation time.
func seedAuditHandlerRow(t *testing.T, db *sql.DB, id, event, result, actorID, targetID string, createdAtMS int64) {
	t.Helper()
	var actor, target any
	if actorID != "" {
		actor = actorID
	}
	if targetID != "" {
		target = targetID
	}
	if _, err := db.Exec(`INSERT INTO audit_events
		(id, event, result, actor_id, target_id, request_id, remote_addr, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, event, result, actor, target, "req-"+id, "192.0.2.1", createdAtMS); err != nil {
		t.Fatalf("seed audit row %s: %v", id, err)
	}
}

func getStaffAudit(h http.Handler, cookie, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/audit-events"+query, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestStaffAuditEvents_SuperAdmin pins the wire shape: newest first, the
// canonical instant, the forensic fields present, and the NULL identities
// OMITTED (not null, not empty) — the client renders only what arrived.
func TestStaffAuditEvents_SuperAdmin(t *testing.T) {
	t.Parallel()

	db, h := setupStaffAuditHandler(t)
	seedAuditHandlerRow(t, db, "e1", audit.EventSignInFailure, audit.ResultFailure, "", "", 1_700_000_000_000)
	seedAuditHandlerRow(t, db, "e2", audit.EventRoleChanged, audit.ResultSuccess, "actor1", "target1", 1_700_000_001_000)
	cookie := mustSuperAdminSession(t, db, "s1")

	rec := getStaffAudit(h, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing — RequestID middleware not applied")
	}

	var body auditBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (body %q)", err, rec.Body.String())
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %d (%v), want 2", len(body.Items), body.Items)
	}

	newest := body.Items[0]
	if newest.ID != "e2" || newest.Event != audit.EventRoleChanged || newest.Result != audit.ResultSuccess {
		t.Errorf("newest = %+v, want the newest row first", newest)
	}
	if newest.ActorID != "actor1" || newest.TargetID != "target1" {
		t.Errorf("identities = %q/%q, want the stored values", newest.ActorID, newest.TargetID)
	}
	if newest.RequestID != "req-e2" || newest.RemoteAddr != "192.0.2.1" {
		t.Errorf("forensics = %q/%q, want the stored values", newest.RequestID, newest.RemoteAddr)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z07:00", newest.CreatedAt); err != nil {
		t.Errorf("createdAt = %q, want the canonical instant (%v)", newest.CreatedAt, err)
	}

	// The actor-only row must not carry the identity keys at all (omitempty on
	// the NULL-mapped columns), while the target row does.
	var rawItems struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rawItems); err != nil {
		t.Fatalf("decode raw items: %v", err)
	}
	actorOnly := rawItems.Items[1]
	for _, key := range []string{"actorId", "targetId", "targetRole"} {
		if value, present := actorOnly[key]; present {
			t.Errorf("actor-only row carries %s = %v, want the NULL identity omitted", key, value)
		}
	}
	if rawItems.Items[0]["targetId"] != "target1" || rawItems.Items[0]["targetRole"] != nil {
		t.Errorf("target row = %v, want targetId present and the NULL targetRole omitted", rawItems.Items[0])
	}
}

// TestStaffAuditEvents_EmptyRemoteAddrIsPresent pins the non-omitempty wire
// field: a stored empty address (the break-glass command) must arrive as
// "remoteAddr":"" — dropping the key would violate the schema's required set.
func TestStaffAuditEvents_EmptyRemoteAddrIsPresent(t *testing.T) {
	t.Parallel()

	db, h := setupStaffAuditHandler(t)
	if _, err := db.Exec(`INSERT INTO audit_events
		(id, event, result, request_id, remote_addr, created_at_ms)
		VALUES ('e1', ?, 'success', '', '', 1_700_000_000_000)`, audit.EventSessionRevoked); err != nil {
		t.Fatalf("seed empty-address row: %v", err)
	}
	cookie := mustSuperAdminSession(t, db, "s1")

	rec := getStaffAudit(h, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var rawItems struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rawItems); err != nil {
		t.Fatalf("decode raw items: %v", err)
	}
	if len(rawItems.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(rawItems.Items))
	}
	if value, present := rawItems.Items[0]["remoteAddr"]; !present || value != "" {
		t.Errorf("remoteAddr = %v (present=%t), want the empty string present", value, present)
	}
}

// TestStaffAuditEvents_FilterAndLimit pins the contract's value semantics:
// the kind filter must change the row set, and the cap bounds it.
func TestStaffAuditEvents_FilterAndLimit(t *testing.T) {
	t.Parallel()

	db, h := setupStaffAuditHandler(t)
	seedAuditHandlerRow(t, db, "e1", audit.EventSignInFailure, audit.ResultFailure, "", "", 1_000)
	seedAuditHandlerRow(t, db, "e2", audit.EventRoleChanged, audit.ResultSuccess, "a", "t", 2_000)
	seedAuditHandlerRow(t, db, "e3", audit.EventSignInFailure, audit.ResultFailure, "", "", 3_000)
	cookie := mustSuperAdminSession(t, db, "s1")

	var body auditBody
	rec := getStaffAudit(h, cookie, "?event=sign_in_failure")
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 2 || body.Items[0].ID != "e3" || body.Items[1].ID != "e1" {
		t.Errorf("filtered = %+v, want only the two sign_in_failure rows", body.Items)
	}

	rec = getStaffAudit(h, cookie, "?limit=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("bounded status = %d, want 200", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "e3" {
		t.Errorf("limit=1 = %+v, want only the newest row", body.Items)
	}
}

// TestStaffAuditEvents_Gates pins the floor: anonymous → 401, and every
// below-floor role → 403 (an admin is NOT enough). That the floor runs BEFORE
// the store is proven separately, by TestStaffAuditEvents_FloorPrecedesTheRead.
func TestStaffAuditEvents_Gates(t *testing.T) {
	t.Parallel()

	db, h := setupStaffAuditHandler(t)
	mustCreateStaffUser(t, db, "u1", "User One", "user", "active", 3_000)
	mustCreateStaffUser(t, db, "mod1", "Mod", "moderator", "active", 3_000)
	mustCreateStaffUser(t, db, "admin1", "Admin", "admin", "active", 3_000)
	mustCreateStaffUser(t, db, "s1", "Super", "super-admin", "active", 3_000)

	for _, tc := range []struct {
		name   string
		cookie string
		want   int
	}{
		{"anonymous", "", http.StatusUnauthorized},
		{"role=user", mustCreateStaffSession(t, db, "u1"), http.StatusForbidden},
		{"moderator", mustCreateStaffSession(t, db, "mod1"), http.StatusForbidden},
		{"admin", mustCreateStaffSession(t, db, "admin1"), http.StatusForbidden},
		{"super-admin", mustCreateStaffSession(t, db, "s1"), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := getStaffAudit(h, tc.cookie, "")
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want != http.StatusOK {
				if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
					t.Errorf("Content-Type = %q, want a problem", ct)
				}
			}
		})
	}
}

// TestStaffAuditEvents_FloorPrecedesTheRead breaks the table the read needs
// and shows the below-floor answer is unchanged — the store never runs.
func TestStaffAuditEvents_FloorPrecedesTheRead(t *testing.T) {
	t.Parallel()

	db, h := setupStaffAuditHandler(t)
	mustCreateStaffUser(t, db, "admin1", "Admin", "admin", "active", 3_000)
	mustCreateStaffUser(t, db, "s1", "Super", "super-admin", "active", 3_000)
	adminCookie := mustCreateStaffSession(t, db, "admin1")
	superCookie := mustCreateStaffSession(t, db, "s1")

	if _, err := db.Exec(`DROP TABLE audit_events`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	if rec := getStaffAudit(h, adminCookie, ""); rec.Code != http.StatusForbidden {
		t.Errorf("admin after the table drop: status = %d, want 403 (the store must not run)", rec.Code)
	}
	if rec := getStaffAudit(h, superCookie, ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("super-admin after the table drop: status = %d, want 500 (the store does run)", rec.Code)
	}

	// The floor also precedes the QUERY parse: a below-floor request carrying a
	// parameter the endpoint does not allow is still a 403, not a 400/422.
	if rec := getStaffAudit(h, adminCookie, "?event=nope"); rec.Code != http.StatusForbidden {
		t.Errorf("admin with a bad event: status = %d, want 403 (the floor answers first)", rec.Code)
	}
}

// TestStaffAuditEvents_QueryParamContract pins 400 vs 422 and the event
// allowlist: free text is rejected, not passed to SQL.
func TestStaffAuditEvents_QueryParamContract(t *testing.T) {
	t.Parallel()

	db, h := setupStaffAuditHandler(t)
	cookie := mustSuperAdminSession(t, db, "s1")

	for _, query := range []string{"?unknown=1", "?limit=1&limit=2", "?limit=x", "?limit=", "?%zz=1"} {
		rec := getStaffAudit(h, cookie, query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %q: status = %d, want 400", query, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
			t.Errorf("GET %q: Content-Type = %q, want a problem", query, ct)
		}
	}

	for _, tc := range []struct {
		name  string
		query string
		field string
	}{
		{"unknown event", "?event=nope", "event"},
		{"limit zero", "?limit=0", "limit"},
		{"limit over max", "?limit=201", "limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := getStaffAudit(h, cookie, tc.query)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %q)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"field":"`+tc.field+`"`) {
				t.Errorf("body %q, want a violation naming %q", rec.Body.String(), tc.field)
			}
		})
	}

	// A blank filter is no filter, like the logs viewer's `q` — a client that
	// builds the parameter from an unselected control gets everything rather
	// than a violation.
	if rec := getStaffAudit(h, cookie, "?event="); rec.Code != http.StatusOK {
		t.Errorf("GET ?event=: status = %d, want 200 (a blank filter is no filter)", rec.Code)
	}

	// The CODE, not just the field: the OpenAPI declares which code each
	// violation carries, so a reclassification must fail here.
	for _, tc := range []struct {
		name  string
		query string
		code  string
	}{
		{"unknown event", "?event=nope", "invalidValue"},
		{"limit over max", "?limit=201", "outOfRange"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := getStaffAudit(h, cookie, tc.query)
			if !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
				t.Errorf("body %q, want violation code %q", rec.Body.String(), tc.code)
			}
		})
	}
}

// TestStaffAuditEvents_EmptyLedger pins the fresh-install answer.
func TestStaffAuditEvents_EmptyLedger(t *testing.T) {
	t.Parallel()

	db, h := setupStaffAuditHandler(t)
	cookie := mustSuperAdminSession(t, db, "s1")

	rec := getStaffAudit(h, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"items":[]`) {
		t.Errorf("body = %q, want an empty items array (not null)", body)
	}
}
