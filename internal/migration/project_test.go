package migration

import (
	"testing"
)

// Project-transform unit tests pin the mapping rules: batch-entry
// normalization, link classification, timestamp mapping and fallbacks,
// derived target IDs, slug/title/thumbnail rejects, and the preserved
// sub-ObjectId row ids.

func TestTransformProject_FullRecord(t *testing.T) {
	t.Parallel()

	rec := LegacyProject{
		ID:          MongoObjectID{OID: "65b0d1a2b3c4d5e6f7a8b9e0"},
		Title:       "Title",
		Description: "Desc",
		Thumbnail:   "https://example.com/t.jpg",
		Slug:        "full-record",
		BatchDownloadLinks: []LegacyBatchDownload{
			{
				ID:                  &MongoObjectID{OID: "65b0f1a2b3c4d5e6f7a8b9c1"},
				Name:                "Batch A",
				DownloadLink:        new("magnet:?xt=urn:btih:aaa"),
				DownloadLinkTorrent: new("https://example.com/a.torrent"),
			},
			{
				ID:                  &MongoObjectID{OID: "65b0f1a2b3c4d5e6f7a8b9c2"},
				Name:                "Batch B",
				DownloadLinkTorrent: new("https://example.com/b.torrent"),
			},
			{
				ID:                  &MongoObjectID{OID: "65b0f1a2b3c4d5e6f7a8b9c3"},
				Name:                "Batch C",
				DownloadLink:        new(""),
				DownloadLinkTorrent: new(""),
			},
		},
		DateTimeCreated: "2024-03-15T18:00:00.000Z",
		UpdatedBy:       &MongoObjectID{OID: "65a0b1c2d3e4f5a6b7c8d9e1"},
		CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-03-15T18:05:00.000Z"`)},
		UpdatedAt:       MongoDate{raw: jsonRaw(t, `{"$numberLong":"1710529500000"}`)},
	}

	tr, err := TransformProject(rec)
	if err != nil {
		t.Fatalf("TransformProject: %v", err)
	}
	if tr.RejectReason != "" {
		t.Fatalf("unexpected rejection: %s", tr.RejectReason)
	}

	// Derived target ID — NOT the legacy ObjectId.
	if !hex32.MatchString(tr.Project.ID) {
		t.Errorf("target ID should be 32-hex, got %q", tr.Project.ID)
	}
	if tr.Project.ID == rec.ID.OID {
		t.Error("target ID must not be the legacy ObjectId")
	}

	// Timestamps preserved exactly (ordering must not break).
	if tr.Project.PublishedAtMS != 1710525600000 {
		t.Errorf("PublishedAtMS: got %d, want 1710525600000", tr.Project.PublishedAtMS)
	}
	if tr.Project.CreatedAtMS != 1710525900000 {
		t.Errorf("CreatedAtMS: got %d, want 1710525900000", tr.Project.CreatedAtMS)
	}
	if tr.Project.UpdatedAtMS != 1710529500000 {
		t.Errorf("UpdatedAtMS: got %d, want 1710529500000", tr.Project.UpdatedAtMS)
	}
	if tr.Project.Status != "published" {
		t.Errorf("Status: got %q, want published", tr.Project.Status)
	}
	if tr.Project.Slug != "full-record" || tr.Project.Description != "Desc" {
		t.Errorf("Slug/Description: got %q/%q", tr.Project.Slug, tr.Project.Description)
	}
	if tr.Project.UpdaterSourceID != "65a0b1c2d3e4f5a6b7c8d9e1" {
		t.Errorf("UpdaterSourceID: got %q", tr.Project.UpdaterSourceID)
	}

	// Every batch entry becomes one row, in array order, with the legacy
	// sub-ObjectId preserved as the row id.
	if len(tr.Project.Downloads) != 3 {
		t.Fatalf("downloads: got %d rows, want 3", len(tr.Project.Downloads))
	}
	if tr.Project.Downloads[0].ID != "65b0f1a2b3c4d5e6f7a8b9c1" || tr.Project.Downloads[0].Position != 0 {
		t.Errorf("download[0] = %+v, want preserved sub-ObjectId at position 0", tr.Project.Downloads[0])
	}
	if tr.Project.Downloads[0].MagnetLink == nil || *tr.Project.Downloads[0].MagnetLink != "magnet:?xt=urn:btih:aaa" ||
		tr.Project.Downloads[0].TorrentLink == nil || *tr.Project.Downloads[0].TorrentLink != "https://example.com/a.torrent" {
		t.Errorf("download[0] links = %+v", tr.Project.Downloads[0])
	}
	// Torrent-only entry: magnet NULL.
	if tr.Project.Downloads[1].MagnetLink != nil || tr.Project.Downloads[1].TorrentLink == nil {
		t.Errorf("download[1] = %+v, want NULL magnet + torrent", tr.Project.Downloads[1])
	}
	// Both-empty entry still becomes a row with NULL links (the batch NAME
	// is content worth preserving — the contract keeps the row).
	if tr.Project.Downloads[2].MagnetLink != nil || tr.Project.Downloads[2].TorrentLink != nil {
		t.Errorf("download[2] = %+v, want both links NULL", tr.Project.Downloads[2])
	}

	// Classification: magnet present(1)/absent(1)/empty(1); torrent
	// present(2)/empty(1).
	if tr.MagnetCounts[statePresent] != 1 || tr.MagnetCounts[stateAbsent] != 1 || tr.MagnetCounts[stateEmpty] != 1 {
		t.Errorf("magnet counts: got %v, want present=1 absent=1 empty=1", tr.MagnetCounts)
	}
	if tr.TorrentCounts[statePresent] != 2 || tr.TorrentCounts[stateEmpty] != 1 || tr.TorrentCounts[stateAbsent] != 0 {
		t.Errorf("torrent counts: got %v, want present=2 empty=1", tr.TorrentCounts)
	}
	if tr.EntriesSkipped != 0 {
		t.Errorf("EntriesSkipped: got %d, want 0", tr.EntriesSkipped)
	}
}

func TestTransformProject_MalformedPreservedWithWarning(t *testing.T) {
	t.Parallel()

	rec := LegacyProject{
		ID:        MongoObjectID{OID: "65d0d1a2b3c4d5e6f7a8b9e3"},
		Title:     "T",
		Thumbnail: "https://example.com/t.jpg",
		Slug:      "malformed",
		BatchDownloadLinks: []LegacyBatchDownload{
			{
				Name:                "Broken batch",
				DownloadLink:        new("not a valid link at all"),
				DownloadLinkTorrent: new(""),
			},
		},
		DateTimeCreated: "2024-01-20T09:30:00.000Z",
		CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-20T09:30:00.000Z"`)},
		UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-20T09:35:00.000Z"`)},
	}

	tr, err := TransformProject(rec)
	if err != nil {
		t.Fatalf("TransformProject: %v", err)
	}

	// Malformed values are PRESERVED as-is with a warning.
	if len(tr.Project.Downloads) != 1 {
		t.Fatalf("downloads: got %d rows, want 1", len(tr.Project.Downloads))
	}
	if tr.Project.Downloads[0].MagnetLink == nil || *tr.Project.Downloads[0].MagnetLink != "not a valid link at all" {
		t.Error("malformed magnet should be preserved as-is")
	}
	if tr.Project.Downloads[0].TorrentLink != nil {
		t.Error("empty torrent should map to NULL")
	}
	if tr.MagnetCounts[stateMalformed] != 1 || tr.TorrentCounts[stateEmpty] != 1 {
		t.Errorf("counts: want magnet malformed=1 torrent empty=1, got %v / %v", tr.MagnetCounts, tr.TorrentCounts)
	}
	if len(tr.Warnings) != 1 {
		t.Fatalf("warnings: got %d, want 1", len(tr.Warnings))
	}
	if tr.Warnings[0].Field != "batchDownloadLinks[0]" {
		t.Errorf("warning field: got %q", tr.Warnings[0].Field)
	}
}

func TestTransformProject_EntryWithoutUsableNameSkipped(t *testing.T) {
	t.Parallel()

	rec := LegacyProject{
		ID:        MongoObjectID{OID: "65d0d1a2b3c4d5e6f7a8b9e4"},
		Title:     "T",
		Thumbnail: "https://example.com/t.jpg",
		Slug:      "no-name",
		BatchDownloadLinks: []LegacyBatchDownload{
			{
				// No name at all: the target CHECK length(name) >= 1 cannot
				// hold — the entry is skipped with a warning, never a raw
				// CHECK abort of the whole import.
				DownloadLink: new("magnet:?xt=urn:btih:eee"),
			},
			{
				Name:                "Named entry",
				DownloadLinkTorrent: new("https://example.com/n.torrent"),
			},
		},
		DateTimeCreated: "2024-01-20T09:30:00.000Z",
		CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-20T09:30:00.000Z"`)},
		UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-20T09:30:00.000Z"`)},
	}

	tr, err := TransformProject(rec)
	if err != nil {
		t.Fatalf("TransformProject: %v", err)
	}
	if tr.EntriesSkipped != 1 {
		t.Errorf("EntriesSkipped: got %d, want 1", tr.EntriesSkipped)
	}
	// The surviving entry KEEPS its original array index (position 1) so
	// display order matches the legacy order.
	if len(tr.Project.Downloads) != 1 || tr.Project.Downloads[0].Position != 1 {
		t.Fatalf("downloads = %+v, want exactly the position-1 entry", tr.Project.Downloads)
	}
	if len(tr.Warnings) != 1 || tr.Warnings[0].Field != "batchDownloadLinks[0]" {
		t.Errorf("warnings = %+v, want one skipped-entry warning", tr.Warnings)
	}
	// The skipped entry's links are NOT classified — no row was produced.
	if tr.MagnetCounts[statePresent] != 0 || tr.TorrentCounts[statePresent] != 1 {
		t.Errorf("counts: got magnet %v torrent %v", tr.MagnetCounts, tr.TorrentCounts)
	}
}

// TestTransformProject_FreshDownloadIDs pins the absent-sub-ObjectId branch:
// an entry without a legacy `_id` receives a fresh 32-hex id, and two such
// entries never share one.
func TestTransformProject_FreshDownloadIDs(t *testing.T) {
	t.Parallel()

	rec := LegacyProject{
		ID:        MongoObjectID{OID: "65d0d1a2b3c4d5e6f7a8b9e6"},
		Title:     "T",
		Thumbnail: "https://example.com/t.jpg",
		Slug:      "fresh-ids",
		BatchDownloadLinks: []LegacyBatchDownload{
			{Name: "one", DownloadLink: new("magnet:?xt=urn:btih:aaa")},
			{Name: "two", DownloadLink: new("magnet:?xt=urn:btih:bbb")},
		},
		DateTimeCreated: "2024-01-20T09:30:00.000Z",
		CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-20T09:30:00.000Z"`)},
		UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-20T09:30:00.000Z"`)},
	}

	tr, err := TransformProject(rec)
	if err != nil {
		t.Fatalf("TransformProject: %v", err)
	}
	if len(tr.Project.Downloads) != 2 {
		t.Fatalf("downloads: got %d, want 2", len(tr.Project.Downloads))
	}
	seen := map[string]bool{}
	for _, dl := range tr.Project.Downloads {
		if !hex32.MatchString(dl.ID) {
			t.Errorf("fresh download id %q is not 32-hex", dl.ID)
		}
		if seen[dl.ID] {
			t.Errorf("duplicate download id %q", dl.ID)
		}
		seen[dl.ID] = true
	}
}

func TestTransformProject_TimestampFallbacks(t *testing.T) {
	t.Parallel()

	t.Run("missing dateTimeCreated falls back to createdAt", func(t *testing.T) {
		rec := LegacyProject{
			ID:        MongoObjectID{OID: "65e0d1a2b3c4d5e6f7a8b9e5"},
			Title:     "T",
			Thumbnail: "https://example.com/t.jpg",
			Slug:      "fallback-1",
			CreatedAt: MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
			UpdatedAt: MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
		}
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
		}
		if tr.Project.PublishedAtMS != tr.Project.CreatedAtMS {
			t.Errorf("published should fall back to created, got %d vs %d", tr.Project.PublishedAtMS, tr.Project.CreatedAtMS)
		}
		if len(tr.Warnings) != 1 || tr.Warnings[0].Field != "dateTimeCreated" {
			t.Errorf("TransformProject warnings = %v, want one dateTimeCreated warning", tr.Warnings)
		}
	})

	t.Run("unparseable dateTimeCreated falls back to createdAt", func(t *testing.T) {
		rec := LegacyProject{
			ID:              MongoObjectID{OID: "65e0d1a2b3c4d5e6f7a8b9e6"},
			Title:           "T",
			Thumbnail:       "https://example.com/t.jpg",
			Slug:            "fallback-2",
			DateTimeCreated: "not-a-date",
			CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
			UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
		}
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
		}
		if tr.Project.PublishedAtMS != tr.Project.CreatedAtMS {
			t.Error("published should fall back to created")
		}
	})

	t.Run("missing updatedAt falls back to createdAt", func(t *testing.T) {
		rec := LegacyProject{
			ID:              MongoObjectID{OID: "65e0d1a2b3c4d5e6f7a8b9e7"},
			Title:           "T",
			Thumbnail:       "https://example.com/t.jpg",
			Slug:            "fallback-3",
			DateTimeCreated: "2024-02-10T12:00:00.000Z",
			CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-02-10T12:00:00.000Z"`)},
			// UpdatedAt absent.
		}
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
		}
		if tr.Project.UpdatedAtMS != tr.Project.CreatedAtMS {
			t.Error("updated should fall back to created")
		}
		if len(tr.Warnings) != 1 || tr.Warnings[0].Field != "updatedAt" {
			t.Errorf("TransformProject warnings = %v, want one updatedAt warning", tr.Warnings)
		}
	})
}

func TestTransformProject_MissingCreatedAtRejects(t *testing.T) {
	t.Parallel()

	// No createdAt AND no dateTimeCreated: the created_at_ms fallback chain
	// is exhausted — reject, never fabricate.
	rec := LegacyProject{
		ID:        MongoObjectID{OID: "65f0d1a2b3c4d5e6f7a8b9e8"},
		Title:     "T",
		Thumbnail: "https://example.com/t.jpg",
		Slug:      "no-created",
	}

	tr, err := TransformProject(rec)
	if err != nil {
		t.Fatalf("TransformProject: %v", err)
	}
	if tr.RejectReason == "" {
		t.Fatal("expected rejection when createdAt AND dateTimeCreated are both unusable — created_at_ms must never be fabricated")
	}
}

// TestTransformProject_CreatedAtFallsBackToDateTimeCreated pins the real
// export shape: legacy projects carry NO
// createdAt field, so created_at_ms derives from dateTimeCreated with a
// warning.
func TestTransformProject_CreatedAtFallsBackToDateTimeCreated(t *testing.T) {
	t.Parallel()

	rec := LegacyProject{
		ID:              MongoObjectID{OID: "65f0d1a2b3c4d5e6f7a8b9e7"},
		Title:           "T",
		Thumbnail:       "https://example.com/t.jpg",
		Slug:            "no-created-at",
		DateTimeCreated: "2024-02-10T12:00:00.000Z",
		UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-03-10T12:00:00.000Z"`)},
		// createdAt absent — the observed production shape.
	}

	tr, err := TransformProject(rec)
	if err != nil {
		t.Fatalf("TransformProject: %v", err)
	}
	if tr.RejectReason != "" {
		t.Fatalf("unexpected rejection: %s", tr.RejectReason)
	}
	wantMS := int64(1707566400000) // 2024-02-10T12:00:00.000Z
	if tr.Project.CreatedAtMS != wantMS {
		t.Errorf("created_at_ms = %d, want dateTimeCreated-derived %d", tr.Project.CreatedAtMS, wantMS)
	}
	if tr.Project.PublishedAtMS != wantMS {
		t.Errorf("published_at_ms = %d, want %d", tr.Project.PublishedAtMS, wantMS)
	}
	if len(tr.Warnings) != 1 || tr.Warnings[0].Field != "createdAt" {
		t.Errorf("TransformProject warnings = %v, want one createdAt warning", tr.Warnings)
	}
}

func TestTransformProject_TargetConstraintViolationsReject(t *testing.T) {
	t.Parallel()

	base := LegacyProject{
		ID:              MongoObjectID{OID: "65f0d1a2b3c4d5e6f7a8b9e9"},
		Title:           "T",
		Thumbnail:       "https://example.com/t.jpg",
		Slug:            "constraints",
		DateTimeCreated: "2024-01-10T10:00:00.000Z",
		CreatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
		UpdatedAt:       MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
	}

	t.Run("empty title", func(t *testing.T) {
		rec := base
		rec.Title = "  "
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
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
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for a missing _id")
		}
	})

	t.Run("empty thumbnail", func(t *testing.T) {
		rec := base
		rec.Thumbnail = ""
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for an empty thumbnail")
		}
	})

	t.Run("empty slug", func(t *testing.T) {
		rec := base
		rec.Slug = "  "
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for an empty slug (slug is NOT NULL with length >= 1)")
		}
	})

	t.Run("epoch-zero createdAt", func(t *testing.T) {
		// A parseable but non-positive instant passes Millis() yet
		// violates created_at_ms > 0 at INSERT — must reject per-record,
		// never abort the whole import with a raw CHECK error.
		rec := base
		rec.CreatedAt = MongoDate{raw: jsonRaw(t, `{"$numberLong":"0"}`)}
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
		}
		if tr.RejectReason == "" {
			t.Fatal("expected rejection for a non-positive createdAt")
		}
	})

	t.Run("updatedAt precedes createdAt", func(t *testing.T) {
		rec := base
		rec.UpdatedAt = MongoDate{raw: jsonRaw(t, `"2024-01-01T10:00:00.000Z"`)}
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
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
		tr, err := TransformProject(rec)
		if err != nil {
			t.Fatalf("TransformProject: %v", err)
		}
		if tr.RejectReason != "" {
			t.Fatalf("unexpected rejection: %s", tr.RejectReason)
		}
		if tr.Project.PublishedAtMS != tr.Project.CreatedAtMS {
			t.Fatalf("published_at_ms = %d, want fallback to created_at_ms %d", tr.Project.PublishedAtMS, tr.Project.CreatedAtMS)
		}
		if len(tr.Warnings) == 0 {
			t.Fatal("expected a fallback warning for the non-positive dateTimeCreated")
		}
	})
}
