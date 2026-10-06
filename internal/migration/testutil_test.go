package migration

import (
	"bytes"
	"database/sql"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"sick-fansubs/internal/database"
)

// testDataDir returns an owner-only temporary directory for tests that open a
// database: database.Open refuses a data directory with group or other access,
// and t.TempDir can inherit wider bits from the environment.
func testDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod test data dir: %v", err)
	}
	return dir
}

// openImportDB creates a temporary migrated SQLite database (the same shape
// the import command uses: maintenance opener applies the embedded schema).
func openImportDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(database.Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("Apply: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// loadFixture reads the synthetic legacy records through the same reader
// the import command uses (rule 8: hand-built synthetic data, never
// production values).
func loadFixture(t *testing.T) []LegacyBlogPost {
	t.Helper()
	records, err := ReadLegacyBlogPostsFile("testdata/legacy-blog.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return records
}

// setupMediaImport returns a migrated database, the data directory it lives
// in, and a source directory holding one jpeg and one PNG-alpha fixture under
// the flat legacy bucket keys.
func setupMediaImport(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	dir := testDataDir(t)
	db, err := database.Open(database.Config{DataDir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("Apply: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	sourceDir := filepath.Join(testDataDir(t))
	// Legacy key fixtures: <ts>-<uuid>.<ext>, the flat legacy bucket shape.
	writeSource(t, sourceDir, "1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg", jpegFixture(t, 640, 360))
	writeSource(t, sourceDir, "1778832678231-6d1c4453-5d43-4b6b-9f89-146a77655f1f.png", pngAlphaFixture(t))

	return db, dir, sourceDir
}

func writeSource(t *testing.T, dir, name string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatalf("write source fixture %s: %v", name, err)
	}
}

func jpegFixture(t *testing.T, w, h int) []byte {
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

func pngAlphaFixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.NRGBA{A: 64}) // translucent → PNG output
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}
	return buf.Bytes()
}

// validUserWithTimestamps builds a synthetic LegacyUser with the given raw
// JSON timestamp fragments ("" leaves the field absent).
func validUserWithTimestamps(t *testing.T, id, created, updated string) LegacyUser {
	t.Helper()
	rec := LegacyUser{
		ID:       MongoObjectID{OID: id},
		Username: "MixedCase",
		Email:    "MixedCase@Example.com",
		Password: v("$2b$", "12"),
		Role:     "user",
		Status:   "active",
		Avatar:   "https://minio.example.com/media/avatar.jpg",
	}
	if created != "" {
		rec.CreatedAt = MongoDate{raw: jsonRaw(t, created)}
	}
	if updated != "" {
		rec.UpdatedAt = MongoDate{raw: jsonRaw(t, updated)}
	}
	return rec
}
