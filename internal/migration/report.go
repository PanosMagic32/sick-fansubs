package migration

// ReconciliationReport is the migration's measurable output ("A
// total-count match alone is insufficient"). It carries counts, source
// IDs, and concise reasons — never full records or sensitive values.
//
// The shape is one JSON document with the sections
// below.
type ReconciliationReport struct {
	Source struct {
		BlogPosts int `json:"blogPosts"`
		Projects  int `json:"projects"`
		Users     int `json:"users"`
		Imported  int `json:"imported"`
		Rejected  int `json:"rejected"`
	} `json:"source"`

	// Identifiers maps every source ID to its target ID. Content rows receive
	// DERIVED 32-hex target IDs (hex(HMAC-SHA256(key, legacy ObjectId))[:32] —
	// legacy ObjectIds are not imported as content target IDs; the mapping is
	// the audit trail); user rows PRESERVE the legacy ObjectId, so their
	// entries are identity pairs.
	Identifiers struct {
		SourceToTarget []IDMapping `json:"sourceToTarget"`
	} `json:"identifiers"`

	// Downloads classifies the four legacy download fields per resolution:
	// present/empty/absent/malformed plus the number of rows actually written.
	Downloads map[string]ResolutionCounts `json:"downloads"`

	// ProjectDownloads classifies the project batch-download links: each
	// entry's magnet and torrent link is counted
	// present/empty/absent/malformed, plus the number of rows written and
	// entries dropped for a missing/empty name. Struct (not pointer) so the
	// zeroed section always marshals — empty sections must never be null.
	ProjectDownloads struct {
		Rows           int               `json:"rows"`
		EntriesDropped int               `json:"entriesDropped"`
		Magnet         ProjectLinkCounts `json:"magnet"`
		Torrent        ProjectLinkCounts `json:"torrent"`
	} `json:"projectDownloads"`

	// Credentials classifies migrated password verifiers — counts and
	// prefixes only, never the verifier strings themselves. Struct
	// (not pointer) so the section always marshals.
	Credentials struct {
		Supported      int            `json:"supported"`      // $2a$/$2b$/$2y$, cost 4..13
		RehashOnSignIn int            `json:"rehashOnSignIn"` // subset of supported: cost < 12
		AboveMaxCost   int            `json:"aboveMaxCost"`   // $2a$/$2b$/$2y$, cost 14..31
		Unsupported    int            `json:"unsupported"`    // other prefixes, unparseable/out-of-range cost
		ByPrefix       map[string]int `json:"byPrefix"`       // every observed prefix → count (inventory)
	} `json:"credentials"`

	// AvatarsDeferred counts legacy absolute avatar URLs skipped (stored
	// NULL) — avatar media migration belongs to the media import.
	AvatarsDeferred int `json:"avatarsDeferred"`

	// SpecialCharacters counts legacy usernames/emails outside the target
	// registration charsets (imported as-is, inventoried for manual
	// resolution — never rejected on charset).
	SpecialCharacters struct {
		Usernames int `json:"usernames"`
		Emails    int `json:"emails"`
	} `json:"specialCharacters"`

	// ExcludedRefreshTokenFields counts legacy records that carried excluded
	// refresh-token material (an aggregate count only; the values are never
	// copied into the target or the report).
	ExcludedRefreshTokenFields int `json:"excludedRefreshTokenFields"`

	// TargetSessions is always 0: the user import creates no session rows
	// (sessions originate only from post-cutover sign-ins). The
	// field exists so the report attests the invariant explicitly.
	TargetSessions int `json:"targetSessions"`

	// Favorites reconciles the legacy favorite arrays:
	// per-kind counts of source rows, written join rows, and skip reasons.
	// Struct (not pointer) so both kinds always marshal — the same convention
	// as every other section (empty sections are zeroed objects, never null).
	// The favorites kind deliberately leaves the Source section zeroed: it
	// imports no records of the kinds Source counts; the favorites section
	// below IS its source/target reconciliation.
	Favorites struct {
		Blog     FavoritesCounts `json:"blog"`
		Projects FavoritesCounts `json:"projects"`
	} `json:"favorites"`

	// Warnings carries non-fatal mapping problems: timestamp fallbacks and
	// malformed download values preserved as-is.
	Warnings []MigrationWarning `json:"warnings"`

	Relationships struct {
		CreatorResolved int `json:"creatorResolved"`
		CreatorOrphans  int `json:"creatorOrphans"`
		UpdaterResolved int `json:"updaterResolved"`
		UpdaterOrphans  int `json:"updaterOrphans"`
	} `json:"relationships"`

	RejectedRecords []RejectedRecord `json:"rejectedRecords"`
}

// IDMapping is one source→target identifier pair.
type IDMapping struct {
	SourceID string `json:"sourceId"`
	TargetID string `json:"targetId"`
}

// FavoritesCounts reconciles one content kind's legacy favorites: every
// array entry is either imported, already present
// (an in-array duplicate or a previous run), or skipped with a reason.
// Users/SourceRows are SOURCE-level — counted from the arrays before any
// resolution, so a missing user's entries still appear in the source counts
// and their outcome in the skip reasons.
type FavoritesCounts struct {
	Users          int `json:"users"`          // users carrying a non-empty array
	SourceRows     int `json:"sourceRows"`     // entries across those arrays
	Imported       int `json:"imported"`       // join rows written
	AlreadyPresent int `json:"alreadyPresent"` // join rows that already existed
	UserMissing    int `json:"userMissing"`    // entries whose user is not in the target users table
	ContentMissing int `json:"contentMissing"` // entries whose derived content id is not in the target table
}

// ResolutionCounts is the per-resolution download classification — the
// report must include counts for each category.
type ResolutionCounts struct {
	Present   int `json:"present"`
	Empty     int `json:"empty"`
	Absent    int `json:"absent"`
	Malformed int `json:"malformed"`
	Rows      int `json:"rows"`
}

// ProjectLinkCounts is the per-link-kind classification for project batch
// downloads — the same four categories as ResolutionCounts, applied to the
// magnet and torrent links separately; row counts live on the
// ProjectDownloads section because one row carries both links.
type ProjectLinkCounts struct {
	Present   int `json:"present"`
	Empty     int `json:"empty"`
	Absent    int `json:"absent"`
	Malformed int `json:"malformed"`
}

// RejectedRecord reports a skipped source record with a concise reason.
type RejectedRecord struct {
	SourceID string `json:"sourceId"`
	Reason   string `json:"reason"`
}

// newReport returns a zeroed report with both resolution buckets present and
// every slice initialized to [] (not nil) so the JSON output is stable —
// empty sections marshal as [] instead of null.
func newReport() *ReconciliationReport {
	r := &ReconciliationReport{}
	r.Downloads = map[string]ResolutionCounts{
		Resolution1080p: {},
		Resolution2160p: {},
	}
	r.Credentials.ByPrefix = map[string]int{}
	r.Identifiers.SourceToTarget = []IDMapping{}
	r.Warnings = []MigrationWarning{}
	r.RejectedRecords = []RejectedRecord{}
	return r
}

// accumulateDownloadCounts merges one record's classification counts into
// the report.
func (r *ReconciliationReport) accumulateDownloadCounts(counts map[string]map[downloadState]int) {
	for resolution, states := range counts {
		res := r.Downloads[resolution]
		res.Present += states[statePresent]
		res.Empty += states[stateEmpty]
		res.Absent += states[stateAbsent]
		res.Malformed += states[stateMalformed]
		r.Downloads[resolution] = res
	}
}

// accumulateUserCounts merges one user record's verifier classification and
// deferred-avatar flag into the report. Like the project accumulator,
// classifications are SOURCE-level: they count every record,
// including records later rejected for other reasons, so the report
// accounts for all source data.
func (r *ReconciliationReport) accumulateUserCounts(tr TransformUserResult) {
	if tr.VerifierPrefix != "" {
		r.Credentials.ByPrefix[tr.VerifierPrefix]++
	}
	switch tr.VerifierCategory {
	case categorySupported:
		r.Credentials.Supported++
		if tr.VerifierCost < verifierTargetCost {
			r.Credentials.RehashOnSignIn++
		}
	case categoryAboveMax:
		r.Credentials.AboveMaxCost++
	default:
		r.Credentials.Unsupported++
	}
	if tr.AvatarDeferred {
		r.AvatarsDeferred++
	}
	if tr.UsernameNonConforming {
		r.SpecialCharacters.Usernames++
	}
	if tr.EmailNonConforming {
		r.SpecialCharacters.Emails++
	}
	if tr.HasRefreshTokenMaterial {
		r.ExcludedRefreshTokenFields++
	}
}

// accumulateProjectCounts merges one project record's link classifications
// and dropped entries into the report.
//
// Classifications are SOURCE-level: they count every record, including
// records later rejected (duplicate slug, constraint violations), so the
// report accounts for all source data. Rows and Imported are TARGET-level
// — what was actually written. The same split applies to the blog section.
func (r *ReconciliationReport) accumulateProjectCounts(tr TransformProjectResult) {
	r.ProjectDownloads.EntriesDropped += tr.EntriesSkipped
	m := &r.ProjectDownloads.Magnet
	m.Present += tr.MagnetCounts[statePresent]
	m.Empty += tr.MagnetCounts[stateEmpty]
	m.Absent += tr.MagnetCounts[stateAbsent]
	m.Malformed += tr.MagnetCounts[stateMalformed]
	t := &r.ProjectDownloads.Torrent
	t.Present += tr.TorrentCounts[statePresent]
	t.Empty += tr.TorrentCounts[stateEmpty]
	t.Absent += tr.TorrentCounts[stateAbsent]
	t.Malformed += tr.TorrentCounts[stateMalformed]
}
