package migration

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"sick-fansubs/internal/media"
)

// pngHeaderBytes crafts a structurally valid PNG header claiming arbitrary
// dimensions — the smallest file that trips the media pipeline's header
// pre-checks.
func pngHeaderBytes(t *testing.T, w, h uint32) []byte {
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
func insertBlogRow(t *testing.T, db *sql.DB, id, thumbnailURL string) {
	t.Helper()
	const q = `INSERT INTO blog_posts
		(id, title, subtitle, description, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'T', '', '', ?, 'published', 1000, 500, 500)`
	if _, err := db.Exec(q, id, thumbnailURL); err != nil {
		t.Fatalf("insert blog row %s: %v", id, err)
	}
}

func insertProjectRow(t *testing.T, db *sql.DB, id, slug, thumbnailURL string) {
	t.Helper()
	const q = `INSERT INTO projects
		(id, title, description, slug, thumbnail_url, status,
		 published_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, 'T', '', ?, ?, 'published', 1000, 500, 500)`
	if _, err := db.Exec(q, id, slug, thumbnailURL); err != nil {
		t.Fatalf("insert project row %s: %v", id, err)
	}
}

func projectThumbnailOf(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT thumbnail_url FROM projects WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatalf("read projects.thumbnail_url of %s: %v", id, err)
	}
	return got
}

func thumbnailOf(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT thumbnail_url FROM blog_posts WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatalf("read thumbnail_url of %s: %v", id, err)
	}
	return got
}

func TestLegacyMediaKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		// Clean legacy MinIO form keeps the object key.
		{"https://sickfansubs.com/media/images/1778-a.webp", "1778-a.webp", false},
		{"http://cdn.example/media/images/k.jpg", "k.jpg", false},
		// Every other absolute URL derives a stable key from the exact URL
		// string (external hosts — Discord CDN,
		// postimg — are the majority of the production data).
		{"https://sickfansubs.com/media/images/deep/key.webp", "x-ecbaa49c76492ebd96b75dc7d42be582.webp", false}, // nested key
		{"https://sickfansubs.com/media/other/k.webp", "x-9aecccc0643bb94a332c90303b4a3bd1.webp", false},         // wrong prefix
		{"https://sickfansubs.com/media/images/", "x-20e87bd1091daf06b27eb6cbd9b2e10b", false},                   // empty key
		{"https://sickfansubs.com/media/images/noext", "x-bc5d4e9c1627db9d59cfaaa8f89917f8", false},              // no extension
		{"https://sickfansubs.com/media/images/.hidden", "x-c78e4e4c4b1920f0e016d8ceae1eaa46", false},            // dotfile
		{"https://sickfansubs.com/media/images/k.webp?x=1", "x-e5050cd1a8904613b331a82eab893a9b.webp", false},    // query on path key
		{"https://i.postimg.cc/N00vRLSn/image.png", "x-bc31192f1fdedb04b0d588ffe4a2eece.png", false},
		{"https://cdn.discordapp.com/attachments/660525275930558504/1000779217954410546/op1026.png", "x-9b0b8986d3332599072382b184c8b0a1.png", false},
		{"https://cdn.discordapp.com/attachments/x/y/z.png?ex=1&is=2&hm=3", "x-2e74f94a926057cb6ffee571ed896126.png", false},
		{"https://i.postimg.cc/abc", "x-f32da472e0d3710b0666b0841ebdbb8c", false},                // no extension
		{"https://i.postimg.cc/x/IMG.PNG", "x-73709bedd4d7c570d8742edf32b3a554.png", false},      // uppercase ext lowercased
		{"https://sickfansubs.com/media/images/k.", "x-31a019b15eeaf16f39d6ed8d6c0192c1", false}, // trailing dot → hash branch, no ext
		{"https://i.postimg.cc/x/k.webp#frag", "x-ba176223a4f2100cb120ae613e95b99f.webp", false}, // fragment hashed with the URL
		{"ftp://host/media/images/k.jpg", "k.jpg", false},                                        // scheme-independent key; fetch reports unsupportedScheme
		// Non-absolute values are the only error (badReferenceShape in both
		// commands) — derivation is deterministic, never a guess.
		{"relative/media/images/k.webp", "", true}, // not absolute
		{"not-a-url", "", true},
		{"mailto:user@example.com", "", true}, // scheme-only, no host
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			got, err := LegacyMediaKey(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Errorf("LegacyMediaKey(%q) = %q, want error", tt.raw, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("LegacyMediaKey(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
			}
		})
	}
}

func TestMediaIDForBytes(t *testing.T) {
	t.Parallel()

	same := []byte("identical source bytes")
	other := []byte("different source bytes")

	a := MediaIDForBytes(same)
	b := MediaIDForBytes(same)
	c := MediaIDForBytes(other)

	if a != b {
		t.Errorf("same bytes produced different IDs: %q vs %q", a, b)
	}
	if a == c {
		t.Error("different bytes produced the same ID")
	}
	if len(a) != 32 {
		t.Errorf("ID length = %d, want 32", len(a))
	}
	for _, ch := range a {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			t.Errorf("ID %q contains non-hex character %q", a, ch)
		}
	}
}

// TestImportMedia_Projects: project thumbnail references are
// scanned, processed once per distinct URL, and rewritten in the projects
// table — the same pipeline as blog rows, with per-table scan counts.
func TestImportMedia_Projects(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	const (
		urlJPG = "https://sickfansubs.com/media/images/1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
		urlPNG = "https://sickfansubs.com/media/images/1778832678231-6d1c4453-5d43-4b6b-9f89-146a77655f1f.png"
	)
	insertProjectRow(t, db, "pr1", "one", urlJPG)
	insertProjectRow(t, db, "pr2", "two", urlPNG)

	report, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("ImportMedia: %v", err)
	}
	if report.Source.ProjectRowsScanned != 2 || report.Source.BlogRowsScanned != 0 || report.Source.DistinctURLs != 2 {
		t.Errorf("source = %+v, want 2 project rows / 0 blog rows / 2 URLs", report.Source)
	}
	if report.Outcome.Processed != 2 || report.Outcome.Failed != 0 || report.Outcome.RowsRewritten != 2 {
		t.Errorf("outcome = %+v, want 2 processed / 0 failed / 2 rewritten", report.Outcome)
	}

	for _, rowID := range []string{"pr1", "pr2"} {
		if got := projectThumbnailOf(t, db, rowID); !hasPrefixMedia(got) {
			t.Errorf("%s thumbnail_url = %q, want relative media path", rowID, got)
		}
	}
}

// TestImportMedia_SharedURLAcrossTables: one legacy URL referenced by BOTH
// a blog post and a project is fetched/processed once and rewrites every
// row in its own table.
func TestImportMedia_SharedURLAcrossTables(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	const url = "https://sickfansubs.com/media/images/1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
	insertBlogRow(t, db, "p1", url)
	insertProjectRow(t, db, "pr1", "shared", url)

	report, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("ImportMedia: %v", err)
	}
	if report.Source.DistinctURLs != 1 || report.Source.BlogRowsScanned != 1 || report.Source.ProjectRowsScanned != 1 {
		t.Errorf("source = %+v, want 1 URL / 1+1 rows", report.Source)
	}
	if report.Outcome.Processed != 1 || report.Outcome.RowsRewritten != 2 {
		t.Errorf("outcome = %+v, want 1 processed / 2 rewritten", report.Outcome)
	}

	if got := thumbnailOf(t, db, "p1"); !hasPrefixMedia(got) {
		t.Errorf("p1 thumbnail_url = %q, want relative media path", got)
	}
	if got := projectThumbnailOf(t, db, "pr1"); !hasPrefixMedia(got) {
		t.Errorf("pr1 thumbnail_url = %q, want relative media path", got)
	}
}

func TestImportMedia_HappyPath(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	const (
		urlJPG = "https://sickfansubs.com/media/images/1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
		urlPNG = "https://sickfansubs.com/media/images/1778832678231-6d1c4453-5d43-4b6b-9f89-146a77655f1f.png"
	)
	// Two rows share the jpeg URL; one row uses the png URL.
	insertBlogRow(t, db, "p1", urlJPG)
	insertBlogRow(t, db, "p2", urlJPG)
	insertBlogRow(t, db, "p3", urlPNG)

	report, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("ImportMedia: %v", err)
	}

	if report.Source.BlogRowsScanned != 3 || report.Source.DistinctURLs != 2 {
		t.Errorf("source = %+v, want 3 rows / 2 URLs", report.Source)
	}
	if report.Outcome.Processed != 2 || report.Outcome.Failed != 0 || report.Outcome.RowsRewritten != 3 {
		t.Errorf("outcome = %+v, want 2 processed / 0 failed / 3 rewritten", report.Outcome)
	}

	const keyJPG = "1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
	const keyPNG = "1778832678231-6d1c4453-5d43-4b6b-9f89-146a77655f1f.png"
	rawJPG, err := os.ReadFile(filepath.Join(sourceDir, keyJPG))
	if err != nil {
		t.Fatalf("read jpg fixture: %v", err)
	}
	rawPNG, err := os.ReadFile(filepath.Join(sourceDir, keyPNG))
	if err != nil {
		t.Fatalf("read png fixture: %v", err)
	}
	id := MediaIDForBytes(rawJPG)
	wantRel := media.RelativePath(id, media.ExtJPG)
	for _, rowID := range []string{"p1", "p2"} {
		if got := thumbnailOf(t, db, rowID); got != wantRel {
			t.Errorf("%s thumbnail_url = %q, want %q", rowID, got, wantRel)
		}
	}
	idPNG := MediaIDForBytes(rawPNG)
	if got := thumbnailOf(t, db, "p3"); got != media.RelativePath(idPNG, media.ExtPNG) {
		t.Errorf("p3 thumbnail_url = %q, want PNG relative path (alpha source)", got)
	}

	// Processed files exist at the derived paths.
	for _, path := range []string{
		media.ImagePath(dataDir, id, media.ExtJPG),
		media.ImagePath(dataDir, idPNG, media.ExtPNG),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("processed file %s missing: %v", path, err)
		}
	}
}

func TestImportMedia_IdempotentRerun(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	const url = "https://sickfansubs.com/media/images/1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
	insertBlogRow(t, db, "p1", url)

	first, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("first ImportMedia: %v", err)
	}
	second, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("second ImportMedia: %v", err)
	}

	// The second run scans nothing (all references are relative now) but the
	// processed file remains; running against a re-imported DB reproduces
	// the same ID, so the file is simply overwritten with identical bytes.
	if second.Source.BlogRowsScanned != 0 {
		t.Errorf("second run scanned %d rows, want 0 (already rewritten)", second.Source.BlogRowsScanned)
	}
	if first.Outcome.RowsRewritten != 1 {
		t.Errorf("first run rewritten = %d, want 1", first.Outcome.RowsRewritten)
	}
}

func TestImportMedia_FailuresAreCountedNotFatal(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	insertBlogRow(t, db, "p1", "https://sickfansubs.com/media/images/9999-missing.jpg")                                       // legacy form, not in source dir
	insertBlogRow(t, db, "p2", "https://sickfansubs.com/not-media/k.jpg")                                                     // external form, not in source dir
	insertBlogRow(t, db, "p3", "relative/thumb.jpg")                                                                          // non-absolute → badReferenceShape
	insertBlogRow(t, db, "p4", "https://sickfansubs.com/media/images/1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg") // good

	report, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("ImportMedia: %v", err)
	}

	if report.Outcome.Failed != 3 || report.Outcome.Processed != 1 || report.Outcome.RowsRewritten != 1 {
		t.Errorf("outcome = %+v, want 3 failed / 1 processed / 1 rewritten", report.Outcome)
	}
	reasons := map[string]int{}
	for _, f := range report.Failures {
		reasons[f.Reason] = f.Count
	}
	if reasons["missingSourceFile"] != 2 || reasons["badReferenceShape"] != 1 {
		t.Errorf("failures = %+v, want 2× missingSourceFile + 1× badReferenceShape", report.Failures)
	}

	// Failed rows keep their legacy URL (never half-rewritten); the good row
	// was rewritten.
	if got := thumbnailOf(t, db, "p1"); got != "https://sickfansubs.com/media/images/9999-missing.jpg" {
		t.Errorf("p1 thumbnail_url = %q, want unchanged legacy URL", got)
	}
	if got := thumbnailOf(t, db, "p4"); !hasPrefixMedia(got) {
		t.Errorf("p4 thumbnail_url = %q, want relative media path", got)
	}
}

func hasPrefixMedia(s string) bool {
	return len(s) >= 6 && s[:6] == "media/"
}

// TestImportMedia_ExternalHostKey pins the external-host join for URLs that
// are not legacy MinIO keys: the row's Discord-CDN URL derives the same
// hashed key the fetch wrote, so the import finds and processes the source
// and rewrites both referencing rows to the target path.
func TestImportMedia_ExternalHostKey(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	const url = "https://cdn.discordapp.com/attachments/660525275930558504/1000779217954410546/op1026.png"
	insertBlogRow(t, db, "p1", url)
	insertProjectRow(t, db, "pr1", "ext", url)

	key, err := LegacyMediaKey(url)
	if err != nil {
		t.Fatalf("LegacyMediaKey: %v", err)
	}
	writeSource(t, sourceDir, key, jpegFixture(t, 320, 180))

	report, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("ImportMedia: %v", err)
	}
	if report.Outcome.Processed != 1 || report.Outcome.RowsRewritten != 2 || report.Outcome.Failed != 0 {
		t.Errorf("outcome = %+v, want 1 processed / 2 rewritten / 0 failed", report.Outcome)
	}
	if got := thumbnailOf(t, db, "p1"); !hasPrefixMedia(got) {
		t.Errorf("p1 thumbnail_url = %q, want relative media path", got)
	}
	if got := projectThumbnailOf(t, db, "pr1"); !hasPrefixMedia(got) {
		t.Errorf("pr1 thumbnail_url = %q, want relative media path", got)
	}
}

// TestImportMedia_ChangedBytesGetFreshID pins the immutable-cache invariant:
// when a source object's bytes change between runs (the
// cutover-time scenario — the bucket object changed since the rehearsal),
// the import must produce a NEW id/file instead of overwriting one that
// browsers may hold under the one-year immutable cache. The old file stays
// on disk (cleanup is a separate task).
func TestImportMedia_ChangedBytesGetFreshID(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	const key = "1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
	const url = "https://sickfansubs.com/media/images/" + key
	insertBlogRow(t, db, "p1", url)

	if _, err := ImportMedia(ctx, db, dataDir, sourceDir); err != nil {
		t.Fatalf("first ImportMedia: %v", err)
	}
	firstRel := thumbnailOf(t, db, "p1")

	// The bucket object changed; a fresh importdata run put the legacy URL
	// back. Both files must exist afterward: the new one serves, the old
	// one is orphaned but never overwritten.
	writeSource(t, sourceDir, key, jpegFixture(t, 320, 180))
	if _, err := db.Exec(`UPDATE blog_posts SET thumbnail_url = ? WHERE id = 'p1'`, url); err != nil {
		t.Fatalf("reset reference to legacy URL: %v", err)
	}

	report, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("second ImportMedia: %v", err)
	}
	if report.Outcome.Processed != 1 || report.Outcome.Failed != 0 || report.Outcome.RowsRewritten != 1 {
		t.Errorf("outcome = %+v, want 1 processed / 0 failed / 1 rewritten", report.Outcome)
	}

	secondRel := thumbnailOf(t, db, "p1")
	if secondRel == firstRel {
		t.Fatalf("changed bytes reused the old reference %q — the immutable-cache contract is violated", firstRel)
	}
	for _, rel := range []string{firstRel, secondRel} {
		if _, err := os.Stat(filepath.Join(dataDir, rel)); err != nil {
			t.Errorf("file %s missing after re-import: %v", rel, err)
		}
	}
}

// TestImportMedia_OversizedSourceFails: a source object beyond the input-size
// bound is rejected before hashing or decoding, counted, and the reference
// stays untouched.
func TestImportMedia_OversizedSourceFails(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	const key = "1780385418323-7e72ea10-72a9-48d7-ab85-47b7f1a05d9a.jpg"
	const url = "https://sickfansubs.com/media/images/" + key
	raw := bytes.Repeat([]byte{0xAB}, media.MaxInputBytes+1)
	writeSource(t, sourceDir, key, raw)
	insertBlogRow(t, db, "p1", url)

	report, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("ImportMedia: %v", err)
	}
	if report.Outcome.Failed != 1 || report.Outcome.Processed != 0 {
		t.Errorf("outcome = %+v, want 1 failed / 0 processed", report.Outcome)
	}
	if len(report.Failures) != 1 || report.Failures[0].Reason != "oversizedSource" {
		t.Errorf("failures = %+v, want oversizedSource", report.Failures)
	}
	if got := thumbnailOf(t, db, "p1"); got != url {
		t.Errorf("p1 thumbnail_url = %q, want untouched legacy URL", got)
	}
}

func TestImportMedia_PixelBombSourceFails(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	// 5000×5000 PNG header = 25 MP — over the total-pixel bound but under
	// the per-axis 8192 bound. The import counts the failure under the
	// pixelsTooLarge code instead of aborting (failures are counted, not
	// fatal).
	const key = "1780385418323-7e72ea10-72a9-48d7-ab85-47b7f1a05d9a.png"
	const url = "https://sickfansubs.com/media/images/" + key
	writeSource(t, sourceDir, key, pngHeaderBytes(t, 5000, 5000))
	insertBlogRow(t, db, "p1", url)

	report, err := ImportMedia(ctx, db, dataDir, sourceDir)
	if err != nil {
		t.Fatalf("ImportMedia: %v", err)
	}
	if report.Outcome.Failed != 1 || report.Outcome.Processed != 0 {
		t.Errorf("outcome = %+v, want 1 failed / 0 processed", report.Outcome)
	}
	if len(report.Failures) != 1 || report.Failures[0].Reason != "pixelsTooLarge" {
		t.Errorf("failures = %+v, want pixelsTooLarge", report.Failures)
	}
	if got := thumbnailOf(t, db, "p1"); got != url {
		t.Errorf("p1 thumbnail_url = %q, want untouched legacy URL", got)
	}
}

// TestMediaReports_FailuresMarshalAsEmptyArray pins the uniform report
// shape on the producer path: a clean run still carries failures as []
// (never null, never omitted) for both media-shaped reports.
func TestMediaReports_FailuresMarshalAsEmptyArray(t *testing.T) {
	t.Parallel()

	mediaReport, err := ImportMedia(t.Context(), openImportDB(t), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("ImportMedia: %v", err)
	}
	avatarReport, err := ImportAvatars(t.Context(), openImportDB(t), t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("ImportAvatars: %v", err)
	}

	cases := []struct {
		name   string
		report any
	}{
		{"media", mediaReport},
		{"avatar", avatarReport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := json.Marshal(tc.report)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var shape struct {
				Failures []json.RawMessage `json:"failures"`
			}
			if err := json.Unmarshal(out, &shape); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if shape.Failures == nil {
				t.Error("failures decoded as null or absent, want an empty array")
			}
		})
	}
}
