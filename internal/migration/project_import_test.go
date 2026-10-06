package migration

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

// loadProjectFixture reads the synthetic legacy project records through the
// same reader the import command uses (rule 8: hand-built synthetic data,
// never production values).
func loadProjectFixture(t *testing.T) []LegacyProject {
	t.Helper()
	records, err := ReadLegacyProjectsFile("testdata/legacy-projects.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return records
}

// TestImportProjects pins the projects end-to-end: synthetic records import
// into a fresh migrated database and the reconciliation report tells the
// whole truth — derived target IDs mapped from source IDs, batch-download
// rows with preserved sub-ObjectIds, link classifications, warnings, and an
// explicit updater orphan.
func TestImportProjects(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	// The fixture's first record references this user as updatedBy —
	// pre-insert it so the updater RESOLVES. The third record references an
	// unknown user — that one must be reported as an orphan.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "65a0b1c2d3e4f5a6b7c8d9e1",
		Username:    "Synthetic",
		CreatedAtMS: 1700000000000,
	})

	report, err := ImportProjects(ctx, db, loadProjectFixture(t))
	if err != nil {
		t.Fatalf("ImportProjects: %v", err)
	}

	if report.Source.Projects != 3 || report.Source.Imported != 3 || report.Source.Rejected != 0 {
		t.Errorf("source counts: got %+v", report.Source)
	}

	// Identifier mapping: 3 pairs, all derived 32-hex targets, none equal to
	// the legacy ObjectId.
	if len(report.Identifiers.SourceToTarget) != 3 {
		t.Fatalf("identifier mapping: got %d pairs, want 3", len(report.Identifiers.SourceToTarget))
	}
	for _, m := range report.Identifiers.SourceToTarget {
		if !hex32.MatchString(m.TargetID) {
			t.Errorf("target ID %q is not 32-hex", m.TargetID)
		}
		if m.SourceID == m.TargetID {
			t.Errorf("target ID must differ from source ID %q", m.SourceID)
		}
	}

	// Downloads across the three records:
	//   rec1: 3 entries (both links; torrent-only; both empty) → 3 rows
	//   rec2: 1 entry (magnet-only, fresh row id) → 1 row
	//   rec3: 2 entries (nameless → dropped; malformed magnet + torrent) → 1 row
	// Link classifications (dropped entries are NOT classified):
	//   magnet: present 2, absent 1, empty 1, malformed 1
	//   torrent: present 3, empty 1, absent 1, malformed 0
	if got := report.ProjectDownloads; got.Rows != 5 || got.EntriesDropped != 1 {
		t.Errorf("projectDownloads: got %+v, want rows=5 entriesDropped=1", got)
	}
	if m := report.ProjectDownloads.Magnet; m.Present != 2 || m.Absent != 1 || m.Empty != 1 || m.Malformed != 1 {
		t.Errorf("magnet counts: got %+v, want present=2 absent=1 empty=1 malformed=1", m)
	}
	if to := report.ProjectDownloads.Torrent; to.Present != 3 || to.Empty != 1 || to.Absent != 1 || to.Malformed != 0 {
		t.Errorf("torrent counts: got %+v, want present=3 empty=1 absent=1 malformed=0", to)
	}

	// Relationships: record 1's updater resolves to the pre-inserted
	// synthetic user; record 3's updater is an orphan; record 2 carries no
	// updater (absent, not orphaned). Creator counts stay 0 for migrated
	// projects.
	if report.Relationships.UpdaterResolved != 1 || report.Relationships.UpdaterOrphans != 1 {
		t.Errorf("updater relationships: got %+v", report.Relationships)
	}
	if report.Relationships.CreatorResolved != 0 || report.Relationships.CreatorOrphans != 0 {
		t.Errorf("creator relationships: got %+v, want 0/0", report.Relationships)
	}

	// Warnings: record 2 (missing dateTimeCreated + missing updatedAt),
	// record 3 (nameless entry + malformed magnet).
	if len(report.Warnings) != 4 {
		t.Fatalf("warnings: got %d, want 4: %+v", len(report.Warnings), report.Warnings)
	}

	// The database itself: 3 projects, 5 download rows, preserved
	// timestamps, NULL creator_id everywhere.
	var projects int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects").Scan(&projects); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if projects != 3 {
		t.Errorf("projects rows: got %d, want 3", projects)
	}
	var downloads int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM project_downloads").Scan(&downloads); err != nil {
		t.Fatalf("count downloads: %v", err)
	}
	if downloads != 5 {
		t.Errorf("project_downloads rows: got %d, want 5", downloads)
	}

	var creatorCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE creator_id IS NOT NULL").Scan(&creatorCount); err != nil {
		t.Fatalf("count creators: %v", err)
	}
	if creatorCount != 0 {
		t.Errorf("creator_id must stay NULL for migrated projects, found %d rows", creatorCount)
	}

	// Ordering: the full record's legacy publication instant survives exactly.
	var published, created int64
	if err := db.QueryRowContext(ctx,
		"SELECT published_at_ms, created_at_ms FROM projects WHERE title = 'Συνθετικό πρότζεκτ ένα (συνθετικό)'",
	).Scan(&published, &created); err != nil {
		t.Fatalf("query full record: %v", err)
	}
	if published != 1710525600000 || created != 1710525900000 {
		t.Errorf("timestamps: got published=%d created=%d", published, created)
	}

	// The legacy sub-ObjectIds survive as download-row ids.
	var subIDCount int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM project_downloads WHERE id = '65b0f1a2b3c4d5e6f7a8b9c1'",
	).Scan(&subIDCount); err != nil {
		t.Fatalf("query sub-ObjectId: %v", err)
	}
	if subIDCount != 1 {
		t.Errorf("preserved sub-ObjectId row: got %d, want 1", subIDCount)
	}

	// The orphaned updater reference is NULL in the target row.
	var updaterID sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT updater_id FROM projects WHERE title = 'Τρίτο συνθετικό πρότζεκτ'",
	).Scan(&updaterID); err != nil {
		t.Fatalf("query orphan record: %v", err)
	}
	if updaterID.Valid {
		t.Errorf("orphan updater should be NULL, got %q", updaterID.String)
	}

	// Derived target IDs never collide with the source identifiers.
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM projects WHERE id IN ('65b0d1a2b3c4d5e6f7a8b9e0','65c0d1a2b3c4d5e6f7a8b9e2','65d0d1a2b3c4d5e6f7a8b9e3')",
	).Scan(&count); err != nil {
		t.Fatalf("query legacy ids: %v", err)
	}
	if count != 0 {
		t.Errorf("legacy ObjectIds must not appear as target IDs, found %d", count)
	}
}

// TestImportProjects_DuplicateSlug pins the UNIQUE-slug rule: the FIRST
// record imports, later duplicates are rejected with a concise reason —
// never a raw UNIQUE-constraint abort.
func TestImportProjects_DuplicateSlug(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	recs := []LegacyProject{
		{
			ID:              MongoObjectID{OID: "6600d1a2b3c4d5e6f7a8b900"},
			Title:           "First",
			Thumbnail:       "https://example.com/f.jpg",
			Slug:            "duplicated-slug",
			DateTimeCreated: "2024-01-10T10:00:00.000Z",
			CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
			UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
		},
		{
			ID:              MongoObjectID{OID: "6600d1a2b3c4d5e6f7a8b901"},
			Title:           "Second",
			Thumbnail:       "https://example.com/s.jpg",
			Slug:            "duplicated-slug",
			DateTimeCreated: "2024-01-11T10:00:00.000Z",
			CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-11T10:00:00.000Z"`)},
			UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-11T10:00:00.000Z"`)},
		},
	}

	report, err := ImportProjects(ctx, db, recs)
	if err != nil {
		t.Fatalf("ImportProjects: %v", err)
	}
	if report.Source.Imported != 1 || report.Source.Rejected != 1 {
		t.Errorf("counts: got imported=%d rejected=%d", report.Source.Imported, report.Source.Rejected)
	}
	if len(report.RejectedRecords) != 1 || report.RejectedRecords[0].SourceID != "6600d1a2b3c4d5e6f7a8b901" {
		t.Errorf("rejected records: got %+v", report.RejectedRecords)
	}
	if !strings.Contains(report.RejectedRecords[0].Reason, "duplicate slug") {
		t.Errorf("reject reason should mention the duplicate slug: %q", report.RejectedRecords[0].Reason)
	}

	var projects int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects").Scan(&projects); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if projects != 1 {
		t.Errorf("projects rows: got %d, want 1 (the duplicate must not insert)", projects)
	}
}

// TestImportProjects_RejectedRecord pins the fail-safe: a record without a
// usable createdAt is reported as rejected and never half-imported, and the
// report shape stays stable (empty sections marshal as [], not null).
func TestImportProjects_RejectedRecord(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	recs := []LegacyProject{
		{
			ID:        MongoObjectID{OID: "65f0d1a2b3c4d5e6f7a8b902"},
			Title:     "No dates",
			Thumbnail: "https://example.com/t.jpg",
			Slug:      "no-dates",
		},
		{
			ID:              MongoObjectID{OID: "65f0d1a2b3c4d5e6f7a8b903"},
			Title:           "Fine",
			Thumbnail:       "https://example.com/t.jpg",
			Slug:            "fine",
			DateTimeCreated: "2024-01-10T10:00:00.000Z",
			CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
			UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
		},
	}

	report, err := ImportProjects(ctx, db, recs)
	if err != nil {
		t.Fatalf("ImportProjects: %v", err)
	}
	if report.Source.Imported != 1 || report.Source.Rejected != 1 {
		t.Errorf("counts: got imported=%d rejected=%d", report.Source.Imported, report.Source.Rejected)
	}
	if len(report.RejectedRecords) != 1 || report.RejectedRecords[0].SourceID != "65f0d1a2b3c4d5e6f7a8b902" {
		t.Errorf("rejected records: got %+v", report.RejectedRecords)
	}

	var projects int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects").Scan(&projects); err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if projects != 1 {
		t.Errorf("projects rows: got %d, want 1 (rejected record must not insert)", projects)
	}

	// The report shape is stable: empty sections marshal as [] not null.
	out, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var shape struct {
		Warnings        []json.RawMessage `json:"warnings"`
		RejectedRecords []json.RawMessage `json:"rejectedRecords"`
		Identifiers     struct {
			SourceToTarget []json.RawMessage `json:"sourceToTarget"`
		} `json:"identifiers"`
	}
	if err := json.Unmarshal(out, &shape); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	for _, section := range []struct {
		name    string
		decoded []json.RawMessage
	}{
		{"warnings", shape.Warnings},
		{"rejectedRecords", shape.RejectedRecords},
		{"identifiers.sourceToTarget", shape.Identifiers.SourceToTarget},
	} {
		if section.decoded == nil {
			t.Errorf("report %s decoded as null, want an array (never null)", section.name)
		}
	}
}
