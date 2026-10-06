package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// testFrontendFS is a minimal web/dist layout. The seam is deliberate: the
// embedded build state (dev machines carry only .gitkeep) must never decide
// what these tests see — they pin serving behavior over a known layout.
func testFrontendFS() fstest.MapFS {
	return fstest.MapFS{
		"web/dist/index.html":           {Data: []byte(`<!doctype html><html><head><meta name="app-version" content="dev"></head><body>shell</body></html>`)},
		"web/dist/sw.js":                {Data: []byte(`const SF_VERSION = "dev";\nself.addEventListener("fetch", () => {});\n`)},
		"web/dist/manifest.json":        {Data: []byte(`{"name":"Sick-Fansubs","icons":[{"src":"/icons/icon-192.png"}]}`)},
		"web/dist/icons/icon-192.png":   {Data: []byte("png-bytes")},
		"web/dist/assets/app-abc123.js": {Data: []byte("console.log('hashed')")},
	}
}

func newTestFrontend(t *testing.T, version string) http.Handler {
	t.Helper()
	h, err := newFrontendHandler(testFrontendFS(), version)
	if err != nil {
		t.Fatalf("newFrontendHandler: %v", err)
	}
	return h
}

func doFrontend(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestFrontendIndexInjectsVersionAndSecurityHeaders(t *testing.T) {
	h := newTestFrontend(t, "2.0.0-beta.10")

	rec := doFrontend(t, h, http.MethodGet, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("CSP missing on the HTML response")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if !strings.Contains(rec.Body.String(), `content="2.0.0-beta.10"`) {
		t.Errorf("index body lacks injected version: %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `content="dev"`) {
		t.Error("index body still carries the dev placeholder")
	}
}

func TestFrontendSPAFallbackServesIndex(t *testing.T) {
	h := newTestFrontend(t, "v1")

	rec := doFrontend(t, h, http.MethodGet, "/blog/some-post")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if !strings.Contains(rec.Body.String(), "shell") {
		t.Error("SPA fallback did not serve the index shell")
	}
}

func TestFrontendMissingAssetAnswers404(t *testing.T) {
	h := newTestFrontend(t, "v1")

	rec := doFrontend(t, h, http.MethodGet, "/assets/gone-abc123.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "shell") {
		t.Error("a missing hashed asset must not answer the SPA shell")
	}
}

func TestFrontendServiceWorkerVersionInjectionAndNoCache(t *testing.T) {
	h := newTestFrontend(t, "2.0.0-beta.11")

	rec := doFrontend(t, h, http.MethodGet, "/sw.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/javascript", got)
	}
	if !strings.Contains(rec.Body.String(), `SF_VERSION = "2.0.0-beta.11"`) {
		t.Errorf("sw body lacks injected version: %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `SF_VERSION = "dev"`) {
		t.Error("sw body still carries the dev placeholder")
	}
}

func TestFrontendPWAStaticPathsGetNoCache(t *testing.T) {
	h := newTestFrontend(t, "v1")

	rec := doFrontend(t, h, http.MethodGet, "/manifest.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var manifest struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Name != "Sick-Fansubs" {
		t.Errorf("manifest name = %q, want Sick-Fansubs", manifest.Name)
	}

	for _, target := range []string{"/icons/icon-192.png"} {
		rec := doFrontend(t, h, http.MethodGet, target)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", target, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want no-cache", target, got)
		}
	}
}

func TestFrontendHashedAssetsKeepImmutableCache(t *testing.T) {
	h := newTestFrontend(t, "v1")

	rec := doFrontend(t, h, http.MethodGet, "/assets/app-abc123.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable", got)
	}
}

func TestFrontendRejectsNonGetHead(t *testing.T) {
	h := newTestFrontend(t, "v1")

	rec := doFrontend(t, h, http.MethodPost, "/sw.js")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want GET, HEAD", got)
	}
}

func TestFrontendMissingSWReturns503(t *testing.T) {
	fsys := testFrontendFS()
	delete(fsys, "web/dist/sw.js")
	h, err := newFrontendHandler(fsys, "v1")
	if err != nil {
		t.Fatalf("newFrontendHandler: %v", err)
	}

	rec := doFrontend(t, h, http.MethodGet, "/sw.js")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestFrontendMissingIndexReturns503(t *testing.T) {
	fsys := testFrontendFS()
	delete(fsys, "web/dist/index.html")
	h, err := newFrontendHandler(fsys, "v1")
	if err != nil {
		t.Fatalf("newFrontendHandler: %v", err)
	}

	// Both the root and the SPA fallback share the processed index; with it
	// missing, neither may answer a broken shell.
	for _, target := range []string{"/", "/blog/some-post"} {
		rec := doFrontend(t, h, http.MethodGet, target)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503", target, rec.Code)
		}
	}
}

func TestNewFrontendHandlerRejectsMissingDist(t *testing.T) {
	// The production embed (embed.FS) implements fs.SubFS and errors when
	// web/dist is absent — that error is what frontendHandler turns into
	// the 503 handler. MapFS's lazy fs.Sub fallback never errors, so
	// simulate the embed's Sub contract directly (only the Sub method
	// matters — the embedded MapFS content is irrelevant here).
	fsys := noDistFS{}
	if _, err := newFrontendHandler(fsys, "v1"); err == nil {
		t.Error("newFrontendHandler accepted an FS whose web/dist is missing")
	}
}

// noDistFS behaves like an embed.FS whose web/dist directory is absent.
type noDistFS struct{ fstest.MapFS }

func (noDistFS) Sub(string) (fs.FS, error) { return nil, fs.ErrNotExist }
