package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func process(t *testing.T, dataDir, id string, raw []byte) string {
	t.Helper()
	rel, err := ProcessContentThumbnail(dataDir, id, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ProcessContentThumbnail: %v", err)
	}
	return rel
}

func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg fixture: %v", err)
	}
	return buf.Bytes()
}

func TestProcessContentThumbnail_JPEGStaysJPEG(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rel := process(t, dir, validID, encodeJPEG(t, 320, 180))

	if got, want := rel, RelativePath(validID, ExtJPG); got != want {
		t.Errorf("rel = %q, want %q", got, want)
	}
	out, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(out)); err != nil || format != "jpeg" {
		t.Errorf("output decodes as %q (err %v), want jpeg", format, err)
	}
}

func TestProcessContentThumbnail_PNGWithAlphaStaysPNG(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.NRGBA{A: 128}) // one translucent pixel
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}

	dir := t.TempDir()
	rel := process(t, dir, validID, buf.Bytes())

	if got, want := rel, RelativePath(validID, ExtPNG); got != want {
		t.Errorf("rel = %q, want %q", got, want)
	}
}

func TestProcessContentThumbnail_OpaquePNGBecomesJPEG(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	// Zero-value NRGBA pixels are fully transparent (alpha 0) — fill the
	// fixture so it is genuinely opaque.
	for y := range 16 {
		for x := range 16 {
			img.Set(x, y, color.NRGBA{R: uint8(x), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}

	dir := t.TempDir()
	rel := process(t, dir, validID, buf.Bytes())

	if got, want := rel, RelativePath(validID, ExtJPG); got != want {
		t.Errorf("rel = %q, want %q (fully opaque PNG re-encodes as JPEG 85)", got, want)
	}
}

// The two minimal WebP fixtures below are standard 1×1 test vectors (one
// lossy VP8, one lossless VP8L). WebP is the dominant legacy format — the
// pipeline must decode it (WebP input accepted).
func TestProcessContentThumbnail_WebPDecodes(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		name string
		b64  string
	}{
		{"vp8-lossy", "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA"},
		{"vp8l-lossless", "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="},
	}
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			raw, err := base64.StdEncoding.DecodeString(fx.b64)
			if err != nil {
				t.Fatalf("bad fixture: %v", err)
			}
			dir := t.TempDir()
			rel := process(t, dir, validID, raw)
			if !strings.HasSuffix(rel, ".jpg") && !strings.HasSuffix(rel, ".png") {
				t.Errorf("rel = %q, want jpg or png output", rel)
			}
		})
	}
}

func TestProcessContentThumbnail_ResizesToFit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rel := process(t, dir, validID, encodeJPEG(t, 2000, 1000))

	out, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	// 2000×1000 scaled by min(1280/2000, 720/1000) = 0.64 → 1280×640.
	if cfg.Width != 1280 || cfg.Height != 640 {
		t.Errorf("output dims = %dx%d, want 1280x640 (proportional fit)", cfg.Width, cfg.Height)
	}
}

func TestProcessContentThumbnail_SmallImageIsNotUpscaled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rel := process(t, dir, validID, encodeJPEG(t, 320, 180))

	out, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if cfg.Width != 320 || cfg.Height != 180 {
		t.Errorf("output dims = %dx%d, want unchanged 320x180", cfg.Width, cfg.Height)
	}
}

// TestProcessContentThumbnail_ExtremeAspectFloorsEdges pins that a source
// with one nearly-zero edge still produces a valid image: the proportional
// fit rounds each edge independently, and a rounded-to-zero edge would
// encode an invalid image (or fail the encode).
func TestProcessContentThumbnail_ExtremeAspectFloorsEdges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		w, h         int
		wantW, wantH int
	}{
		{"one-pixel-wide source", 1, 2000, 1, 720},
		{"one-pixel-tall source", 3000, 1, 1280, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			rel := process(t, dir, validID, encodeJPEG(t, tt.w, tt.h))

			out, err := os.ReadFile(filepath.Join(dir, rel))
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("decode output: %v", err)
			}
			if cfg.Width != tt.wantW || cfg.Height != tt.wantH {
				t.Errorf("ProcessContentThumbnail(%dx%d) dims = %dx%d, want %dx%d",
					tt.w, tt.h, cfg.Width, cfg.Height, tt.wantW, tt.wantH)
			}
		})
	}
}

// TestProcessContentThumbnail_RejectsEmptyAndZeroDimension pins the two
// degenerate sources at the header stage: a zero-length body and a PNG
// header claiming zero pixels are both classification failures, never a
// panic or an encode of an invalid image.
func TestProcessContentThumbnail_RejectsEmptyAndZeroDimension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
	}{
		{"empty body", nil},
		{"zero-dimension png header", pngHeaderWithDimensions(t, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ProcessContentThumbnail(t.TempDir(), validID, bytes.NewReader(tt.raw))
			if !errors.Is(err, ErrDecodeHeader) {
				t.Errorf("ProcessContentThumbnail(%s) err = %v, want ErrDecodeHeader", tt.name, err)
			}
		})
	}
}

// pngHeaderWithDimensions crafts a structurally valid PNG header claiming
// arbitrary dimensions — the smallest possible file that trips the
// DecodeConfig pre-check (reject before pixel allocation).
func pngHeaderWithDimensions(t *testing.T, w, h uint32) []byte {
	t.Helper()
	sig := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	// IHDR chunk: 4-byte length + type + 13-byte data, then a 4-byte CRC
	// over type+data.
	ihdr := make([]byte, 4+4+13)
	binary.BigEndian.PutUint32(ihdr[0:4], 13)
	copy(ihdr[4:8], "IHDR")
	binary.BigEndian.PutUint32(ihdr[8:12], w)
	binary.BigEndian.PutUint32(ihdr[12:16], h)
	ihdr[16] = 8 // bit depth
	ihdr[17] = 6 // RGBA
	ihdr[18] = 0 // compression
	ihdr[19] = 0 // filter
	ihdr[20] = 0 // interlace
	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(ihdr[4:]))
	return append(append(sig, ihdr...), crc...)
}

func TestProcessContentThumbnail_RejectsHugeHeaderBeforeDecode(t *testing.T) {
	t.Parallel()

	raw := pngHeaderWithDimensions(t, 50000, 50000)
	_, err := ProcessContentThumbnail(t.TempDir(), validID, bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected rejection for 50000x50000 header")
	}
	if !errors.Is(err, ErrDimensionsTooBig) {
		t.Errorf("err = %v, want ErrDimensionsTooBig", err)
	}
}

func TestProcessContentThumbnail_RejectsPixelBombHeader(t *testing.T) {
	t.Parallel()

	// 5000×5000 = 25 MP — under the 8192 per-axis bound (so the dimension
	// check passes) but over the 16 MP total-pixel bound.
	raw := pngHeaderWithDimensions(t, 5000, 5000)
	_, err := ProcessContentThumbnail(t.TempDir(), validID, bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected rejection for the 25 MP header")
	}
	if !errors.Is(err, ErrPixelsTooBig) {
		t.Errorf("err = %v, want ErrPixelsTooBig", err)
	}
}

func TestProcessContentThumbnail_AcceptsUnderPixelBound(t *testing.T) {
	t.Parallel()

	// 4096×4096 = 16 MP exactly — the boundary value itself passes
	// (strictly greater rejects).
	raw := pngHeaderWithDimensions(t, 4096, 4096)
	dir := t.TempDir()
	// The header is not a decodable image (no pixel data), so the full
	// pipeline fails at decode — which is the point: it got PAST the pixel
	// check (no ErrPixelsTooBig / ErrDimensionsTooBig in the error).
	_, err := ProcessContentThumbnail(dir, validID, bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected a decode failure for a header-only fixture")
	}
	if errors.Is(err, ErrPixelsTooBig) || errors.Is(err, ErrDimensionsTooBig) {
		t.Errorf("err = %v, the 16 MP boundary must pass the pixel check", err)
	}
}

func TestProcessContentThumbnail_RejectsOversizedInput(t *testing.T) {
	t.Parallel()

	raw := bytes.Repeat([]byte{0x00}, MaxInputBytes+1)
	_, err := ProcessContentThumbnail(t.TempDir(), validID, bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected rejection for oversized input")
	}
	if !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("err = %v, want ErrInputTooLarge", err)
	}
}

// TestProcessContentThumbnail_StripsEXIF splices an APP1 Exif segment into a
// valid JPEG and verifies the processed output never carries it — the
// strip-EXIF step is satisfied by decode+re-encode.
func TestProcessContentThumbnail_StripsEXIF(t *testing.T) {
	t.Parallel()

	encoded := encodeJPEG(t, 64, 64)
	payload := append([]byte("Exif\x00\x00"), []byte("MM\x00*marker")...)
	var app1 []byte
	app1 = append(app1, 0xff, 0xe1)
	app1 = binary.BigEndian.AppendUint16(app1, uint16(2+len(payload)))
	app1 = append(app1, payload...)

	// SOI is encoded[:2]; splice APP1 right after it.
	withExif := append([]byte{}, encoded[:2]...)
	withExif = append(withExif, app1...)
	withExif = append(withExif, encoded[2:]...)

	dir := t.TempDir()
	rel := process(t, dir, validID, withExif)

	out, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if bytes.Contains(out, []byte("Exif")) {
		t.Error("output contains EXIF bytes — metadata must be stripped")
	}
}

// TestProcessContentThumbnail_WriteErrorHidesFilesystemPath pins the log
// hygiene of a write failure: the sentinel stays in the chain, and the
// path-bearing *os.PathError text never reaches a log record
// (docs/patterns/go/logging.md rule 8).
func TestProcessContentThumbnail_WriteErrorHidesFilesystemPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// A regular file where the media directory must be created: MkdirAll
	// fails with a path-bearing *os.PathError.
	if err := os.WriteFile(filepath.Join(dir, "media"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	_, err := ProcessContentThumbnail(dir, validID, bytes.NewReader(encodeJPEG(t, 8, 8)))
	if !errors.Is(err, ErrWrite) {
		t.Fatalf("ProcessContentThumbnail(blocked media path) err = %v, want ErrWrite", err)
	}
	if strings.Contains(err.Error(), dir) {
		t.Errorf("ProcessContentThumbnail(blocked media path) err = %v, must not carry the data directory", err)
	}
}

func TestRelativePathLayout(t *testing.T) {
	t.Parallel()

	got := RelativePath(validID, ExtJPG)
	want := filepath.Join("media", "images", validID[:2], validID+".jpg")
	if got != want {
		t.Errorf("RelativePath = %q, want %q", got, want)
	}
}

func TestServedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stored string
		want   string
	}{
		{"storage subdirectory dropped", "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
			"media/images/abcdef0123456789abcdef0123456789.jpg"},
		{"without the subdirectory unchanged", "media/images/cdef0123456789abcdef0123456789.jpg",
			"media/images/cdef0123456789abcdef0123456789.jpg"},
		{"short four-segment value still mapped", "media/images/ab/cdef.jpg", "media/images/cdef.jpg"},
		{"three-segment value unchanged", "media/images/cdef.jpg", "media/images/cdef.jpg"},
		{"non-media path unchanged", "not-a-media-path", "not-a-media-path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ServedPath(tt.stored); got != tt.want {
				t.Errorf("ServedPath(%q) = %q, want %q", tt.stored, got, tt.want)
			}
		})
	}
}

// TestParseStorageReference pins the stored-reference grammar: the value the
// content write bodies accept and the sweep matches. The 2-hex subdirectory
// must be the id's first two characters — the layout's own derivation — so a
// reference can never address a different file than the one the sweep and
// the delete guard resolve.
func TestParseStorageReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
		ok   bool
	}{
		{"canonical jpg", "media/images/ab/" + validID + ".jpg", true},
		{"canonical png", "media/images/ab/" + validID + ".png", true},
		{"mismatched subdirectory", "media/images/cd/" + validID + ".jpg", false},
		{"non-hex subdirectory", "media/images/zz/" + validID + ".jpg", false},
		{"uppercase subdirectory", "media/images/AB/" + validID + ".jpg", false},
		{"dot-dot subdirectory", "media/images/../" + validID + ".jpg", false},
		{"jpeg is never stored", "media/images/ab/" + validID + ".jpeg", false},
		{"short id", "media/images/ab/" + validID[:31] + ".jpg", false},
		{"uppercase id", "media/images/ab/" + strings.ToUpper(validID) + ".jpg", false},
		{"missing subdirectory", "media/images/" + validID + ".jpg", false},
		{"nested deeper", "media/images/ab/ab/" + validID + ".jpg", false},
		{"empty extension", "media/images/ab/" + validID + ".", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id, ext, ok := ParseStorageReference(tt.ref)
			if ok != tt.ok {
				t.Fatalf("ParseStorageReference(%q) ok = %v, want %v", tt.ref, ok, tt.ok)
			}
			if !tt.ok {
				return
			}
			if id != validID {
				t.Errorf("ParseStorageReference(%q) id = %q, want %q", tt.ref, id, validID)
			}
			if ext != ExtJPG && ext != ExtPNG {
				t.Errorf("ParseStorageReference(%q) ext = %q, want jpg or png", tt.ref, ext)
			}
		})
	}
}

func processAvatar(t *testing.T, dataDir, id string, raw []byte) string {
	t.Helper()
	rel, err := ProcessAvatar(dataDir, id, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ProcessAvatar: %v", err)
	}
	return rel
}

// decodeOutput reads a processed file and returns its decoded config.
func decodeOutput(t *testing.T, dir, rel string) image.Config {
	t.Helper()
	out, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if format != "png" {
		t.Errorf("output format = %q, want png (avatars are PNG)", format)
	}
	return cfg
}

// TestProcessAvatar_WideSourceCenterCrops pins the geometry: a wide source
// crops to the largest centered square (height-tall), then scales to
// exactly 200×200 — always a PNG.
func TestProcessAvatar_WideSourceCenterCrops(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rel := processAvatar(t, dir, validID, encodeJPEG(t, 800, 400))

	if got, want := rel, RelativePath(validID, ExtPNG); got != want {
		t.Errorf("rel = %q, want %q (avatars are always PNG)", got, want)
	}
	cfg := decodeOutput(t, dir, rel)
	if cfg.Width != 200 || cfg.Height != 200 {
		t.Errorf("output dims = %dx%d, want exactly 200x200", cfg.Width, cfg.Height)
	}
}

// TestProcessAvatar_TallSourceCenterCrops mirrors the wide case for a tall
// source (crop to width-wide square).
func TestProcessAvatar_TallSourceCenterCrops(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 120, 300))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}

	dir := t.TempDir()
	rel := processAvatar(t, dir, validID, buf.Bytes())

	cfg := decodeOutput(t, dir, rel)
	if cfg.Width != 200 || cfg.Height != 200 {
		t.Errorf("output dims = %dx%d, want exactly 200x200", cfg.Width, cfg.Height)
	}
}

// TestProcessAvatar_SmallSourceUpscales pins the upscale contract: the
// stored avatar is always exactly 200×200 — small sources grow, unlike
// content thumbnails (which never upscale).
func TestProcessAvatar_SmallSourceUpscales(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rel := processAvatar(t, dir, validID, encodeJPEG(t, 50, 50))

	cfg := decodeOutput(t, dir, rel)
	if cfg.Width != 200 || cfg.Height != 200 {
		t.Errorf("output dims = %dx%d, want upscaled 200x200", cfg.Width, cfg.Height)
	}
}

// TestProcessAvatar_PreservesTransparency pins the alpha contract: a
// translucent PNG stays translucent after crop + scale (PNG output with
// the alpha channel intact — draw.Src on the crop, not a flattening draw).
func TestProcessAvatar_PreservesTransparency(t *testing.T) {
	t.Parallel()

	img := image.NewNRGBA(image.Rect(0, 0, 300, 150))
	for y := range 150 {
		for x := range 300 {
			img.Set(x, y, color.NRGBA{R: uint8(x), A: 128}) // half-transparent everywhere
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}

	dir := t.TempDir()
	rel := processAvatar(t, dir, validID, buf.Bytes())

	out, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if _, _, _, a := decoded.At(100, 100).RGBA(); a == 0xffff {
		t.Error("center pixel is fully opaque, want preserved translucency")
	}
}

// TestProcessAvatar_RejectsOversizedInput pins the shared input bound on
// the avatar pipeline (10 MB cap — the same readBounded every processor
// uses).
func TestProcessAvatar_RejectsOversizedInput(t *testing.T) {
	t.Parallel()

	raw := bytes.Repeat([]byte{0x00}, MaxInputBytes+1)
	_, err := ProcessAvatar(t.TempDir(), validID, bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected rejection for oversized input")
	}
	if !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("err = %v, want ErrInputTooLarge", err)
	}
}

// TestProcessAvatar_RejectsHugeHeader pins the shared header bounds on the
// avatar pipeline (the decodeBounded helper serves both processors).
func TestProcessAvatar_RejectsHugeHeader(t *testing.T) {
	t.Parallel()

	raw := pngHeaderWithDimensions(t, 50000, 50000)
	_, err := ProcessAvatar(t.TempDir(), validID, bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected rejection for 50000x50000 header")
	}
	if !errors.Is(err, ErrDimensionsTooBig) {
		t.Errorf("err = %v, want ErrDimensionsTooBig", err)
	}
}

// TestProcessAvatar_WebPDecodes pins the legacy-format contract: WebP
// sources decode (WebP input accepted) and upscale to 200×200.
func TestProcessAvatar_WebPDecodes(t *testing.T) {
	t.Parallel()

	// The same minimal lossless VP8L 1×1 fixture as the thumbnail tests.
	raw, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	dir := t.TempDir()
	rel := processAvatar(t, dir, validID, raw)

	cfg := decodeOutput(t, dir, rel)
	if cfg.Width != 200 || cfg.Height != 200 {
		t.Errorf("output dims = %dx%d, want upscaled 200x200", cfg.Width, cfg.Height)
	}
}

// failingReader always fails — an infrastructure read error, never an image
// decode fact.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// TestProcessContentThumbnail_ReadFailureIsNotADecodeFailure pins the
// classification: a failed source read must not masquerade as an undecodable
// body (the upload boundary answers its masked 500 for it).
func TestProcessContentThumbnail_ReadFailureIsNotADecodeFailure(t *testing.T) {
	t.Parallel()

	readErr := errors.New("transport reset")
	_, err := ProcessContentThumbnail(t.TempDir(), validID, failingReader{err: readErr})
	if err == nil {
		t.Fatal("expected the read failure to surface")
	}
	if errors.Is(err, ErrDecode) || errors.Is(err, ErrDecodeHeader) {
		t.Errorf("err = %v, a source-read failure must not classify as a decode error", err)
	}
	if errors.Is(err, readErr) {
		t.Errorf("err = %v, the reader cause must be flattened, not wrapped", err)
	}
}

// TestProcessContentThumbnail_PerAxisBoundEdges pins both sides of the
// per-axis bound: 8192 passes the header checks, 8193 does not.
func TestProcessContentThumbnail_PerAxisBoundEdges(t *testing.T) {
	t.Parallel()

	raw := pngHeaderWithDimensions(t, 8192, 1)
	_, err := ProcessContentThumbnail(t.TempDir(), validID, bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected a decode failure for a header-only fixture")
	}
	if errors.Is(err, ErrDimensionsTooBig) || errors.Is(err, ErrPixelsTooBig) {
		t.Errorf("err = %v, the 8192 edge must pass both header checks", err)
	}

	raw = pngHeaderWithDimensions(t, 8193, 1)
	_, err = ProcessContentThumbnail(t.TempDir(), validID, bytes.NewReader(raw))
	if !errors.Is(err, ErrDimensionsTooBig) {
		t.Errorf("8193-wide err = %v, want ErrDimensionsTooBig", err)
	}
}

// TestProcessContentThumbnail_TotalPixelRejectEdge pins a constructible
// product over the total-pixel bound with both axes inside the per-axis
// limit: 4096×4097 = 2^24+4096. An exact 2^24+1 product has no factorization
// with both axes within 8192, and 4096×4096 (the accepted edge) is pinned
// beside it.
func TestProcessContentThumbnail_TotalPixelRejectEdge(t *testing.T) {
	t.Parallel()

	raw := pngHeaderWithDimensions(t, 4097, 4096)
	_, err := ProcessContentThumbnail(t.TempDir(), validID, bytes.NewReader(raw))
	if !errors.Is(err, ErrPixelsTooBig) {
		t.Errorf("4097×4096 err = %v, want ErrPixelsTooBig", err)
	}
}

// TestProcessContentThumbnail_AcceptsExactlyMaxInput pins the accepting side
// of the input bound: exactly MaxInputBytes is within it and then fails the
// header check — which is what proves it was not classified as too large.
func TestProcessContentThumbnail_AcceptsExactlyMaxInput(t *testing.T) {
	t.Parallel()

	raw := bytes.Repeat([]byte{0x00}, MaxInputBytes)
	_, err := ProcessContentThumbnail(t.TempDir(), validID, bytes.NewReader(raw))
	if err == nil {
		t.Fatal("expected a header failure for zero bytes")
	}
	if errors.Is(err, ErrInputTooLarge) {
		t.Errorf("err = %v, exactly MaxInputBytes must pass the size bound", err)
	}
}
