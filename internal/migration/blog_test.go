package migration

import (
	"encoding/json"
	"regexp"
	"testing"
)

// Transform unit tests pin the mapping rules: download classification,
// timestamp mapping and fallbacks, derived target IDs.

//go:fix inline
func strPtr(s string) *string { return new(s) }

// jsonRaw wraps a raw JSON fragment as json.RawMessage (for MongoDate tests).
func jsonRaw(t *testing.T, fragment string) []byte {
	t.Helper()
	var v json.RawMessage
	if err := json.Unmarshal([]byte(fragment), &v); err != nil {
		t.Fatalf("jsonRaw(%q): %v", fragment, err)
	}
	return v
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestTransformBlogPost_FullRecord(t *testing.T) {
	t.Parallel()

	rec := LegacyBlogPost{
		ID:                    MongoObjectID{OID: "65b0a1c2d3e4f5a6b7c8d9e0"},
		Title:                 "Title",
		Subtitle:              "Sub",
		Description:           "Desc",
		Thumbnail:             "https://example.com/t.jpg",
		DownloadLink:          new("magnet:?xt=urn:btih:aaa"),
		DownloadLinkTorrent:   new("https://example.com/a.torrent"),
		DownloadLink4k:        new("magnet:?xt=urn:btih:bbb"),
		DownloadLink4kTorrent: new("https://example.com/b.torrent"),
		DateTimeCreated:       "2024-03-15T18:00:00.000Z",
		Creator:               &MongoObjectID{OID: "65a0b1c2d3e4f5a6b7c8d9e1"},
		CreatedAt:             MongoDate{raw: jsonRaw(t, `"2024-03-15T18:05:00.000Z"`)},
		UpdatedAt:             MongoDate{raw: jsonRaw(t, `{"$numberLong":"1710529500000"}`)},
	}

	tr, err := TransformBlogPost(rec)
	if err != nil {
		t.Fatalf("TransformBlogPost: %v", err)
	}
	if tr.RejectReason != "" {
		t.Fatalf("unexpected rejection: %s", tr.RejectReason)
	}

	// Derived target ID — NOT the legacy ObjectId.
	if !hex32.MatchString(tr.Post.ID) {
		t.Errorf("target ID should be 32-hex, got %q", tr.Post.ID)
	}
	if tr.Post.ID == rec.ID.OID {
		t.Error("target ID must not be the legacy ObjectId")
	}

	// Timestamps preserved exactly (ordering must not break).
	if tr.Post.PublishedAtMS != 1710525600000 {
		t.Errorf("PublishedAtMS: got %d, want 1710525600000", tr.Post.PublishedAtMS)
	}
	if tr.Post.CreatedAtMS != 1710525900000 {
		t.Errorf("CreatedAtMS: got %d, want 1710525900000", tr.Post.CreatedAtMS)
	}
	if tr.Post.UpdatedAtMS != 1710529500000 {
		t.Errorf("UpdatedAtMS: got %d, want 1710529500000", tr.Post.UpdatedAtMS)
	}
	if tr.Post.Status != "published" {
		t.Errorf("Status: got %q, want published", tr.Post.Status)
	}
	if tr.Post.CreatorSourceID != "65a0b1c2d3e4f5a6b7c8d9e1" {
		t.Errorf("CreatorSourceID: got %q", tr.Post.CreatorSourceID)
	}

	// Both resolutions present with both links.
	if len(tr.Post.Downloads) != 2 {
		t.Fatalf("downloads: got %d rows, want 2", len(tr.Post.Downloads))
	}
	if tr.Post.Downloads[0].Resolution != "1080p" || tr.Post.Downloads[0].Position != 0 {
		t.Errorf("first download should be 1080p position 0, got %+v", tr.Post.Downloads[0])
	}
	if tr.Post.Downloads[1].Resolution != "2160p" || tr.Post.Downloads[1].Position != 1 {
		t.Errorf("second download should be 2160p position 1, got %+v", tr.Post.Downloads[1])
	}
	if tr.DownloadCounts["1080p"][statePresent] != 2 || tr.DownloadCounts["2160p"][statePresent] != 2 {
		t.Errorf("counts: want 2 present per resolution, got %v", tr.DownloadCounts)
	}
}

func TestTransformBlogPost_EmptyAndAbsentProduceNoRow(t *testing.T) {
	t.Parallel()

	// Torrent-only 1080p; 4k magnet EMPTY ("") + 4k torrent ABSENT (nil).
	rec := LegacyBlogPost{
		ID:                    MongoObjectID{OID: "65c0a1c2d3e4f5a6b7c8d9e2"},
		Title:                 "T",
		Thumbnail:             "https://example.com/t.jpg",
		DownloadLink:          nil,
		DownloadLinkTorrent:   new("https://example.com/a.torrent"),
		DownloadLink4k:        new(""),
		DownloadLink4kTorrent: nil,
		DateTimeCreated:       "2024-02-10T12:00:00.000Z",
		CreatedAt:             MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
		UpdatedAt:             MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
	}

	tr, err := TransformBlogPost(rec)
	if err != nil {
		t.Fatalf("TransformBlogPost: %v", err)
	}

	// One row: 1080p torrent-only (magnet NULL). The empty/absent 4K pair
	// produces NO 2160p row.
	if len(tr.Post.Downloads) != 1 {
		t.Fatalf("downloads: got %d rows, want 1", len(tr.Post.Downloads))
	}
	if tr.Post.Downloads[0].MagnetLink != nil {
		t.Error("1080p magnet should be NULL when absent")
	}
	if tr.Post.Downloads[0].TorrentLink == nil || *tr.Post.Downloads[0].TorrentLink != "https://example.com/a.torrent" {
		t.Error("1080p torrent should be preserved")
	}
	if tr.DownloadCounts["1080p"][stateAbsent] != 1 || tr.DownloadCounts["1080p"][statePresent] != 1 {
		t.Errorf("1080p counts: want 1 absent + 1 present, got %v", tr.DownloadCounts["1080p"])
	}
	if tr.DownloadCounts["2160p"][stateEmpty] != 1 || tr.DownloadCounts["2160p"][stateAbsent] != 1 {
		t.Errorf("2160p counts: want 1 empty + 1 absent, got %v", tr.DownloadCounts["2160p"])
	}
}

func TestTransformBlogPost_MalformedPreservedWithWarning(t *testing.T) {
	t.Parallel()

	rec := LegacyBlogPost{
		ID:                  MongoObjectID{OID: "65d0a1c2d3e4f5a6b7c8d9e3"},
		Title:               "T",
		Thumbnail:           "https://example.com/t.jpg",
		DownloadLink:        new("not a valid link at all"),
		DownloadLinkTorrent: new(""),
		DateTimeCreated:     "2024-01-20T09:30:00.000Z",
		CreatedAt:           MongoDate{raw: jsonRaw(t, `"2024-01-20T09:30:00.000Z"`)},
		UpdatedAt:           MongoDate{raw: jsonRaw(t, `"2024-01-20T09:35:00.000Z"`)},
	}

	tr, err := TransformBlogPost(rec)
	if err != nil {
		t.Fatalf("TransformBlogPost: %v", err)
	}

	// Malformed values are PRESERVED as-is with a warning.
	if len(tr.Post.Downloads) != 1 {
		t.Fatalf("downloads: got %d rows, want 1", len(tr.Post.Downloads))
	}
	if tr.Post.Downloads[0].MagnetLink == nil || *tr.Post.Downloads[0].MagnetLink != "not a valid link at all" {
		t.Error("malformed magnet should be preserved as-is")
	}
	if tr.Post.Downloads[0].TorrentLink != nil {
		t.Error("empty torrent should map to NULL")
	}
	if tr.DownloadCounts["1080p"][stateMalformed] != 1 || tr.DownloadCounts["1080p"][stateEmpty] != 1 {
		t.Errorf("1080p counts: want 1 malformed + 1 empty, got %v", tr.DownloadCounts["1080p"])
	}
	if len(tr.Warnings) != 1 {
		t.Fatalf("warnings: got %d, want 1", len(tr.Warnings))
	}
	if tr.Warnings[0].Field != "1080p download" {
		t.Errorf("warning field: got %q", tr.Warnings[0].Field)
	}
}

func TestTransformBlogPost_TimestampFallbacks(t *testing.T) {
	t.Parallel()

	t.Run("missing dateTimeCreated falls back to createdAt", func(t *testing.T) {
		rec := LegacyBlogPost{
			ID:        MongoObjectID{OID: "65e0a1c2d3e4f5a6b7c8d9e4"},
			Title:     "T",
			Thumbnail: "https://example.com/t.jpg",
			CreatedAt: MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
			UpdatedAt: MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
		}
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.Post.PublishedAtMS != tr.Post.CreatedAtMS {
			t.Errorf("published should fall back to created, got %d vs %d", tr.Post.PublishedAtMS, tr.Post.CreatedAtMS)
		}
		if len(tr.Warnings) != 1 || tr.Warnings[0].Field != "dateTimeCreated" {
			t.Errorf("TransformBlogPost warnings = %v, want one dateTimeCreated warning", tr.Warnings)
		}
	})

	t.Run("unparseable dateTimeCreated falls back to createdAt", func(t *testing.T) {
		rec := LegacyBlogPost{
			ID:              MongoObjectID{OID: "65e0a1c2d3e4f5a6b7c8d9e5"},
			Title:           "T",
			Thumbnail:       "https://example.com/t.jpg",
			DateTimeCreated: "not-a-date",
			CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
			UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
		}
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.Post.PublishedAtMS != tr.Post.CreatedAtMS {
			t.Error("published should fall back to created")
		}
	})

	t.Run("missing updatedAt falls back to createdAt", func(t *testing.T) {
		rec := LegacyBlogPost{
			ID:              MongoObjectID{OID: "65e0a1c2d3e4f5a6b7c8d9e6"},
			Title:           "T",
			Thumbnail:       "https://example.com/t.jpg",
			DateTimeCreated: "2024-02-10T12:00:00.000Z",
			CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
			// UpdatedAt absent.
		}
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.Post.UpdatedAtMS != tr.Post.CreatedAtMS {
			t.Error("updated should fall back to created")
		}
		if len(tr.Warnings) != 1 || tr.Warnings[0].Field != "updatedAt" {
			t.Errorf("TransformBlogPost warnings = %v, want one updatedAt warning", tr.Warnings)
		}
	})
}

func TestTransformBlogPost_CreatedAtFallback(t *testing.T) {
	t.Parallel()

	t.Run("missing createdAt and dateTimeCreated rejects", func(t *testing.T) {
		rec := LegacyBlogPost{
			ID:        MongoObjectID{OID: "65f0a1c2d3e4f5a6b7c8d9e7"},
			Title:     "T",
			Thumbnail: "https://example.com/t.jpg",
		}

		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection when createdAt AND dateTimeCreated are both unusable — created_at_ms must never be fabricated")
		}
	})

	t.Run("missing createdAt derives from dateTimeCreated with a warning", func(t *testing.T) {
		// The production shape: most legacy blog posts carry dateTimeCreated
		// only (see internal/migration/AGENTS.md).
		rec := LegacyBlogPost{
			ID:              MongoObjectID{OID: "65f0a1c2d3e4f5a6b7c8d9e8"},
			Title:           "T",
			Thumbnail:       "https://example.com/t.jpg",
			DateTimeCreated: "2024-02-10T12:00:00.000Z",
			// CreatedAt absent; UpdatedAt present and equal so the only
			// warning is the createdAt derivation.
			UpdatedAt: MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
		}

		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason != "" {
			t.Fatalf("unexpected rejection: %s", tr.RejectReason)
		}
		if tr.Post.CreatedAtMS != 1707566400000 {
			t.Errorf("CreatedAtMS: got %d, want the dateTimeCreated instant", tr.Post.CreatedAtMS)
		}
		if tr.Post.PublishedAtMS != tr.Post.CreatedAtMS {
			t.Errorf("published and created should agree, got %d vs %d", tr.Post.PublishedAtMS, tr.Post.CreatedAtMS)
		}
		if len(tr.Warnings) != 1 || tr.Warnings[0].Field != "createdAt" {
			t.Errorf("TransformBlogPost warnings = %v, want one createdAt derivation warning", tr.Warnings)
		}
	})

	t.Run("missing createdAt with unparseable dateTimeCreated rejects", func(t *testing.T) {
		rec := LegacyBlogPost{
			ID:              MongoObjectID{OID: "65f0a1c2d3e4f5a6b7c8d9e9"},
			Title:           "T",
			Thumbnail:       "https://example.com/t.jpg",
			DateTimeCreated: "not-a-date",
		}

		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection — neither createdAt nor dateTimeCreated is usable")
		}
	})

	t.Run("missing createdAt with non-positive dateTimeCreated rejects", func(t *testing.T) {
		rec := LegacyBlogPost{
			ID:              MongoObjectID{OID: "65f0a1c2d3e4f5a6b7c8d9e10"},
			Title:           "T",
			Thumbnail:       "https://example.com/t.jpg",
			DateTimeCreated: "1970-01-01T00:00:00.000Z",
		}

		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection — a non-positive dateTimeCreated cannot satisfy created_at_ms > 0")
		}
	})

	t.Run("updatedAt precedes a derived createdAt rejects", func(t *testing.T) {
		// createdAt is absent, so created_at_ms derives from
		// dateTimeCreated; a present-but-earlier updatedAt still violates
		// updated_at_ms >= created_at_ms — reject, never clamp.
		rec := LegacyBlogPost{
			ID:              MongoObjectID{OID: "65f0a1c2d3e4f5a6b7c8d9e11"},
			Title:           "T",
			Thumbnail:       "https://example.com/t.jpg",
			DateTimeCreated: "2024-02-10T12:00:00.000Z",
			UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-01T10:00:00.000Z"`)},
		}

		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for updatedAt < the derived created_at_ms")
		}
	})
}

func TestTransformBlogPost_TargetConstraintViolationsReject(t *testing.T) {
	t.Parallel()

	base := LegacyBlogPost{
		ID:              MongoObjectID{OID: "65f0a1c2d3e4f5a6b7c8d9e8"},
		Title:           "T",
		Thumbnail:       "https://example.com/t.jpg",
		DateTimeCreated: "2024-01-10T10:00:00.000Z",
		CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
		UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
	}

	t.Run("empty title", func(t *testing.T) {
		rec := base
		rec.Title = "  "
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for an empty title — emit a rejection item when data cannot map safely")
		}
	})

	t.Run("missing _id", func(t *testing.T) {
		// The target id derives from the legacy ObjectId — no source id means
		// no derivable target id: reject per-record, never fabricate or abort
		// the import.
		rec := base
		rec.ID = MongoObjectID{}
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for a missing _id")
		}
	})

	t.Run("empty thumbnail", func(t *testing.T) {
		rec := base
		rec.Thumbnail = ""
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for an empty thumbnail")
		}
	})

	t.Run("epoch-zero createdAt", func(t *testing.T) {
		// A parseable but non-positive instant passes Millis() yet
		// violates created_at_ms > 0 at INSERT — must reject per-record,
		// never abort the whole import with a raw CHECK error.
		rec := base
		rec.CreatedAt = MongoDate{raw: jsonRaw(t, `{"$numberLong":"0"}`)}
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for a non-positive createdAt")
		}
	})

	t.Run("negative createdAt", func(t *testing.T) {
		// Pins the <= boundary: a pre-epoch instant is non-positive too
		// and must not reach the INSERT.
		rec := base
		rec.CreatedAt = MongoDate{raw: jsonRaw(t, `{"$numberLong":"-5"}`)}
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for a negative createdAt")
		}
	})

	t.Run("updatedAt precedes createdAt", func(t *testing.T) {
		rec := base
		rec.UpdatedAt = MongoDate{raw: jsonRaw(t, `"2024-01-01T10:00:00.000Z"`)}
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for updatedAt < createdAt (updated_at_ms >= created_at_ms)")
		}
	})

	t.Run("epoch-zero dateTimeCreated falls back to createdAt", func(t *testing.T) {
		// A non-positive publication instant is treated like the missing
		// case: fall back to createdAt with a warning so visible content
		// does not silently become a draft.
		rec := base
		rec.DateTimeCreated = "1970-01-01T00:00:00.000Z"
		tr, err := TransformBlogPost(rec)
		if err != nil {
			t.Fatalf("TransformBlogPost: %v", err)
		}
		if tr.RejectReason != "" {
			t.Fatalf("unexpected rejection: %s", tr.RejectReason)
		}
		if tr.Post.PublishedAtMS != tr.Post.CreatedAtMS {
			t.Fatalf("published_at_ms = %d, want fallback to created_at_ms %d", tr.Post.PublishedAtMS, tr.Post.CreatedAtMS)
		}
		if len(tr.Warnings) == 0 {
			t.Fatal("expected a fallback warning for the non-positive dateTimeCreated")
		}
	})
}
