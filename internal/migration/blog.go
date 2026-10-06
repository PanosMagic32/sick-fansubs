// Package migration transforms legacy MongoDB-export records into the
// target SQLite schema and emits a reconciliation report.
//
// This is migration tooling, not the runtime store: the runtime boundary
// (internal/store) owns the serving path; this package owns the one-time
// source→target transform. The mapping rules come from the content-schema
// contract (posts, projects, and their downloads) — data safety: counts,
// source IDs, and concise reasons in reports, never full records or
// sensitive values.
//
// Migrated content rows receive DERIVED 32-hex target IDs —
// hex(HMAC-SHA256(key, legacy ObjectId))[:32]; the legacy content ObjectId
// survives in the reconciliation mapping. Legacy timestamps are preserved so
// the public ordering (published_at_ms DESC) does not break.
package migration

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LegacyBlogPost mirrors the legacy MongoDB export shape for blog posts.
// It exists ONLY as the migration source contract — never a target runtime
// type.
//
// Download fields are pointers so the transform can distinguish the four
// classification states the mapping requires: absent (nil), empty (""),
// malformed (non-empty, not a URL/magnet), and present (valid).
type LegacyBlogPost struct {
	ID          MongoObjectID `json:"_id"`
	Title       string        `json:"title"`
	Subtitle    string        `json:"subtitle"`
	Description string        `json:"description"`
	Thumbnail   string        `json:"thumbnail"`

	DownloadLink          *string `json:"downloadLink"`
	DownloadLinkTorrent   *string `json:"downloadLinkTorrent"`
	DownloadLink4k        *string `json:"downloadLink4k"`
	DownloadLink4kTorrent *string `json:"downloadLink4kTorrent"`

	// DateTimeCreated is the human-oriented ISO 8601 publication instant.
	DateTimeCreated string `json:"dateTimeCreated"`
	// Creator/Updater are ObjectId references to users (may be absent).
	// The real export carries BOTH on blog posts — projects carry updatedBy
	// only.
	Creator   *MongoObjectID `json:"creator"`
	UpdatedBy *MongoObjectID `json:"updatedBy"`

	CreatedAt MongoDate `json:"createdAt"`
	UpdatedAt MongoDate `json:"updatedAt"`

	// __v (Mongoose version counter) is deliberately ignored — migration
	// evidence, not content.
}

// MongoObjectID decodes the {"$oid": "..."} export wrapper.
type MongoObjectID struct {
	OID string `json:"$oid"`
}

// MongoDate decodes the {"$date": ...} export wrapper. The value is either
// an ISO 8601 string or {"$numberLong": "<millis>"} — both appear in
// Mongoose-managed exports.
type MongoDate struct {
	raw json.RawMessage
}

// UnmarshalJSON keeps the raw inner $date value for Millis().
func (d *MongoDate) UnmarshalJSON(b []byte) error {
	var wrapper struct {
		Date json.RawMessage `json:"$date"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return err
	}
	d.raw = wrapper.Date
	return nil
}

// Millis returns the UTC Unix-millisecond instant, or false when the value
// is missing or unparseable. The transform treats a missing/unparseable date
// deliberately (reject for created_at, fall back for the others).
func (d MongoDate) Millis() (int64, bool) {
	if len(d.raw) == 0 {
		return 0, false
	}
	var s string
	if err := json.Unmarshal(d.raw, &s); err == nil {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return 0, false
		}
		return t.UnixMilli(), true
	}
	var nl struct {
		NumberLong string `json:"$numberLong"`
	}
	if err := json.Unmarshal(d.raw, &nl); err == nil && nl.NumberLong != "" {
		ms, err := strconv.ParseInt(nl.NumberLong, 10, 64)
		if err != nil {
			return 0, false
		}
		return ms, true
	}
	return 0, false
}

// downloadState classifies one legacy download field.
type downloadState int

const (
	stateAbsent downloadState = iota
	stateEmpty
	stateMalformed
	statePresent
)

// downloadClass classifies one legacy download field.
func downloadClass(v *string) downloadState {
	if v == nil {
		return stateAbsent
	}
	if *v == "" {
		return stateEmpty
	}
	if !validLink(*v) {
		return stateMalformed
	}
	return statePresent
}

// validLink reports whether a legacy download value is a plausible URL or
// magnet link. The check is deliberately shallow — the migration preserves
// malformed values as-is with a warning rather than silently discarding or
// repairing content.
func validLink(v string) bool {
	u, err := url.Parse(v)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "magnet":
		return true
	default:
		return false
	}
}

// Resolution labels for blog downloads. Declared once — the transform, the
// counts map, and the report all share these.
const (
	Resolution1080p = "1080p"
	Resolution2160p = "2160p"
)

type ImportBlogPost struct {
	ID              string // derived 32-hex target ID
	Title           string
	Subtitle        string
	Description     string
	ThumbnailURL    string
	Status          string
	CreatorSourceID string // legacy creator ObjectId, "" when absent
	UpdaterSourceID string // legacy updatedBy ObjectId, "" when absent
	PublishedAtMS   int64
	CreatedAtMS     int64
	UpdatedAtMS     int64

	// Downloads, in display order (1080p position 0, 2160p position 1).
	Downloads []ImportDownload
}

// ImportDownload is one normalized blog_post_downloads row.
type ImportDownload struct {
	Resolution  string
	MagnetLink  *string
	TorrentLink *string
	Position    int
}

// MigrationWarning records a non-fatal mapping problem — timestamp
// fallbacks and malformed download values preserved as-is. The name is
// deliberately general: warnings span more than timestamps.
type MigrationWarning struct {
	SourceID string `json:"sourceId"`
	Field    string `json:"field"`
	Detail   string `json:"detail"`
}

// TransformResult is the outcome of mapping one legacy record.
type TransformResult struct {
	Post     ImportBlogPost
	Warnings []MigrationWarning
	// RejectReason is non-empty when the record cannot map safely — the
	// record is skipped and reported, never half-imported.
	RejectReason string
	// DownloadCounts classifies the download fields per resolution.
	DownloadCounts map[string]map[downloadState]int
}

// TransformBlogPost maps one legacy record to the target shape:
// dateTimeCreated → published_at_ms, createdAt → created_at_ms,
// updatedAt → updated_at_ms. The fallback chains, the per-record rejection
// policy, and the target CHECK pre-validation are the contract in
// internal/migration/AGENTS.md.
func TransformBlogPost(rec LegacyBlogPost) (TransformResult, error) {
	counts := map[string]map[downloadState]int{
		Resolution1080p: {stateAbsent: 0, stateEmpty: 0, stateMalformed: 0, statePresent: 0},
		Resolution2160p: {stateAbsent: 0, stateEmpty: 0, stateMalformed: 0, statePresent: 0},
	}
	res := TransformResult{DownloadCounts: counts}

	// The target id DERIVES from the legacy ObjectId — a missing _id rejects
	// per-record (never a raw failure of the import).
	if rec.ID.OID == "" {
		res.RejectReason = "missing _id — the target id derives from the legacy ObjectId"
		return res, nil
	}
	id := deriveTargetID(rec.ID.OID)

	// Target CHECK constraints are part of the mapping contract:
	// emit a rejection item when data cannot map safely. A record that
	// would violate a NOT NULL CHECK is rejected here — with a concise
	// reason — instead of aborting the whole import with a raw CHECK error
	// later.
	if strings.TrimSpace(rec.Title) == "" {
		res.RejectReason = "title is empty — violates the target NOT NULL/CHECK contract"
		return res, nil
	}
	if strings.TrimSpace(rec.Thumbnail) == "" {
		res.RejectReason = "thumbnail is empty — violates the target NOT NULL/CHECK contract"
		return res, nil
	}

	// published_at_ms source first: dateTimeCreated is the human-oriented
	// publication instant and doubles as the created_at_ms fallback below
	// (the project-transform precedent).
	publishedMS, pubOK := parseISO8601(rec.DateTimeCreated)

	// created_at_ms is mandatory — derive, never fabricate. The fallback
	// chain is createdAt → dateTimeCreated (most legacy blog posts carry no
	// createdAt — see internal/migration/AGENTS.md). Reject only when BOTH
	// are unusable; a non-positive derived value is caught by the
	// createdMS <= 0 guard below (the project-transform structure).
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
	// parseable but non-positive instant (e.g. epoch 0) passes Millis()
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
			Detail:   "missing, unparseable, or not a positive instant; fell back to createdAt",
		})
	}

	res.Post = ImportBlogPost{
		ID:            id,
		Title:         rec.Title,
		Subtitle:      rec.Subtitle,
		Description:   rec.Description,
		ThumbnailURL:  rec.Thumbnail,
		Status:        "published", // legacy posts were publicly visible
		PublishedAtMS: publishedMS,
		CreatedAtMS:   createdMS,
		UpdatedAtMS:   updatedMS,
	}
	if rec.Creator != nil {
		res.Post.CreatorSourceID = rec.Creator.OID
	}
	if rec.UpdatedBy != nil {
		res.Post.UpdaterSourceID = rec.UpdatedBy.OID
	}

	appendResolution := func(resolution string, magnet, torrent *string, position int) {
		ms, ts := downloadClass(magnet), downloadClass(torrent)
		counts[resolution][ms]++
		counts[resolution][ts]++
		if ms == statePresent || ms == stateMalformed || ts == statePresent || ts == stateMalformed {
			row := ImportDownload{Resolution: resolution, Position: position,
				MagnetLink:  copyLink(magnet, ms),
				TorrentLink: copyLink(torrent, ts)}
			res.Post.Downloads = append(res.Post.Downloads, row)
			if ms == stateMalformed || ts == stateMalformed {
				res.Warnings = append(res.Warnings, MigrationWarning{
					SourceID: rec.ID.OID,
					Field:    resolution + " download",
					Detail:   "malformed value preserved as-is with a warning",
				})
			}
		}
	}
	appendResolution(Resolution1080p, rec.DownloadLink, rec.DownloadLinkTorrent, 0)
	appendResolution(Resolution2160p, rec.DownloadLink4k, rec.DownloadLink4kTorrent, 1)

	return res, nil
}

// copyLink returns the pointer for a row column: present and malformed
// values are preserved as-is; absent/empty produce NULL (no column value).
func copyLink(v *string, state downloadState) *string {
	if state == statePresent || state == stateMalformed {
		s := *v
		return &s
	}
	return nil
}

// parseISO8601 parses the legacy human-oriented timestamp. Only ISO 8601
// strings are expected.
func parseISO8601(s string) (int64, bool) {
	if strings.TrimSpace(s) == "" {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0, false
	}
	return t.UnixMilli(), true
}
