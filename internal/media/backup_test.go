package media

import (
	"archive/tar"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedMedia writes fake processed images under the storage layout and
// returns a map of storage-relative path → content.
func seedMedia(t *testing.T, dataDir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dataDir, "media", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func buildTestTar(t *testing.T, dataDir, destDir, tarName string) string {
	t.Helper()
	sha, err := BuildMediaTar(dataDir, destDir, tarName)
	if err != nil {
		t.Fatalf("BuildMediaTar: %v", err)
	}
	if len(sha) != 64 {
		t.Fatalf("sha256 hex = %q, want 64 chars", sha)
	}
	if _, err := hex.DecodeString(sha); err != nil {
		t.Fatalf("sha256 not hex: %v", err)
	}
	return sha
}

func TestBuildMediaTar_RoundTrip(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	files := map[string]string{
		"images/ab/" + validID + ".jpg":                  "jpeg-bytes",
		"images/cd/deadbeefdeadbeefdeadbeefdeadbeef.png": "png-bytes",
	}
	seedMedia(t, src, files)

	dest := t.TempDir()
	sha := buildTestTar(t, src, dest, "media.tar")

	// Extract into a fresh data directory and verify the tree matches.
	fresh := t.TempDir()
	if err := ExtractMediaTar(fresh, filepath.Join(dest, "media.tar")); err != nil {
		t.Fatalf("ExtractMediaTar: %v", err)
	}
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(fresh, "media", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read extracted %s: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("extracted %s = %q, want %q", rel, got, want)
		}
	}

	// The manifest-side verification accepts the same tar.
	if err := VerifyMediaTar(dest, MediaManifest{MediaTarFileName: "media.tar", MediaTarSHA256: sha}); err != nil {
		t.Errorf("VerifyMediaTar on the built tar: %v", err)
	}
}

func TestBuildMediaTar_MissingMediaDirIsEmpty(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir() // no media/ directory at all
	dest := t.TempDir()
	if _, err := BuildMediaTar(dataDir, dest, "media.tar"); err != nil {
		t.Fatalf("BuildMediaTar without media dir: %v", err)
	}

	// A valid empty tar: extraction succeeds and creates nothing.
	fresh := t.TempDir()
	if err := ExtractMediaTar(fresh, filepath.Join(dest, "media.tar")); err != nil {
		t.Fatalf("ExtractMediaTar empty tar: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(fresh, "media")); err == nil && len(entries) != 0 {
		t.Errorf("extracted %d entries from an empty tar, want 0", len(entries))
	}
}

func TestVerifyMediaTar_Rejections(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	seedMedia(t, dataDir, map[string]string{"images/ab/" + validID + ".jpg": "bytes"})
	dest := t.TempDir()
	sha := buildTestTar(t, dataDir, dest, "media.tar")

	cases := []struct {
		name     string
		manifest MediaManifest
	}{
		{"missing tarball", MediaManifest{MediaTarFileName: "nope.tar", MediaTarSHA256: sha}},
		{"wrong hash", MediaManifest{MediaTarFileName: "media.tar", MediaTarSHA256: strings.Repeat("0", 64)}},
		{"no filename", MediaManifest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := VerifyMediaTar(dest, tc.manifest); err == nil {
				t.Error("VerifyMediaTar: got nil, want rejection")
			}
		})
	}

	// A byte-flipped tar fails its recorded hash.
	corrupt := filepath.Join(dest, "corrupt.tar")
	raw, err := os.ReadFile(filepath.Join(dest, "media.tar"))
	if err != nil {
		t.Fatalf("read tar: %v", err)
	}
	raw[len(raw)/2] ^= 0xFF
	if err := os.WriteFile(corrupt, raw, 0o600); err != nil {
		t.Fatalf("write corrupt tar: %v", err)
	}
	if err := VerifyMediaTar(dest, MediaManifest{MediaTarFileName: "corrupt.tar", MediaTarSHA256: sha}); err == nil {
		t.Error("corrupt tar: expected checksum mismatch")
	}
}

// rawTarEntry hand-crafts one tar header block + content + padding, so
// malicious entries reach the extractor exactly as an attacker's tar would,
// independent of tar.Writer's own validation. declaredSize sets the header's
// size field; content is what follows (may be shorter — the extractor checks
// the declared size first, which is what the oversize test needs).
func rawTarEntry(name string, typeflag byte, declaredSize int, content []byte) []byte {
	var hdr [512]byte
	copy(hdr[0:100], name)
	copy(hdr[100:108], "0000644\x00")
	copy(hdr[108:116], "0000000\x00")
	copy(hdr[116:124], "0000000\x00")
	copy(hdr[124:136], fmt.Sprintf("%011o\x00", declaredSize))
	copy(hdr[136:148], "00000000000\x00")
	copy(hdr[148:156], "        ")
	hdr[156] = typeflag
	copy(hdr[257:263], "ustar\x00")
	copy(hdr[263:265], "00")
	var sum int
	for _, b := range hdr {
		sum += int(b)
	}
	copy(hdr[148:156], fmt.Sprintf("%06o\x00 ", sum))

	out := append([]byte{}, hdr[:]...)
	out = append(out, content...)
	pad := (512 - len(content)%512) % 512
	out = append(out, make([]byte, pad)...)
	return out
}

func TestExtractMediaTar_RejectsUnsafeEntries(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()

	cases := []struct {
		name string
		raw  []byte
	}{
		{"absolute path", rawTarEntry("/etc/passwd", tar.TypeReg, 5, []byte("owned"))},
		{"parent traversal", rawTarEntry("../evil.txt", tar.TypeReg, 5, []byte("owned"))},
		{"dot component", rawTarEntry("images/./evil.txt", tar.TypeReg, 5, []byte("owned"))},
		{"symlink", rawTarEntry("images/ab/link", tar.TypeSymlink, 13, []byte("images/ab/x"))},
		// Declared size over the limit — rejected before any content is read.
		{"oversized", rawTarEntry("images/ab/huge.jpg", tar.TypeReg, maxExtractFileBytes+1, []byte("x"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tarPath := filepath.Join(t.TempDir(), "evil.tar")
			if err := os.WriteFile(tarPath, tc.raw, 0o600); err != nil {
				t.Fatalf("write tar: %v", err)
			}
			if err := ExtractMediaTar(dataDir, tarPath); err == nil {
				t.Error("ExtractMediaTar: got nil, want rejection")
			}
		})
	}
	// Nothing from any malicious tar may have been written.
	entries, _ := os.ReadDir(filepath.Join(dataDir, "media"))
	if len(entries) != 0 {
		t.Errorf("media dir has %d entries after malicious extracts, want 0", len(entries))
	}
}

func TestExtractMediaTar_LeavesUnrelatedFiles(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	seedMedia(t, src, map[string]string{"images/ab/" + validID + ".jpg": "bytes"})
	dest := t.TempDir()
	buildTestTar(t, src, dest, "media.tar")

	// The live media dir has a file the tar does not contain (added after
	// the backup point). Extraction must leave it alone — it becomes
	// unreferenced and is the sweep's job.
	live := t.TempDir()
	extra := filepath.Join(live, "media", "images", "ff", "postbackup.jpg")
	if err := os.MkdirAll(filepath.Dir(extra), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(extra, []byte("post-backup"), 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}

	if err := ExtractMediaTar(live, filepath.Join(dest, "media.tar")); err != nil {
		t.Fatalf("ExtractMediaTar: %v", err)
	}
	if got, err := os.ReadFile(extra); err != nil || string(got) != "post-backup" {
		t.Errorf("extra file = %q, %v; want it untouched", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(live, "media", "images", "ab", validID+".jpg")); err != nil || string(got) != "bytes" {
		t.Errorf("restored file = %q, %v; want the backup content", got, err)
	}
}

func TestExtractMediaTar_IsIdempotent(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	seedMedia(t, src, map[string]string{"images/ab/" + validID + ".jpg": "bytes"})
	dest := t.TempDir()
	buildTestTar(t, src, dest, "media.tar")

	live := t.TempDir()
	tarPath := filepath.Join(dest, "media.tar")
	for i := range 2 {
		if err := ExtractMediaTar(live, tarPath); err != nil {
			t.Fatalf("ExtractMediaTar run %d: %v", i+1, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(live, "media", "images", "ab", validID+".jpg"))
	if err != nil || string(got) != "bytes" {
		t.Errorf("restored file after two extractions = %q, %v; want the backup content", got, err)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sick-fansubs.db.media-manifest.json")
	want := MediaManifest{
		BackupTimestampMS: 1725058800000,
		DBFileName:        "sick-fansubs.db",
		MediaTarFileName:  "sick-fansubs.media.tar",
		MediaTarSHA256:    strings.Repeat("a", 64),
	}
	if err := WriteMediaManifest(path, want); err != nil {
		t.Fatalf("WriteMediaManifest: %v", err)
	}
	got, err := ReadMediaManifest(path)
	if err != nil {
		t.Fatalf("ReadMediaManifest: %v", err)
	}
	if got != want {
		t.Errorf("manifest = %+v, want %+v", got, want)
	}
}

func TestReadMediaManifest_Strict(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "m.json")
	// Two JSON values — exactly-one-value decoding must reject.
	if err := os.WriteFile(path, []byte(`{"backupTimestampMs":1,"dbFileName":"d","mediaTarFileName":"t","mediaTarSHA256":"`+strings.Repeat("a", 64)+`"}{"again":true}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadMediaManifest(path); err == nil {
		t.Error("trailing JSON value: expected rejection")
	}

	// Unknown field — strict allowlist must reject.
	if err := os.WriteFile(path, []byte(`{"backupTimestampMs":1,"dbFileName":"d","mediaTarFileName":"t","mediaTarSHA256":"`+strings.Repeat("a", 64)+`","extra":1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadMediaManifest(path); err == nil {
		t.Error("unknown field: expected rejection")
	}
}

func TestManifestPathFor(t *testing.T) {
	t.Parallel()
	got := ManifestPathFor(filepath.Join("backups", "2026-08-31", "sick-fansubs.db"))
	want := filepath.Join("backups", "2026-08-31", "sick-fansubs.db.media-manifest.json")
	if got != want {
		t.Errorf("ManifestPathFor = %q, want %q", got, want)
	}
}

// A tar produced by the real archive/tar writer must still extract (the
// round-trip guard against over-strict entry handling).
func TestExtractMediaTar_AcceptsWriterOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "images/ab/x.jpg", Mode: 0o644, Size: 4, Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("header: %v", err)
	}
	if _, err := tw.Write([]byte("data")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	tarPath := filepath.Join(t.TempDir(), "w.tar")
	if err := os.WriteFile(tarPath, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write tar: %v", err)
	}

	dataDir := t.TempDir()
	if err := ExtractMediaTar(dataDir, tarPath); err != nil {
		t.Fatalf("ExtractMediaTar on archive/tar output: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dataDir, "media", "images", "ab", "x.jpg"))
	if err != nil || string(got) != "data" {
		t.Errorf("extracted = %q, %v", got, err)
	}
}

// TestValidateManifest pins the sidecar field contract: a valid manifest
// passes, and each hostile/corrupt shape (traversal filenames, separators,
// bad hash length/charset, non-positive timestamp, empty fields) is rejected
// so the pre-activation gate can never steer extraction outside the unit.
func TestValidateManifest(t *testing.T) {
	t.Parallel()

	valid := MediaManifest{
		BackupTimestampMS: 1725058800000,
		DBFileName:        "sick-fansubs.db",
		MediaTarFileName:  "sick-fansubs.media.tar",
		MediaTarSHA256:    strings.Repeat("a", 64),
	}
	if err := ValidateManifest(valid); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	cases := []struct {
		name   string
		damage func(m *MediaManifest)
	}{
		{"zero timestamp", func(m *MediaManifest) { m.BackupTimestampMS = 0 }},
		{"empty db filename", func(m *MediaManifest) { m.DBFileName = "" }},
		{"db filename with separator", func(m *MediaManifest) { m.DBFileName = "sub/sick.db" }},
		{"db filename traversal", func(m *MediaManifest) { m.DBFileName = "../sick.db" }},
		{"tar filename traversal", func(m *MediaManifest) { m.MediaTarFileName = "../evil.tar" }},
		{"tar filename absolute", func(m *MediaManifest) { m.MediaTarFileName = "/abs/evil.tar" }},
		{"tar filename backslash", func(m *MediaManifest) { m.MediaTarFileName = `..\evil.tar` }},
		{"short hash", func(m *MediaManifest) { m.MediaTarSHA256 = strings.Repeat("a", 63) }},
		{"uppercase hash", func(m *MediaManifest) { m.MediaTarSHA256 = strings.Repeat("A", 64) }},
		{"non-hex hash", func(m *MediaManifest) { m.MediaTarSHA256 = strings.Repeat("g", 64) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := valid
			tc.damage(&m)
			if err := ValidateManifest(m); err == nil {
				t.Error("ValidateManifest: got nil, want rejection")
			}
		})
	}
}

// TestReadMediaManifest_SizeBound pins both sides of the manifest size
// bound: exactly maxManifestBytes (trailing JSON whitespace is legal) is
// accepted, one byte more is refused by the explicit length check rather
// than silently truncated.
func TestReadMediaManifest_SizeBound(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "m.json")
	valid := `{"backupTimestampMs":1,"dbFileName":"d","mediaTarFileName":"t","mediaTarSHA256":"` + strings.Repeat("a", 64) + `"}`

	atBound := valid + strings.Repeat(" ", maxManifestBytes-len(valid))
	if err := os.WriteFile(path, []byte(atBound), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadMediaManifest(path); err != nil {
		t.Errorf("exactly-at-bound manifest: %v", err)
	}

	overBound := valid + strings.Repeat(" ", maxManifestBytes-len(valid)+1)
	if err := os.WriteFile(path, []byte(overBound), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadMediaManifest(path); err == nil {
		t.Error("over-bound manifest: expected rejection")
	}
}
