package routes

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/media"
)

// Avatar-upload route tests:
// PUT /api/v1/users/me/avatar on the users subtree chain — RequestID →
// TrustedOrigin → Session → ForcePasswordChange → CSRF. The multipart
// builder is the media suite's `multipartUploadBody` (same package — the
// framing is shared, so the fixture builder is too).

// doAvatarUpload sends a PUT multipart request to the avatar endpoint with
// the given auth context (cookie from setupUsersMux; CSRF fixed zero bytes
// as created by that setup).
func doAvatarUpload(t *testing.T, mux *http.ServeMux, body []byte, contentType, cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/avatar", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Origin", "http://localhost:5173")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "sf_session", Value: cookie})
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func avatarJPEGFixture(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatalf("encode jpeg fixture: %v", err)
	}
	return buf.Bytes()
}

func avatarStoredPath(t *testing.T, db *sql.DB, userID string) string {
	t.Helper()
	var stored string
	err := db.QueryRow(`SELECT avatar_url FROM users WHERE id = ?`, userID).Scan(&stored)
	if err != nil {
		t.Fatalf("read avatar_url: %v", err)
	}
	return stored
}

// TestAvatarUpload_Success pins the happy path: a role=user session uploads
// a JPEG, the response is the refreshed profile with the absolute served
// PNG URL, the row stores the 4-segment storage form, and the processed
// file is a 200×200 PNG at the storage layout.
func TestAvatarUpload_Success(t *testing.T) {
	t.Parallel()

	mux, db, cookie, dir := setupUsersMux(t)
	csrf := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	body, ct := multipartUploadBody(t, "me.jpg", avatarJPEGFixture(t, 400, 300), nil)
	rec := doAvatarUpload(t, mux, body, ct, cookie, csrf)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Username  string  `json:"username"`
		AvatarURL *string `json:"avatarUrl"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Username != "Katakuri" {
		t.Errorf("username = %q, want Katakuri (refreshed profile)", resp.Username)
	}
	if resp.AvatarURL == nil {
		t.Fatal("avatarUrl = null, want the absolute served URL")
	}
	// The served URL is base + /media/images/<32hex>.png — the 2-hex
	// storage subdirectory never appears on the wire; avatars are
	// always PNG.
	if !strings.HasPrefix(*resp.AvatarURL, "http://localhost:3000/media/images/") ||
		!strings.HasSuffix(*resp.AvatarURL, ".png") {
		t.Fatalf("avatarUrl = %q, want http://localhost:3000/media/images/<32hex>.png", *resp.AvatarURL)
	}

	stored := avatarStoredPath(t, db, "u1")
	if !strings.HasPrefix(stored, "media/images/") || !strings.HasSuffix(stored, ".png") {
		t.Fatalf("stored avatar_url = %q, want the storage-relative media/images/… form", stored)
	}
	parts := strings.Split(stored, "/")
	if len(parts) != 4 {
		t.Errorf("stored avatar_url = %q, want the 4-segment storage form", stored)
	}

	// The processed file exists at the storage layout and is exactly
	// 200×200 PNG.
	f, err := os.Open(filepath.Join(dir, stored))
	if err != nil {
		t.Fatalf("open processed avatar: %v", err)
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode processed avatar: %v", err)
	}
	if format != "png" {
		t.Errorf("processed format = %q, want png", format)
	}
	if cfg.Width != 200 || cfg.Height != 200 {
		t.Errorf("processed dims = %dx%d, want exactly 200x200", cfg.Width, cfg.Height)
	}
}

// TestAvatarUpload_Validation pins the framing/error contract (mirrors
// POST /api/v1/media): missing file → 422 file:required, unknown
// fields → 400, undecodable bytes → 422 file:invalidFormat, oversized
// source → 413 file:tooLarge.
func TestAvatarUpload_Validation(t *testing.T) {
	t.Parallel()

	mux, _, cookie, _ := setupUsersMux(t)
	csrf := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	t.Run("missing file", func(t *testing.T) {
		// An EMPTY multipart body (no parts at all): no file part → the
		// 422 {file, required} outcome. A body with unknown non-file parts
		// dies earlier at the strict-fields 400 (pinned in the media
		// upload suite).
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		if err := w.Close(); err != nil {
			t.Fatalf("close writer: %v", err)
		}
		rec := doAvatarUpload(t, mux, buf.Bytes(), w.FormDataContentType(), cookie, csrf)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422 (body %s)", rec.Code, rec.Body.String())
		}
		var body struct {
			Violations []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"violations"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body.Violations) != 1 || body.Violations[0].Field != "file" || body.Violations[0].Code != "required" {
			t.Errorf("violations = %+v, want [{file required}]", body.Violations)
		}
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		body, ct := multipartUploadBody(t, "me.jpg", avatarJPEGFixture(t, 40, 30), map[string]string{"note": "x"})
		rec := doAvatarUpload(t, mux, body, ct, cookie, csrf)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("undecodable bytes", func(t *testing.T) {
		body, ct := multipartUploadBody(t, "me.jpg", []byte("definitely not an image"), nil)
		rec := doAvatarUpload(t, mux, body, ct, cookie, csrf)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422 (body %s)", rec.Code, rec.Body.String())
		}
		var body422 struct {
			Violations []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"violations"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body422); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body422.Violations) != 1 || body422.Violations[0].Field != "file" || body422.Violations[0].Code != "invalidFormat" {
			t.Errorf("violations = %+v, want [{file invalidFormat}]", body422.Violations)
		}
	})

	t.Run("oversized source", func(t *testing.T) {
		// Over the 10 MB input bound — the shared writeProcessError maps
		// ErrInputTooLarge to the same 413 file:tooLarge.
		body, ct := multipartUploadBody(t, "me.jpg", bytes.Repeat([]byte{0x00}, media.MaxInputBytes+1), nil)
		rec := doAvatarUpload(t, mux, body, ct, cookie, csrf)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
		}
		var body413 struct {
			Violations []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"violations"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body413); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body413.Violations) != 1 || body413.Violations[0].Field != "file" || body413.Violations[0].Code != "tooLarge" {
			t.Errorf("violations = %+v, want [{file tooLarge}]", body413.Violations)
		}
	})

	t.Run("huge header dimensions", func(t *testing.T) {
		// A valid PNG header claiming 50000×50000 — the per-axis 8192 bound
		// rejects before decode; the shared mapping emits 413.
		body, ct := multipartUploadBody(t, "me.png", pngHeaderWithDimensions(t, 50000, 50000), nil)
		rec := doAvatarUpload(t, mux, body, ct, cookie, csrf)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
		}
	})
}

// TestAvatarUpload_AuthRequirements pins the boundary: unauthenticated →
// 401, missing CSRF → 403 (the trusted-origin chain).
func TestAvatarUpload_AuthRequirements(t *testing.T) {
	t.Parallel()

	mux, _, cookie, _ := setupUsersMux(t)
	csrf := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	body, ct := multipartUploadBody(t, "me.jpg", avatarJPEGFixture(t, 40, 30), nil)

	t.Run("unauthenticated", func(t *testing.T) {
		rec := doAvatarUpload(t, mux, body, ct, "", csrf)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing csrf", func(t *testing.T) {
		rec := doAvatarUpload(t, mux, body, ct, cookie, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 for a missing CSRF token", rec.Code)
		}
	})
}

// TestAvatarUpload_ReplacementKeepsOldFile pins the replacement
// rule: a second upload stores a NEW id, updates the reference, and leaves
// the old file on disk (the sweep reclaims it after the grace window).
func TestAvatarUpload_ReplacementKeepsOldFile(t *testing.T) {
	t.Parallel()

	mux, db, cookie, dir := setupUsersMux(t)
	csrf := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	first, ct := multipartUploadBody(t, "a.jpg", avatarJPEGFixture(t, 60, 60), nil)
	rec1 := doAvatarUpload(t, mux, first, ct, cookie, csrf)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first upload: status = %d, want 200 (body %s)", rec1.Code, rec1.Body.String())
	}
	oldStored := avatarStoredPath(t, db, "u1")

	second, ct2 := multipartUploadBody(t, "b.png", func() []byte {
		var buf bytes.Buffer
		if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 30, 90))); err != nil {
			t.Fatalf("encode png fixture: %v", err)
		}
		return buf.Bytes()
	}(), nil)
	rec2 := doAvatarUpload(t, mux, second, ct2, cookie, csrf)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second upload: status = %d, want 200 (body %s)", rec2.Code, rec2.Body.String())
	}
	newStored := avatarStoredPath(t, db, "u1")
	if newStored == oldStored {
		t.Fatalf("replacement kept the same reference %q — want a fresh id", newStored)
	}

	// The old file must STILL EXIST on disk (never immediately deleted —
	// the sweep reclaims it after the grace window), and both
	// references keep the storage grammar.
	for _, ref := range []string{oldStored, newStored} {
		if !strings.HasPrefix(ref, "media/images/") {
			t.Errorf("stored reference %q lost the storage prefix", ref)
		}
		if _, err := os.Stat(filepath.Join(dir, ref)); err != nil {
			t.Errorf("file for %q missing: %v (old avatar files are not deleted on replacement)", ref, err)
		}
	}
}

// TestAvatarUpload_UpdatedAtBumps pins the row write: the avatar change
// stamps updated_at_ms (a profile change), without touching auth_version
// (sessions stay valid — a picture is not one of the revocation events).
func TestAvatarUpload_UpdatedAtBumps(t *testing.T) {
	t.Parallel()

	mux, db, cookie, _ := setupUsersMux(t)
	csrf := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	body, ct := multipartUploadBody(t, "me.jpg", avatarJPEGFixture(t, 40, 30), nil)
	rec := doAvatarUpload(t, mux, body, ct, cookie, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var row struct {
		UpdatedAtMS int64 `json:"-"`
		AuthVersion int64 `json:"-"`
	}
	err := db.QueryRow(`SELECT updated_at_ms, auth_version FROM users WHERE id = 'u1'`).
		Scan(&row.UpdatedAtMS, &row.AuthVersion)
	if err != nil {
		t.Fatalf("read user row: %v", err)
	}
	if row.UpdatedAtMS <= 1_700_000_000_000 {
		t.Errorf("updated_at_ms = %d, want a post-fixture stamp", row.UpdatedAtMS)
	}
	if row.AuthVersion != 1 {
		t.Errorf("auth_version = %d, want unchanged 1 (avatar is not a revocation event)", row.AuthVersion)
	}
}

// TestAvatarUpload_StoreMissIs401 pins the deletion race: the session
// outlives the user row → the generic 401, no distinguishable outcome.
func TestAvatarUpload_StoreMissIs401(t *testing.T) {
	t.Parallel()

	mux, db, cookie, _ := setupUsersMux(t)
	csrf := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if _, err := db.Exec(`DELETE FROM users WHERE id = 'u1'`); err != nil {
		t.Fatalf("delete fixture user: %v", err)
	}

	body, ct := multipartUploadBody(t, "me.jpg", avatarJPEGFixture(t, 40, 30), nil)
	rec := doAvatarUpload(t, mux, body, ct, cookie, csrf)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}
