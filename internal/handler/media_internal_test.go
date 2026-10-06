package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"sick-fansubs/internal/media"
)

// TestParseMediaFile pins the serving-segment contract:
// exactly 32 lowercase hex characters and an accepted extension — the gate
// before any filesystem path is built.
func TestParseMediaFile(t *testing.T) {
	t.Parallel()

	const valid = "abcdef0123456789abcdef0123456789"

	tests := []struct {
		name   string
		file   string
		wantOK bool
	}{
		{"jpg accepted", valid + ".jpg", true},
		{"parser-tolerated jpeg accepted", valid + ".jpeg", true},
		{"png accepted", valid + ".png", true},
		{"empty segment", "", false},
		{"no extension", "no-extension", false},
		{"dot at start", ".jpg", false},
		{"dot at end", valid + ".", false},
		{"extension is a single known one", valid + ".jpg.jpg", false},
		{"webp never served", valid + ".webp", false},
		{"extension case-sensitive", valid + ".JPG", false},
		{"id too short", valid[:31] + ".jpg", false},
		{"id too long", valid + "0.jpg", false},
		{"non-hex id", valid[:31] + "G.jpg", false},
		{"uppercase hex id", valid[:31] + "A.jpg", false},
		{"traversal", "..", false},
		{"traversal with separators", "../etc/passwd", false},
		{"embedded separator", valid[:16] + "/" + valid[16:] + ".jpg", false},
		{"null byte", valid + ".jpg\x00", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, ok := parseMediaFile(tt.file)
			if ok != tt.wantOK {
				t.Errorf("parseMediaFile(%q) ok = %v, want %v", tt.file, ok, tt.wantOK)
			}
		})
	}
}

// TestReadUploadFile_FramingArms pins the shared multipart framing: a body
// past the frame (file bound + slack) is a pre-parse 413, a duplicate "file"
// part is 400, and a single differently named part is 400.
func TestReadUploadFile_FramingArms(t *testing.T) {
	t.Parallel()

	// attempt builds the multipart request, runs readUploadFile, and returns
	// the written status plus whether a file part was returned.
	attempt := func(t *testing.T, build func(*multipart.Writer)) (int, bool) {
		t.Helper()
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		build(w)
		if err := w.Close(); err != nil {
			t.Fatalf("close multipart writer: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/media", bytes.NewReader(buf.Bytes()))
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		_, ok := readUploadFile(rec, req)
		return rec.Code, ok
	}

	writeFile := func(t *testing.T, w *multipart.Writer, field, content string) {
		t.Helper()
		part, err := w.CreateFormFile(field, "t.jpg")
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}

	t.Run("whole body past the frame", func(t *testing.T) {
		t.Parallel()
		code, ok := attempt(t, func(w *multipart.Writer) {
			// One byte past file bound + slack: MaxBytesReader trips during
			// ParseMultipartForm, before any pipeline work.
			part, err := w.CreateFormFile("file", "huge.jpg")
			if err != nil {
				t.Fatalf("create file part: %v", err)
			}
			if _, err := part.Write(make([]byte, media.MaxInputBytes+mediaUploadSlack+1)); err != nil {
				t.Fatalf("write file part: %v", err)
			}
		})
		if ok || code != http.StatusRequestEntityTooLarge {
			t.Fatalf("readUploadFile(whole body one byte over the frame) = (ok=%v, status %d), want 413 written", ok, code)
		}
	})

	t.Run("duplicate file parts", func(t *testing.T) {
		t.Parallel()
		code, ok := attempt(t, func(w *multipart.Writer) {
			writeFile(t, w, "file", "x")
			writeFile(t, w, "file", "y")
		})
		if ok || code != http.StatusBadRequest {
			t.Fatalf("readUploadFile(duplicate file parts) = (ok=%v, status %d), want 400 written", ok, code)
		}
	})

	t.Run("single part not named file", func(t *testing.T) {
		t.Parallel()
		code, ok := attempt(t, func(w *multipart.Writer) {
			writeFile(t, w, "image", "x")
		})
		if ok || code != http.StatusBadRequest {
			t.Fatalf("readUploadFile(single part not named file) = (ok=%v, status %d), want 400 written", ok, code)
		}
	})
}

// TestMediaURL pins the URL-joining contract of mediaURL: the storage
// subdirectory is dropped, a base path prefix is kept, a flat stored path
// stays flat, a legacy absolute URL passes through, and a value failing the
// scheme guard emits the empty string.
func TestMediaURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		base   string
		stored string
		want   string
	}{
		{"storage subdirectory dropped", "https://sickfansubs.com", "media/images/ab/cdef0123456789abcdef0123456789.jpg",
			"https://sickfansubs.com/media/images/cdef0123456789abcdef0123456789.jpg"},
		{"base path prefix kept", "https://cdn.example.com/static", "media/images/ab/cdef0123456789abcdef0123456789.jpg",
			"https://cdn.example.com/static/media/images/cdef0123456789abcdef0123456789.jpg"},
		{"flat stored path stays flat", "https://sickfansubs.com", "media/images/cdef0123456789abcdef0123456789.jpg",
			"https://sickfansubs.com/media/images/cdef0123456789abcdef0123456789.jpg"},
		{"legacy absolute passes through", "https://sickfansubs.com", "https://sickfansubs.com/media/images/old-key.webp",
			"https://sickfansubs.com/media/images/old-key.webp"},
		{"javascript scheme rejected", "https://sickfansubs.com", "javascript:alert(1)", ""},
		{"data scheme rejected", "https://sickfansubs.com", "data:image/png;base64,AAAA", ""},
		{"relative junk rejected", "https://sickfansubs.com", "not-a-url", ""},
		{"empty stays empty", "https://sickfansubs.com", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mediaURL(tt.base, tt.stored); got != tt.want {
				t.Errorf("mediaURL(%q, %q) = %q, want %q", tt.base, tt.stored, got, tt.want)
			}
		})
	}
}
