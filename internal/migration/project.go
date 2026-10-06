package migration

import (
	"fmt"
	"strings"

	"sick-fansubs/internal/id"
)

// LegacyProject mirrors the legacy MongoDB export shape for projects. It
// exists ONLY as the migration source contract — never a target runtime
// type.
//
// The legacy record has no `creator` field (creator_id stays NULL for
// migrated projects — the field is deliberately not decoded so no code path
// can accidentally map it). `updatedBy` is the ObjectId reference to the
// last editor.
type LegacyProject struct {
	ID          MongoObjectID `json:"_id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Thumbnail   string        `json:"thumbnail"`
	Slug        string        `json:"slug"`

	// BatchDownloadLinks is the embedded array of named batch entries. Each
	// entry may carry its own `_id` (a legacy sub-ObjectId, preserved as the
	// download-row id).
	BatchDownloadLinks []LegacyBatchDownload `json:"batchDownloadLinks"`

	// DateTimeCreated is the human-oriented ISO 8601 publication instant.
	DateTimeCreated string         `json:"dateTimeCreated"`
	UpdatedBy       *MongoObjectID `json:"updatedBy"`

	CreatedAt MongoDate `json:"createdAt"`
	UpdatedAt MongoDate `json:"updatedAt"`

	// __v (Mongoose version counter) is deliberately ignored — migration
	// evidence, not content.
}

// LegacyBatchDownload is one `batchDownloadLinks` entry. The two link
// fields are pointers so the transform can distinguish the four
// classification states the mapping requires: absent (nil), empty (""),
// malformed (non-empty, not a URL/magnet), and present (valid).
type LegacyBatchDownload struct {
	ID                  *MongoObjectID `json:"_id"`
	Name                string         `json:"name"`
	DownloadLink        *string        `json:"downloadLink"`
	DownloadLinkTorrent *string        `json:"downloadLinkTorrent"`
}

// ImportProject is the target row shape produced by the transform.
type ImportProject struct {
	ID              string // derived 32-hex target ID
	Title           string
	Description     string
	Slug            string
	ThumbnailURL    string
	Status          string
	UpdaterSourceID string // legacy updatedBy ObjectId, "" when absent
	PublishedAtMS   int64
	CreatedAtMS     int64
	UpdatedAtMS     int64

	// Downloads, in display order (position = original array index).
	Downloads []ImportProjectDownload
}

// ImportProjectDownload is one normalized project_downloads row. The legacy
// sub-ObjectId is preserved as the row id; entries without one receive a
// fresh 32-hex id at transform time.
type ImportProjectDownload struct {
	ID          string
	Name        string
	MagnetLink  *string
	TorrentLink *string
	Position    int
}

// TransformProjectResult is the outcome of mapping one legacy record.
type TransformProjectResult struct {
	Project      ImportProject
	Warnings     []MigrationWarning
	RejectReason string
	// MagnetCounts/TorrentCounts classify the two link kinds across the
	// record's batch entries — the report carries counts per classification
	// category.
	MagnetCounts  map[downloadState]int
	TorrentCounts map[downloadState]int
	// EntriesSkipped counts batch entries dropped for a missing/empty name
	// (the target CHECK length(name) >= 1 could not hold).
	EntriesSkipped int
}

// newStateCounts returns a zeroed classification counter per link kind.
func newStateCounts() map[downloadState]int {
	return map[downloadState]int{stateAbsent: 0, stateEmpty: 0, stateMalformed: 0, statePresent: 0}
}

// TransformProject maps one legacy record to the target shape:
// dateTimeCreated → published_at_ms, createdAt → created_at_ms,
// updatedAt → updated_at_ms. The fallback chains, the per-record rejection
// policy, and the target CHECK pre-validation are the contract in
// internal/migration/AGENTS.md.
func TransformProject(rec LegacyProject) (TransformProjectResult, error) {
	res := TransformProjectResult{
		MagnetCounts:  newStateCounts(),
		TorrentCounts: newStateCounts(),
	}

	// The target id DERIVES from the legacy ObjectId — a missing _id rejects
	// per-record (never a raw failure of the import).
	if rec.ID.OID == "" {
		res.RejectReason = "missing _id — the target id derives from the legacy ObjectId"
		return res, nil
	}
	projectID := deriveTargetID(rec.ID.OID)

	// Target CHECK constraints are part of the mapping contract:
	// emit a rejection item when data cannot map safely. A record that
	// would violate a NOT NULL CHECK is rejected here — with a concise
	// reason — instead of aborting the whole import with a raw CHECK error
	// later. The slug CHECK is part of the projects contract (unique,
	// case-sensitive, length >= 1).
	if strings.TrimSpace(rec.Title) == "" {
		res.RejectReason = "title is empty — violates the target NOT NULL/CHECK contract"
		return res, nil
	}
	if strings.TrimSpace(rec.Thumbnail) == "" {
		res.RejectReason = "thumbnail is empty — violates the target NOT NULL/CHECK contract"
		return res, nil
	}
	if strings.TrimSpace(rec.Slug) == "" {
		res.RejectReason = "slug is empty — violates the target NOT NULL/CHECK contract"
		return res, nil
	}

	// published_at_ms source first: dateTimeCreated is the human-oriented
	// publication instant and doubles as the created_at_ms fallback below.
	publishedMS, pubOK := parseISO8601(rec.DateTimeCreated)

	// created_at_ms is mandatory — derive, never fabricate. The legacy
	// project export carries no createdAt (see internal/migration/AGENTS.md),
	// so the fallback chain is createdAt → dateTimeCreated. Reject only when
	// BOTH are unusable.
	createdMS, createdOK := rec.CreatedAt.Millis()
	if !createdOK {
		if !pubOK {
			res.RejectReason = "createdAt and dateTimeCreated both missing or unparseable — cannot map created_at_ms"
			return res, nil
		}
		createdMS = publishedMS
		res.Warnings = append(res.Warnings, MigrationWarning{
			SourceID: rec.ID.OID,
			Field:    "createdAt",
			Detail:   "missing or unparseable; derived created_at_ms from dateTimeCreated",
		})
	}
	// The numeric CHECKs are pre-validated too: reject
	// per-record, never a raw CHECK abort of the whole import. A
	// parseable but non-positive instant (e.g. epoch 0) passes the parsers
	// yet violates created_at_ms > 0 at INSERT.
	if createdMS <= 0 {
		res.RejectReason = "createdAt is not a positive instant — violates the target CHECK contract"
		return res, nil
	}
	updatedMS, ok := rec.UpdatedAt.Millis()
	if !ok {
		// Fall back to created_at_ms with a warning; the CHECK
		// (updated_at_ms >= created_at_ms) stays satisfied.
		updatedMS = createdMS
		res.Warnings = append(res.Warnings, MigrationWarning{
			SourceID: rec.ID.OID,
			Field:    "updatedAt",
			Detail:   "missing or unparseable; fell back to createdAt",
		})
	} else if updatedMS < createdMS {
		// A present-but-earlier updatedAt violates updated_at_ms >=
		// created_at_ms. Reject per-record — never clamp silently, the
		// source data is what it is.
		res.RejectReason = "updatedAt precedes createdAt — violates the target CHECK contract"
		return res, nil
	}

	// published_at_ms: the public ordering key. Preserve the legacy instant
	// exactly — the legacy ordering must not break; missing, unparseable, or
	// non-positive falls back to created_at_ms with a warning — a 0 would
	// violate published_at_ms > 0 at INSERT and silently demote visible
	// content to a draft.
	if !pubOK || publishedMS <= 0 {
		publishedMS = createdMS
		res.Warnings = append(res.Warnings, MigrationWarning{
			SourceID: rec.ID.OID,
			Field:    "dateTimeCreated",
			Detail:   "missing, unparseable, or not a positive instant; fell back to created_at_ms",
		})
	}

	res.Project = ImportProject{
		ID:            projectID,
		Title:         rec.Title,
		Description:   rec.Description,
		Slug:          rec.Slug,
		ThumbnailURL:  rec.Thumbnail,
		Status:        "published", // legacy projects were publicly visible
		PublishedAtMS: publishedMS,
		CreatedAtMS:   createdMS,
		UpdatedAtMS:   updatedMS,
	}
	if rec.UpdatedBy != nil {
		res.Project.UpdaterSourceID = rec.UpdatedBy.OID
	}

	for i, entry := range rec.BatchDownloadLinks {
		// A row's name must satisfy length(name) >= 1. An entry without a
		// usable name is skipped with a warning — never a raw CHECK abort of
		// the whole import. Position keeps the ORIGINAL array index so the
		// display order of the surviving entries is preserved.
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			res.EntriesSkipped++
			res.Warnings = append(res.Warnings, MigrationWarning{
				SourceID: rec.ID.OID,
				Field:    fmt.Sprintf("batchDownloadLinks[%d]", i),
				Detail:   "entry skipped — empty or missing name violates the target NOT NULL/CHECK contract",
			})
			continue
		}

		ms, ts := downloadClass(entry.DownloadLink), downloadClass(entry.DownloadLinkTorrent)
		res.MagnetCounts[ms]++
		res.TorrentCounts[ts]++

		// Every valid entry becomes one row, even when both links are
		// absent/empty: empty-string links become NULL and the batch NAME is
		// still content worth preserving.
		row := ImportProjectDownload{
			Name:        name,
			MagnetLink:  copyLink(entry.DownloadLink, ms),
			TorrentLink: copyLink(entry.DownloadLinkTorrent, ts),
			Position:    i,
		}
		// The legacy sub-ObjectId is preserved as the row id;
		// entries without one receive a fresh 32-hex id.
		if entry.ID != nil && entry.ID.OID != "" {
			row.ID = entry.ID.OID
		} else {
			generatedID, err := id.New()
			if err != nil {
				return TransformProjectResult{}, fmt.Errorf("migration: random id: %w", err)
			}
			row.ID = generatedID
		}
		if ms == stateMalformed || ts == stateMalformed {
			res.Warnings = append(res.Warnings, MigrationWarning{
				SourceID: rec.ID.OID,
				Field:    fmt.Sprintf("batchDownloadLinks[%d]", i),
				Detail:   "malformed value preserved as-is with a warning",
			})
		}
		res.Project.Downloads = append(res.Project.Downloads, row)
	}

	return res, nil
}
