package migration

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"sick-fansubs/internal/store/storetest"
)

// TestImportBlogPosts pins the pilot end-to-end: synthetic records import
// into a fresh migrated database and the reconciliation report tells the
// whole truth — derived target IDs mapped from source IDs, download rows,
// classification counts, warnings, and explicit creator orphans.
func TestImportBlogPosts(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	// The fixture's first record references this user — pre-insert it so the
	// creator RESOLVES. The third record references an unknown user — that
	// one must be reported as an orphan.
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "65a0b1c2d3e4f5a6b7c8d9e1",
		Username:    "Synthetic",
		CreatedAtMS: 1700000000000,
	})

	report, err := ImportBlogPosts(ctx, db, loadFixture(t))
	if err != nil {
		t.Fatalf("ImportBlogPosts: %v", err)
	}

	if report.Source.BlogPosts != 3 || report.Source.Imported != 3 || report.Source.Rejected != 0 {
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

	// Downloads: record 1 → one 1080p row + one 2160p row; record 2 → one
	// 1080p row; record 3 → one 1080p row (malformed, preserved).
	// Classification totals across the three records:
	//   1080p: rec1 present+present, rec2 absent+present,
	//          rec3 malformed+empty
	//   2160p: rec1 present+present, rec2 empty+absent,
	//          rec3 absent+absent
	if got := report.Downloads["1080p"]; got.Rows != 3 || got.Present != 3 || got.Absent != 1 || got.Malformed != 1 || got.Empty != 1 {
		t.Errorf("1080p: got %+v, want rows=3 present=3 absent=1 malformed=1 empty=1", got)
	}
	if got := report.Downloads["2160p"]; got.Rows != 1 || got.Present != 2 || got.Empty != 1 || got.Absent != 3 {
		t.Errorf("2160p: got %+v, want rows=1 present=2 empty=1 absent=3", got)
	}

	// Relationships: record 1's creator AND updater resolve to the
	// pre-inserted synthetic user; record 3's creator is an orphan;
	// records 2/3 carry no updater (absent, not orphaned).
	if report.Relationships.CreatorResolved != 1 || report.Relationships.CreatorOrphans != 1 {
		t.Errorf("creator relationships: got %+v", report.Relationships)
	}
	if report.Relationships.UpdaterResolved != 1 || report.Relationships.UpdaterOrphans != 0 {
		t.Errorf("updater relationships: got %+v", report.Relationships)
	}

	// Warnings: record 2 (missing dateTimeCreated + missing updatedAt),
	// record 3 (malformed 1080p magnet).
	if len(report.Warnings) != 3 {
		t.Fatalf("warnings: got %d, want 3: %+v", len(report.Warnings), report.Warnings)
	}

	// The database itself: 3 posts, 4 download rows, preserved timestamps.
	var posts int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blog_posts").Scan(&posts); err != nil {
		t.Fatalf("count posts: %v", err)
	}
	if posts != 3 {
		t.Errorf("blog_posts rows: got %d, want 3", posts)
	}
	var downloads int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blog_post_downloads").Scan(&downloads); err != nil {
		t.Fatalf("count downloads: %v", err)
	}
	if downloads != 4 {
		t.Errorf("blog_post_downloads rows: got %d, want 4", downloads)
	}

	// Ordering: the full record's legacy publication instant survives exactly.
	var published, created int64
	if err := db.QueryRowContext(ctx,
		"SELECT published_at_ms, created_at_ms FROM blog_posts WHERE title = 'Ανατομία ενός Fall (συνθετικό)'",
	).Scan(&published, &created); err != nil {
		t.Fatalf("query full record: %v", err)
	}
	if published != 1710525600000 || created != 1710525900000 {
		t.Errorf("timestamps: got published=%d created=%d", published, created)
	}

	// The orphaned creator reference is NULL in the target row.
	var creatorID sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT creator_id FROM blog_posts WHERE title = 'Τρίτη συνθετική ανάρτηση'",
	).Scan(&creatorID); err != nil {
		t.Fatalf("query orphan record: %v", err)
	}
	if creatorID.Valid {
		t.Errorf("orphan creator should be NULL, got %q", creatorID.String)
	}

	// Derived target IDs never collide with the source identifiers.
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM blog_posts WHERE id IN ('65b0a1c2d3e4f5a6b7c8d9e0','65c0a1c2d3e4f5a6b7c8d9e2','65d0a1c2d3e4f5a6b7c8d9e3')",
	).Scan(&count); err != nil {
		t.Fatalf("query legacy ids: %v", err)
	}
	if count != 0 {
		t.Errorf("legacy ObjectIds must not appear as target IDs, found %d", count)
	}
}

// TestImportBlogPosts_RejectedRecord pins the fail-safe: a record with no
// usable createdAt source (neither createdAt nor dateTimeCreated) is
// reported as rejected and never half-imported.
func TestImportBlogPosts_RejectedRecord(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	recs := []LegacyBlogPost{
		{
			ID:        MongoObjectID{OID: "65f0a1c2d3e4f5a6b7c8d9e7"},
			Title:     "No dates",
			Thumbnail: "https://example.com/t.jpg",
		},
		{
			ID:              MongoObjectID{OID: "65f0a1c2d3e4f5a6b7c8d9e8"},
			Title:           "Fine",
			Thumbnail:       "https://example.com/t.jpg",
			DateTimeCreated: "2024-01-10T10:00:00.000Z",
			CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
			UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
		},
	}

	report, err := ImportBlogPosts(ctx, db, recs)
	if err != nil {
		t.Fatalf("ImportBlogPosts: %v", err)
	}
	if report.Source.Imported != 1 || report.Source.Rejected != 1 {
		t.Errorf("counts: got imported=%d rejected=%d", report.Source.Imported, report.Source.Rejected)
	}
	if len(report.RejectedRecords) != 1 || report.RejectedRecords[0].SourceID != "65f0a1c2d3e4f5a6b7c8d9e7" {
		t.Errorf("rejected records: got %+v", report.RejectedRecords)
	}

	var posts int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blog_posts").Scan(&posts); err != nil {
		t.Fatalf("count posts: %v", err)
	}
	if posts != 1 {
		t.Errorf("blog_posts rows: got %d, want 1 (rejected record must not insert)", posts)
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

// TestImportUsers_ExistingTargetRowRejectsPerRecord pins the re-run arm: a
// record colliding with an already-imported target row rejects per-record
// with a reason naming the collided constraint — never an operator-stopping
// error. The in-batch canonical map cannot see this class, so the UNIQUE
// violation arm is the only path.
func TestImportUsers_ExistingTargetRowRejectsPerRecord(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:          "65a0b1c2d3e4f5a6b7c8d9e1",
		Username:    "Existing",
		CreatedAtMS: 1700000000000,
	})

	// Same id as the seeded row — the INSERT collides on the primary key.
	sameID := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e1", `"2024-01-10T10:00:00.000Z"`, `"2024-01-10T10:00:00.000Z"`)
	sameID.Username, sameID.Email = "FreshName", "fresh@example.com"
	// A different id, the same canonical username as the seeded row.
	sameCanon := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e2", `"2024-01-10T10:00:00.000Z"`, `"2024-01-10T10:00:00.000Z"`)
	sameCanon.Username, sameCanon.Email = "existing", "another@example.com"
	// A different id and username, the seeded row's default email.
	sameEmail := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e3", `"2024-01-10T10:00:00.000Z"`, `"2024-01-10T10:00:00.000Z"`)
	sameEmail.Username, sameEmail.Email = "DifferentName", "existing@example.com"

	report, err := ImportUsers(ctx, db, []LegacyUser{sameID, sameCanon, sameEmail})
	if err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}
	if report.Source.Imported != 0 || report.Source.Rejected != 3 {
		t.Fatalf("source = %+v, want 0 imported / 3 rejected", report.Source)
	}
	reasons := map[string]string{}
	for _, r := range report.RejectedRecords {
		reasons[r.SourceID] = r.Reason
	}
	if got := reasons["65a0b1c2d3e4f5a6b7c8d9e1"]; !strings.Contains(got, "user id already present") {
		t.Errorf("id-collision reason = %q, want the id wording", got)
	}
	if got := reasons["65a0b1c2d3e4f5a6b7c8d9e2"]; !strings.Contains(got, "canonical username") {
		t.Errorf("canon-collision reason = %q, want the canonical-username wording", got)
	}
	if got := reasons["65a0b1c2d3e4f5a6b7c8d9e3"]; !strings.Contains(got, "canonical email") {
		t.Errorf("email-collision reason = %q, want the canonical-email wording", got)
	}
}
