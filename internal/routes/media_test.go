package routes

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/media"
)

const mediaRouteID = "abcdef0123456789abcdef0123456789"

// setupMediaMux creates a data directory with one served jpg, an empty
// migrated database, and a mux carrying the media routes (serving + upload).
func setupMediaMux(t *testing.T) (*http.ServeMux, string, *sql.DB) {
	t.Helper()

	dir := testDataDir(t)
	mediaDir := filepath.Join(dir, "media", "images", mediaRouteID[:2])
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatalf("mkdir media fixture: %v", err)
	}
	f, err := os.Create(filepath.Join(mediaDir, mediaRouteID+".jpg"))
	if err != nil {
		t.Fatalf("create media fixture: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatalf("encode media fixture: %v", err)
	}

	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Apply(db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	mux := http.NewServeMux()
	// secure=false, trusted origin + public base = the dev-local pair.
	Media(mux, dir, db, false, "http://localhost:3000", "http://localhost:3000")
	return mux, dir, db
}

func doMediaRoute(t *testing.T, mux *http.ServeMux, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestMediaRoute_ServesFile(t *testing.T) {
	t.Parallel()

	mux, _, _ := setupMediaMux(t)
	rec := doMediaRoute(t, mux, http.MethodGet, "/media/images/"+mediaRouteID+".jpg")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID missing — RequestID middleware must wrap media serving")
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable policy", got)
	}
}

func TestMediaRoute_EncodedSeparatorIs400(t *testing.T) {
	t.Parallel()

	mux, _, _ := setupMediaMux(t)
	// %2F stays inside the segment for matching; the mux decodes it before
	// handing the segment to the handler, whose parser rejects the embedded
	// separator with 400 — the file system is never consulted.
	seg := mediaRouteID[:16] + "%2F" + mediaRouteID[16:] + ".jpg"
	rec := doMediaRoute(t, mux, http.MethodGet, "/media/images/"+seg)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMediaRoute_LiteralTraversalFallsTo404(t *testing.T) {
	t.Parallel()

	mux, _, _ := setupMediaMux(t)
	// The ServeMux cleans ".." from the URL path before matching and
	// redirects the client to the cleaned path (307 here, via ServeHTTP).
	// The target is the media 404 fallback: a problem, never a filesystem
	// read or SPA HTML.
	rec := doMediaRoute(t, mux, http.MethodGet, "/media/images/../etc/passwd")

	if rec.Code < 300 || rec.Code > 399 {
		t.Fatalf("status = %d, want the mux path-cleaning redirect", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("redirect has no Location header")
	}

	followed := doMediaRoute(t, mux, http.MethodGet, loc)
	if followed.Code != http.StatusNotFound {
		t.Fatalf("cleaned path %q: status = %d, want 404", loc, followed.Code)
	}
	if ct := followed.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want problem+json (never SPA HTML)", ct)
	}
}

func TestMediaRoute_MissingIs404(t *testing.T) {
	t.Parallel()

	mux, _, _ := setupMediaMux(t)
	rec := doMediaRoute(t, mux, http.MethodGet, "/media/images/ffffffffffffffffffffffffffffffff.jpg")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMediaRoute_BarePathsAre404Problems(t *testing.T) {
	t.Parallel()

	mux, _, _ := setupMediaMux(t)
	for _, target := range []string{"/media", "/media/", "/media/images", "/media/images/"} {
		rec := doMediaRoute(t, mux, http.MethodGet, target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", target, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q, want problem+json", target, ct)
		}
	}
}

func TestMediaRoute_NonGETIs404Problem(t *testing.T) {
	t.Parallel()

	mux, _, _ := setupMediaMux(t)
	rec := doMediaRoute(t, mux, http.MethodPost, "/media/images/"+mediaRouteID+".jpg")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (media serving is GET-only)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want problem+json", ct)
	}
}

// TestBlogListThumbnailURLServes pins the closed loop the contract requires:
// the absolute thumbnailUrl the blog list EMITS must be the exact URL the
// media route SERVES — the storage subdirectory never appears on the wire.
// Fetching the list and then fetching its thumbnailUrl must return the
// image.
func TestBlogListThumbnailURLServes(t *testing.T) {
	t.Parallel()

	dataDir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := database.Apply(db); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Post whose thumbnail is stored WITH the 2-hex subdirectory, exactly
	// as the migration and future uploads write it.
	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES ('p1', 'T', '', '', 'media/images/ab/abcdef0123456789abcdef0123456789.jpg',
		 'published', 1000, 500, 500)`
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("insert post: %v", err)
	}

	// The processed file at its storage location.
	mediaDir := filepath.Join(dataDir, "media", "images", "ab")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatalf("mkdir media: %v", err)
	}
	f, err := os.Create(filepath.Join(mediaDir, "abcdef0123456789abcdef0123456789.jpg"))
	if err != nil {
		t.Fatalf("create media: %v", err)
	}
	if err := jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatalf("encode media: %v", err)
	}
	f.Close()

	mux := http.NewServeMux()
	Blog(mux, db, dataDir, false, "http://localhost:3000", "http://localhost:3000", nil)
	Media(mux, dataDir, db, false, "http://localhost:3000", "http://localhost:3000")
	APIFallback(mux)

	// Fetch the list and take the emitted thumbnailUrl at face value.
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/v1/blog-posts?limit=1", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listRec.Code)
	}
	var body struct {
		Items []struct {
			ThumbnailURL string `json:"thumbnailUrl"`
		} `json:"items"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ThumbnailURL == "" {
		t.Fatalf("unexpected list body: %+v", body)
	}

	// The emitted URL must not contain the storage subdirectory and must
	// serve the image through the media route.
	u, err := url.Parse(body.Items[0].ThumbnailURL)
	if err != nil {
		t.Fatalf("thumbnailUrl is not a URL: %q", body.Items[0].ThumbnailURL)
	}
	if got, want := u.Path, "/media/images/abcdef0123456789abcdef0123456789.jpg"; got != want {
		t.Fatalf("emitted path = %q, want %q (no storage subdirectory)", got, want)
	}
	imgRec := httptest.NewRecorder()
	mux.ServeHTTP(imgRec, httptest.NewRequest(http.MethodGet, u.Path, nil))
	if imgRec.Code != http.StatusOK {
		t.Fatalf("serving the emitted URL: status = %d, want 200 (body %s)", imgRec.Code, imgRec.Body.String())
	}
}

// doUpload sends a multipart upload request with the given auth context.
func doUpload(t *testing.T, mux *http.ServeMux, body []byte, contentType string, cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/media", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Origin", "http://localhost:3000")
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

func jpegFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 320, 180)), nil); err != nil {
		t.Fatalf("encode jpeg fixture: %v", err)
	}
	return buf.Bytes()
}

// pngHeaderWithDimensions crafts a structurally valid PNG header claiming
// arbitrary dimensions — the smallest file that trips the per-axis and
// total-pixel header pre-checks. Mirror of the media package test helper
// (package-internal there).
func pngHeaderWithDimensions(t *testing.T, w, h uint32) []byte {
	t.Helper()
	sig := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	ihdr := make([]byte, 4+4+13)
	binary.BigEndian.PutUint32(ihdr[0:4], 13)
	copy(ihdr[4:8], "IHDR")
	binary.BigEndian.PutUint32(ihdr[8:12], w)
	binary.BigEndian.PutUint32(ihdr[12:16], h)
	ihdr[16] = 8
	ihdr[17] = 6
	ihdr[18] = 0
	ihdr[19] = 0
	ihdr[20] = 0
	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(ihdr[4:]))
	return append(append(sig, ihdr...), crc...)
}

func TestMediaUpload_Success(t *testing.T) {
	t.Parallel()

	mux, dir, db := setupMediaMux(t)
	cookie, csrf := setupUploadSession(t, db, "admin")

	body, ct := multipartUploadBody(t, "thumb.jpg", jpegFixture(t), nil)
	rec := doUpload(t, mux, body, ct, cookie, csrf)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc == "" || !strings.HasPrefix(loc, "/media/images/") || !strings.HasSuffix(loc, ".jpg") {
		t.Fatalf("Location = %q, want the served URL path /media/images/<32hex>.jpg", loc)
	}
	var resp struct {
		Path string `json:"path"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.HasPrefix(resp.Path, "media/images/") {
		t.Fatalf("path = %q, want the storage-relative media/images/… form", resp.Path)
	}
	if !strings.HasSuffix(resp.Path, ".jpg") && !strings.HasSuffix(resp.Path, ".png") {
		t.Fatalf("path = %q, want jpg or png", resp.Path)
	}
	if resp.URL != "http://localhost:3000/"+loc[1:] {
		t.Fatalf("url = %q, want the absolute served URL of %q", resp.URL, loc)
	}

	// The processed file exists at the storage layout and the emitted URL
	// serves through the public media route.
	if _, err := os.Stat(filepath.Join(dir, resp.Path)); err != nil {
		t.Fatalf("stored file missing: %v", err)
	}
	imgRec := doMediaRoute(t, mux, http.MethodGet, loc)
	if imgRec.Code != http.StatusOK {
		t.Fatalf("serving the uploaded image: status = %d, want 200", imgRec.Code)
	}
}

func TestMediaUpload_RoleMatrix(t *testing.T) {
	t.Parallel()

	mux, _, db := setupMediaMux(t)
	body, ct := multipartUploadBody(t, "t.jpg", jpegFixture(t), nil)

	for _, role := range []string{"moderator", "user"} {
		t.Run(role, func(t *testing.T) {
			cookie, csrf := setupUploadSession(t, db, role)
			rec := doUpload(t, mux, body, ct, cookie, csrf)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 for role %q", rec.Code, role)
			}
		})
	}

	t.Run("anonymous", func(t *testing.T) {
		rec := doUpload(t, mux, body, ct, "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for an unauthenticated upload", rec.Code)
		}
	})

	t.Run("super-admin succeeds", func(t *testing.T) {
		cookie, csrf := setupUploadSession(t, db, "super-admin")
		rec := doUpload(t, mux, body, ct, cookie, csrf)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 for super-admin (body %s)", rec.Code, rec.Body.String())
		}
	})
}

func TestMediaUpload_MissingCSRFIs403(t *testing.T) {
	t.Parallel()

	mux, _, db := setupMediaMux(t)
	cookie, _ := setupUploadSession(t, db, "admin")
	body, ct := multipartUploadBody(t, "t.jpg", jpegFixture(t), nil)

	rec := doUpload(t, mux, body, ct, cookie, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a missing CSRF token", rec.Code)
	}
}

func TestMediaUpload_Validation(t *testing.T) {
	t.Parallel()

	mux, _, db := setupMediaMux(t)
	cookie, csrf := setupUploadSession(t, db, "admin")

	t.Run("unknown field rejected", func(t *testing.T) {
		// A non-file field alongside the file: strict known fields —
		// 400, never silently accepted.
		body, ct := multipartUploadBody(t, "t.jpg", jpegFixture(t), map[string]string{"note": "x"})
		rec := doUpload(t, mux, body, ct, cookie, csrf)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (unknown field note — strict fields)", rec.Code)
		}
	})

	t.Run("no parts at all", func(t *testing.T) {
		// A multipart body with zero parts: no "file" — 422 {file, required}.
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		w.Close()
		rec := doUpload(t, mux, buf.Bytes(), w.FormDataContentType(), cookie, csrf)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422 {file, required}", rec.Code)
		}
	})

	t.Run("garbage bytes", func(t *testing.T) {
		body, ct := multipartUploadBody(t, "t.jpg", []byte("this is not an image"), nil)
		rec := doUpload(t, mux, body, ct, cookie, csrf)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422 {file, invalidFormat}", rec.Code)
		}
	})

	t.Run("pixel-bound PNG header", func(t *testing.T) {
		// 5000×5000 = 25 MP > 16 MP, per-axis fine — trips the
		// total-pixel bound (413 tooLarge, never a decode).
		raw := pngHeaderWithDimensions(t, 5000, 5000)
		body, ct := multipartUploadBody(t, "t.png", raw, nil)
		rec := doUpload(t, mux, body, ct, cookie, csrf)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413 {file, tooLarge}", rec.Code)
		}
	})

	t.Run("GET on the upload path is a 404 problem", func(t *testing.T) {
		rec := doMediaRoute(t, mux, http.MethodGet, "/api/v1/media")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for GET /api/v1/media", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("Content-Type = %q, want problem+json", ct)
		}
	})

	t.Run("DELETE on the upload path is a 404 problem", func(t *testing.T) {
		rec := doMediaRoute(t, mux, http.MethodDelete, "/api/v1/media")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for DELETE /api/v1/media", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("Content-Type = %q, want problem+json", ct)
		}
	})
}

// doMediaDelete sends a DELETE media request with the given auth context
// (mirror of doUpload — the full authenticated chain is the point).
func doMediaDelete(mux *http.ServeMux, cookie, csrf, file string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/media/"+file, nil)
	req.Header.Set("Origin", "http://localhost:3000")
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

// writeOrphanFixture writes an unreferenced served media file into the
// fixture data dir and returns its served segment.
func writeOrphanFixture(t *testing.T, dir string) string {
	t.Helper()
	const id = "fedcba9876543210fedcba9876543210"
	full := filepath.Join(dir, media.RelativePath(id, media.ExtJPG))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir orphan fixture: %v", err)
	}
	f, err := os.Create(full)
	if err != nil {
		t.Fatalf("create orphan fixture: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatalf("encode orphan fixture: %v", err)
	}
	return id + ".jpg"
}

// TestMediaDeleteRoute pins the DELETE registration: the
// full authenticated chain, the role floor, and the delete outcomes.
func TestMediaDeleteRoute(t *testing.T) {
	t.Parallel()

	t.Run("unauthenticated is 401", func(t *testing.T) {
		mux, _, _ := setupMediaMux(t)
		rec := doMediaDelete(mux, "", "", mediaRouteID+".jpg")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("moderator is 403", func(t *testing.T) {
		mux, _, db := setupMediaMux(t)
		cookie, csrf := setupUploadSession(t, db, "moderator")
		rec := doMediaDelete(mux, cookie, csrf, mediaRouteID+".jpg")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("missing CSRF is 403", func(t *testing.T) {
		mux, _, db := setupMediaMux(t)
		cookie, _ := setupUploadSession(t, db, "admin")
		rec := doMediaDelete(mux, cookie, "", mediaRouteID+".jpg")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (CSRF is mandatory on mutations)", rec.Code)
		}
	})

	t.Run("referenced file is 409 and kept", func(t *testing.T) {
		mux, dir, db := setupMediaMux(t)
		if _, err := db.Exec(`INSERT INTO blog_posts
			(id, title, subtitle, description, thumbnail_url, status, published_at_ms, created_at_ms, updated_at_ms)
			VALUES ('b1', 'T', '', '', ?, 'published', 1000, 500, 500)`,
			media.RelativePath(mediaRouteID, media.ExtJPG)); err != nil {
			t.Fatalf("seed reference: %v", err)
		}
		cookie, csrf := setupUploadSession(t, db, "admin")
		rec := doMediaDelete(mux, cookie, csrf, mediaRouteID+".jpg")
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409", rec.Code)
		}
		if _, err := os.Stat(filepath.Join(dir, media.RelativePath(mediaRouteID, media.ExtJPG))); err != nil {
			t.Errorf("referenced file was removed: %v", err)
		}
	})

	t.Run("admin deletes an unreferenced file with 204", func(t *testing.T) {
		mux, dir, db := setupMediaMux(t)
		seg := writeOrphanFixture(t, dir)
		cookie, csrf := setupUploadSession(t, db, "admin")
		rec := doMediaDelete(mux, cookie, csrf, seg)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
		id := seg[:len(seg)-4] // strip .jpg
		if _, err := os.Stat(filepath.Join(dir, media.RelativePath(id, media.ExtJPG))); !os.IsNotExist(err) {
			t.Errorf("orphan file still exists: %v", err)
		}
	})
}
