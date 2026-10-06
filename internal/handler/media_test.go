package handler_test

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"sick-fansubs/internal/handler"
)

// validID is a well-formed 32-hex media ID; validID[:2] is its directory.
const validID = "abcdef0123456789abcdef0123456789"

// writeFixtureImage renders a tiny in-memory image through the real
// encoders (jpeg.Encode / png.Encode) so serving tests exercise actual
// image bytes, not fabricated content.
func writeFixtureImage(t *testing.T, dir, name string, encode func(io.Writer, image.Image) error) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer f.Close()
	if err := encode(f, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
}

// setupMedia creates a data directory containing one jpg and one png media
// fixture and returns the ServeMedia handler.
func setupMedia(t *testing.T) http.HandlerFunc {
	t.Helper()
	dir := testDataDir(t)
	mediaDir := filepath.Join(dir, "media", "images", validID[:2])
	writeFixtureImage(t, mediaDir, validID+".jpg", func(w io.Writer, m image.Image) error {
		return jpeg.Encode(w, m, &jpeg.Options{Quality: 85})
	})
	writeFixtureImage(t, mediaDir, validID+".png", png.Encode)
	return handler.ServeMedia(dir)
}

// doMedia serves one segment through the handler directly, setting the path
// value the way the ServeMux would. The request URL is a fixed safe target:
// the handler only reads r.PathValue, and a raw control character (null byte)
// is not a valid request URL.
func doMedia(t *testing.T, h http.HandlerFunc, segment string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/media/images/x.jpg", nil)
	req.SetPathValue("file", segment)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestServeMedia_ServesJPEG(t *testing.T) {
	t.Parallel()

	h := setupMedia(t)
	rec := doMedia(t, h, validID+".jpg")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable policy", got)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected non-empty image body")
	}
}

func TestServeMedia_ServesPNG(t *testing.T) {
	t.Parallel()

	h := setupMedia(t)
	rec := doMedia(t, h, validID+".png")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
}

func TestServeMedia_WellFormedButMissingIs404(t *testing.T) {
	t.Parallel()

	h := setupMedia(t)
	rec := doMedia(t, h, "ffffffffffffffffffffffffffffffff.jpg")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestServeMedia_MalformedIs400(t *testing.T) {
	t.Parallel()

	h := setupMedia(t)
	for _, segment := range []string{
		"..",
		"../etc/passwd",
		validID[:31] + "g.jpg",
		validID + ".webp",
		validID + ".jpg\x00",
	} {
		rec := doMedia(t, h, segment)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("segment %q: status = %d, want 400", segment, rec.Code)
		}
	}
}
