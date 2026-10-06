package migration

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

// Avatar import tests: the join is export-side
// (users.avatar_url is NULL in the target — the deferred import state),
// processing is the avatar pipeline, and failures are per-object counts,
// never fatal.

func insertUserRow(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          id,
		Username:    "user" + id,
		Email:       id + "@example.com",
		CreatedAtMS: 500,
	})
}

func avatarURL(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var stored string
	if err := db.QueryRow(`SELECT avatar_url FROM users WHERE id = ?`, id).Scan(&stored); err != nil {
		t.Fatalf("read avatar_url for %s: %v", id, err)
	}
	return stored
}

// TestImportAvatars_Success pins the happy path: the export map joins by
// the preserved ObjectId, each distinct URL is processed once through the
// avatar pipeline (200×200 PNG at the storage layout), every matching row
// is rewritten, and the report counts match.
func TestImportAvatars_Success(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	insertUserRow(t, db, "65a0b1c2d3e4f5a6b7c8d9e1")
	insertUserRow(t, db, "65a0b1c2d3e4f5a6b7c8d9e2")
	insertUserRow(t, db, "65a0b1c2d3e4f5a6b7c8d9e3")

	const (
		sharedURL = "https://sickfansubs.com/media/images/1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
		ownURL    = "https://sickfansubs.com/media/images/1778832678231-6d1c4453-5d43-4b6b-9f89-146a77655f1f.png"
	)
	legacyByUserID := map[string]string{
		"65a0b1c2d3e4f5a6b7c8d9e1": sharedURL, // shared between two users
		"65a0b1c2d3e4f5a6b7c8d9e2": sharedURL,
		"65a0b1c2d3e4f5a6b7c8d9e3": ownURL,
		"65a0b1c2d3e4f5a6b7c8d9e4": "", // no legacy avatar — counted as a record, never processed
	}

	report, err := ImportAvatars(ctx, db, dataDir, sourceDir, legacyByUserID)
	if err != nil {
		t.Fatalf("ImportAvatars: %v", err)
	}

	if report.Source.ExportRecords != 4 || report.Source.UsersWithAvatars != 3 {
		t.Errorf("source = %+v, want 4 records / 3 avatar-bearing users", report.Source)
	}
	if report.Source.MatchedUsers != 3 {
		t.Errorf("matchedUsers = %d, want 3", report.Source.MatchedUsers)
	}
	if report.Source.DistinctURLs != 2 {
		t.Errorf("distinctLegacyUrls = %d, want 2", report.Source.DistinctURLs)
	}
	if report.Outcome.Processed != 2 || report.Outcome.RowsRewritten != 3 || report.Outcome.Failed != 0 {
		t.Errorf("outcome = %+v, want 2 processed / 3 rewritten / 0 failed", report.Outcome)
	}

	// Both users sharing the URL got the SAME storage reference (the
	// content-derived ID deduplicates them), the third got its own, and
	// every stored value carries the avatar pipeline's PNG output.
	shared := avatarURL(t, db, "65a0b1c2d3e4f5a6b7c8d9e1")
	if got := avatarURL(t, db, "65a0b1c2d3e4f5a6b7c8d9e2"); got != shared {
		t.Errorf("shared-URL users diverged: %q vs %q", shared, got)
	}
	if !strings.HasPrefix(shared, "media/images/") || !strings.HasSuffix(shared, ".png") {
		t.Errorf("stored avatar = %q, want media/images/<2hex>/<32hex>.png", shared)
	}
	own := avatarURL(t, db, "65a0b1c2d3e4f5a6b7c8d9e3")
	if !strings.HasPrefix(own, "media/images/") || !strings.HasSuffix(own, ".png") {
		t.Errorf("stored avatar = %q, want media/images/<2hex>/<32hex>.png", own)
	}
	if own == shared {
		t.Errorf("distinct sources mapped to the same ID %q", shared)
	}

	// The processed files exist at the storage layout.
	for _, rel := range []string{shared, own} {
		if _, err := openProcessed(t, dataDir, rel); err != nil {
			t.Errorf("processed file for %q missing: %v", rel, err)
		}
	}
}

// openProcessed opens a processed file at the storage layout.
func openProcessed(t *testing.T, dataDir, rel string) (*os.File, error) {
	t.Helper()
	f, err := os.Open(filepath.Join(dataDir, rel))
	if err != nil {
		return nil, err
	}
	return f, nil
}

// TestImportAvatars_FailuresAreCountedNotFatal pins the per-object failure
// contract: a missing source file, a non-absolute URL (badReferenceShape),
// and undecodable bytes are all counted with machine reasons and never
// abort the run.
func TestImportAvatars_FailuresAreCountedNotFatal(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	insertUserRow(t, db, "65a0b1c2d3e4f5a6b7c8d9e1")
	insertUserRow(t, db, "65a0b1c2d3e4f5a6b7c8d9e2")
	insertUserRow(t, db, "65a0b1c2d3e4f5a6b7c8d9e3")
	insertUserRow(t, db, "65a0b1c2d3e4f5a6b7c8d9e4")

	// A valid source for the success leg.
	writeSource(t, sourceDir, "1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg", jpegFixture(t, 320, 320))

	legacyByUserID := map[string]string{
		"65a0b1c2d3e4f5a6b7c8d9e1": "https://sickfansubs.com/media/images/1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg", // good
		"65a0b1c2d3e4f5a6b7c8d9e2": "https://sickfansubs.com/media/images/9999-missing.jpg",                                       // missing source file
		"65a0b1c2d3e4f5a6b7c8d9e3": "relative/avatar.png",                                                                         // non-absolute → badReferenceShape
		"65a0b1c2d3e4f5a6b7c8d9e4": "https://sickfansubs.com/media/images/1778832678231-6d1c4453-5d43-4b6b-9f89-146a77655f1f.png", // present but undecodable bytes below
	}
	// Replace the png fixture with a header-only PNG (valid IHDR, no pixel
	// data): DecodeConfig passes, the full decode fails → undecodable.
	writeSource(t, sourceDir, "1778832678231-6d1c4453-5d43-4b6b-9f89-146a77655f1f.png", pngHeaderBytes(t, 4, 4))

	report, err := ImportAvatars(ctx, db, dataDir, sourceDir, legacyByUserID)
	if err != nil {
		t.Fatalf("ImportAvatars: %v", err)
	}

	reasons := map[string]int{}
	for _, f := range report.Failures {
		reasons[f.Reason] = f.Count
	}
	if reasons["missingSourceFile"] != 1 || reasons["badReferenceShape"] != 1 || reasons["undecodable"] != 1 {
		t.Errorf("failures = %+v, want 1× missingSourceFile + 1× badReferenceShape + 1× undecodable", report.Failures)
	}
	if report.Outcome.Processed != 1 || report.Outcome.RowsRewritten != 1 || report.Outcome.Failed != 3 {
		t.Errorf("outcome = %+v, want 1 processed / 1 rewritten / 3 failed", report.Outcome)
	}
}

// TestImportAvatars_Idempotent pins the re-run contract: a rewritten row
// (avatar_url already set) is never matched again, so a second run over the
// same database scans zero rows and touches nothing.
func TestImportAvatars_Idempotent(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	insertUserRow(t, db, "65a0b1c2d3e4f5a6b7c8d9e1")
	const url = "https://sickfansubs.com/media/images/1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"

	first, err := ImportAvatars(ctx, db, dataDir, sourceDir,
		map[string]string{"65a0b1c2d3e4f5a6b7c8d9e1": url})
	if err != nil {
		t.Fatalf("first ImportAvatars: %v", err)
	}
	if first.Outcome.RowsRewritten != 1 {
		t.Fatalf("first run rewritten = %d, want 1", first.Outcome.RowsRewritten)
	}
	before := avatarURL(t, db, "65a0b1c2d3e4f5a6b7c8d9e1")

	second, err := ImportAvatars(ctx, db, dataDir, sourceDir,
		map[string]string{"65a0b1c2d3e4f5a6b7c8d9e1": url})
	if err != nil {
		t.Fatalf("second ImportAvatars: %v", err)
	}
	if second.Outcome.RowsRewritten != 0 || second.Outcome.Processed != 0 {
		t.Errorf("second run outcome = %+v, want 0 processed / 0 rewritten", second.Outcome)
	}
	if after := avatarURL(t, db, "65a0b1c2d3e4f5a6b7c8d9e1"); after != before {
		t.Errorf("reference changed on re-run: %q → %q", before, after)
	}
}

// TestImportAvatars_SkipsUnimportedUsers pins the join guard: a legacy URL
// whose user id is NOT in the target DB (a rejected/unimported record) is
// skipped silently — counted nowhere, never a failure, never a phantom row.
func TestImportAvatars_SkipsUnimportedUsers(t *testing.T) {
	t.Parallel()

	db, dataDir, sourceDir := setupMediaImport(t)
	ctx := t.Context()

	insertUserRow(t, db, "65a0b1c2d3e4f5a6b7c8d9e1")
	legacyByUserID := map[string]string{
		"65a0b1c2d3e4f5a6b7c8d9e1": "https://sickfansubs.com/media/images/1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg",
		"65ffffffffffffffffffffff": "https://sickfansubs.com/media/images/1778832678231-6d1c4453-5d43-4b6b-9f89-146a77655f1f.png", // never imported
	}

	report, err := ImportAvatars(ctx, db, dataDir, sourceDir, legacyByUserID)
	if err != nil {
		t.Fatalf("ImportAvatars: %v", err)
	}

	if report.Source.MatchedUsers != 1 {
		t.Errorf("matchedUsers = %d, want 1 (the unimported id is skipped)", report.Source.MatchedUsers)
	}
	if report.Outcome.Failed != 0 {
		t.Errorf("failures = %+v, want none for the skipped id", report.Failures)
	}
	if report.Outcome.RowsRewritten != 1 {
		t.Errorf("rowsRewritten = %d, want 1", report.Outcome.RowsRewritten)
	}
}
