package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/identity"
)

// gateHandler records whether the gate let the request through.
type gateHandler struct{ called bool }

func (h *gateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusNoContent)
}

// TestForcePasswordChange_NoSession passes unauthenticated requests through —
// the handlers own their 401s; the gate only acts on a resolved session.
func TestForcePasswordChange_NoSession(t *testing.T) {
	t.Parallel()
	h := &gateHandler{}
	mw := ForcePasswordChange()(h)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !h.called {
		t.Fatal("unauthenticated request must pass the gate")
	}
}

// TestForcePasswordChange_NoFlag passes a normal authenticated session.
func TestForcePasswordChange_NoFlag(t *testing.T) {
	t.Parallel()
	h := &gateHandler{}
	mw := ForcePasswordChange()(h)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(SetSession(req.Context(), &identity.SessionUser{}))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if !h.called {
		t.Fatal("unflagged session must pass the gate")
	}
}

// TestForcePasswordChange_Blocks pins the 403 contract for a flagged session:
// the actionable problem type, the RFC 9457 shape, and that the wrapped
// handler never runs. The gate applies to safe and unsafe methods alike —
// every gated request, not just mutations.
func TestForcePasswordChange_Blocks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
	}{
		{"safe method", http.MethodGet},
		{"unsafe method", http.MethodPost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &gateHandler{}
			mw := ForcePasswordChange()(h)
			req := httptest.NewRequest(tt.method, "/", nil)
			req = req.WithContext(SetSession(req.Context(), &identity.SessionUser{MustChangePassword: true}))
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)

			if h.called {
				t.Fatalf("flagged session must not reach the handler")
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status: got %d, want 403", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("content-type: got %q", ct)
			}

			var body struct {
				Type   string `json:"type"`
				Status int    `json:"status"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if body.Type != "/problems/auth/password-change-required" {
				t.Errorf("problem type: got %q, want /problems/auth/password-change-required", body.Type)
			}
			if body.Status != http.StatusForbidden {
				t.Errorf("problem status: got %d, want 403", body.Status)
			}
		})
	}
}
