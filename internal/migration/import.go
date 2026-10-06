package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"sick-fansubs/internal/id"
	"sick-fansubs/internal/store"
)

// ImportBlogPosts transforms and inserts every legacy blog record into the
// target database and returns the reconciliation report.
//
// Each record imports in its own transaction — a transaction never spans
// records — and stops on the first unexplained database error. Rejected
// records are reported and skipped, never half-imported. Callers run the
// strict migration-history verification and foreign-key checks around this
// (the migrate command does it after import; docs/patterns/go/sqlite.md).
func ImportBlogPosts(ctx context.Context, db *sql.DB, records []LegacyBlogPost) (*ReconciliationReport, error) {
	report := newReport()
	report.Source.BlogPosts = len(records)

	for _, rec := range records {
		if err := importOne(ctx, db, rec, report); err != nil {
			return report, fmt.Errorf("migration: record %q: %w", rec.ID.OID, err)
		}
	}
	return report, nil
}

// importOne maps and inserts one record.
func importOne(ctx context.Context, db *sql.DB, rec LegacyBlogPost, report *ReconciliationReport) error {
	tr, err := TransformBlogPost(rec)
	if err != nil {
		return err
	}
	report.accumulateDownloadCounts(tr.DownloadCounts)
	report.Warnings = append(report.Warnings, tr.Warnings...)

	if tr.RejectReason != "" {
		report.Source.Rejected++
		report.RejectedRecords = append(report.RejectedRecords, RejectedRecord{
			SourceID: rec.ID.OID,
			Reason:   tr.RejectReason,
		})
		return nil
	}

	// Creator/updater resolution: the legacy ObjectIds must reference already
	// imported users. Orphans are reported and the reference is left NULL
	// (report orphans explicitly, never fabricate a user). The
	// SELECT runs outside the write transaction (a benign TOCTOU for an
	// offline import): if a user were deleted in between, the INSERT would
	// fail the foreign-key constraint — fail-safe either way.
	resolve := func(sourceID string) (*string, error) {
		if sourceID == "" {
			return nil, nil // absent in the source — not an orphan
		}
		var one int
		err := db.QueryRowContext(ctx, "SELECT 1 FROM users WHERE id = ?", sourceID).Scan(&one)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, nil // reported by the caller
		case err != nil:
			return nil, fmt.Errorf("resolve user reference: %w", err)
		default:
			return &sourceID, nil
		}
	}

	creatorID, err := resolve(tr.Post.CreatorSourceID)
	if err != nil {
		return err
	}
	if tr.Post.CreatorSourceID != "" && creatorID == nil {
		report.Relationships.CreatorOrphans++
	} else if creatorID != nil {
		report.Relationships.CreatorResolved++
	}

	updaterID, err := resolve(tr.Post.UpdaterSourceID)
	if err != nil {
		return err
	}
	if tr.Post.UpdaterSourceID != "" && updaterID == nil {
		report.Relationships.UpdaterOrphans++
	} else if updaterID != nil {
		report.Relationships.UpdaterResolved++
	}

	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("begin import tx: %w", err)
	}
	defer tx.Rollback()

	// creator_id/updater_id are nullable — pass a typed nil for NULL.
	var creatorArg any
	if creatorID != nil {
		creatorArg = *creatorID
	}
	var updaterArg any
	if updaterID != nil {
		updaterArg = *updaterID
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO blog_posts (id, title, subtitle, description, thumbnail_url,
			status, creator_id, updater_id, published_at_ms, created_at_ms, updated_at_ms, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		tr.Post.ID, tr.Post.Title, tr.Post.Subtitle, tr.Post.Description, tr.Post.ThumbnailURL,
		tr.Post.Status, creatorArg, updaterArg, tr.Post.PublishedAtMS, tr.Post.CreatedAtMS, tr.Post.UpdatedAtMS,
	)
	if err != nil {
		return fmt.Errorf("insert blog post: %w", err)
	}

	// Search index in the same transaction: rows without a publish time stay
	// out of the index. `> 0` is the belt-and-braces mirror of the store's
	// `published_at_ms IS NOT NULL` rule — the transform guarantees a positive
	// publish instant for every non-rejected record, so no accepted row takes
	// the false arm.
	if tr.Post.PublishedAtMS > 0 {
		rowid, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("blog post rowid: %w", err)
		}
		if err := store.IndexBlogPostTx(ctx, tx, rowid,
			tr.Post.Title, tr.Post.Subtitle, tr.Post.Description); err != nil {
			return err
		}
	}

	for _, dl := range tr.Post.Downloads {
		dlID, err := id.New()
		if err != nil {
			return fmt.Errorf("migration: random id: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO blog_post_downloads (id, blog_post_id, resolution, magnet_link, torrent_link, position, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			dlID, tr.Post.ID, dl.Resolution, dl.MagnetLink, dl.TorrentLink, dl.Position, tr.Post.CreatedAtMS,
		); err != nil {
			return fmt.Errorf("insert download: %w", err)
		}
		res := report.Downloads[dl.Resolution]
		res.Rows++
		report.Downloads[dl.Resolution] = res
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import tx: %w", err)
	}

	report.Source.Imported++
	report.Identifiers.SourceToTarget = append(report.Identifiers.SourceToTarget, IDMapping{
		SourceID: rec.ID.OID,
		TargetID: tr.Post.ID,
	})
	return nil
}

// ImportProjects transforms and inserts every legacy project record into
// the target database and returns the reconciliation report.
//
// Each record imports in its own transaction — a transaction never spans
// records — and stops on the first unexplained database error. Rejected
// records are reported and skipped, never half-imported. Duplicate slugs
// keep the FIRST record and reject later duplicates with a reason (slug is
// UNIQUE). Callers run the
// strict migration-history verification and foreign-key checks around this
// (the import command does it after import; docs/patterns/go/sqlite.md).
func ImportProjects(ctx context.Context, db *sql.DB, records []LegacyProject) (*ReconciliationReport, error) {
	report := newReport()
	report.Source.Projects = len(records)

	// Case-sensitive map — matches SQLite's BINARY UNIQUE(slug) (the slug is
	// case-sensitive), so slugs differing only by case are distinct rows, not
	// duplicates.
	seenSlugs := make(map[string]bool)
	for _, rec := range records {
		if err := importProjectOne(ctx, db, rec, report, seenSlugs); err != nil {
			return report, fmt.Errorf("migration: project record %q: %w", rec.ID.OID, err)
		}
	}
	return report, nil
}

// importProjectOne maps and inserts one project record.
func importProjectOne(ctx context.Context, db *sql.DB, rec LegacyProject, report *ReconciliationReport, seenSlugs map[string]bool) error {
	tr, err := TransformProject(rec)
	if err != nil {
		return err
	}
	report.accumulateProjectCounts(tr)
	report.Warnings = append(report.Warnings, tr.Warnings...)

	if tr.RejectReason != "" {
		report.Source.Rejected++
		report.RejectedRecords = append(report.RejectedRecords, RejectedRecord{
			SourceID: rec.ID.OID,
			Reason:   tr.RejectReason,
		})
		return nil
	}

	// Duplicate-slug rule (slug is UNIQUE): keep the first record, reject
	// later duplicates with a concise reason. The slug VALUE is content —
	// the report carries the reason only.
	if seenSlugs[tr.Project.Slug] {
		report.Source.Rejected++
		report.RejectedRecords = append(report.RejectedRecords, RejectedRecord{
			SourceID: rec.ID.OID,
			Reason:   "duplicate slug — an earlier record imported with the same slug",
		})
		return nil
	}
	seenSlugs[tr.Project.Slug] = true

	// Updater resolution: the legacy updatedBy ObjectId must reference an
	// already imported user. Orphans are reported and the reference is left
	// NULL (report orphans explicitly, never fabricate a user).
	// Creator stays NULL for migrated projects. The SELECT runs outside the
	// write transaction (a benign TOCTOU for an
	// offline import): if the user were deleted in between, the INSERT
	// would fail the foreign-key constraint — fail-safe either way.
	var updaterID *string
	if tr.Project.UpdaterSourceID != "" {
		var one int
		err := db.QueryRowContext(ctx, "SELECT 1 FROM users WHERE id = ?", tr.Project.UpdaterSourceID).Scan(&one)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// NULL reference, reported by the orphan counter below.
		case err != nil:
			return fmt.Errorf("resolve user reference: %w", err)
		default:
			updaterID = &tr.Project.UpdaterSourceID
		}
	}
	if tr.Project.UpdaterSourceID != "" && updaterID == nil {
		report.Relationships.UpdaterOrphans++
	} else if updaterID != nil {
		report.Relationships.UpdaterResolved++
	}

	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("begin import tx: %w", err)
	}
	defer tx.Rollback()

	// creator_id/updater_id are nullable — pass a typed nil for NULL.
	var updaterArg any
	if updaterID != nil {
		updaterArg = *updaterID
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO projects (id, title, description, slug, thumbnail_url,
			status, creator_id, updater_id, published_at_ms, created_at_ms, updated_at_ms, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		tr.Project.ID, tr.Project.Title, tr.Project.Description, tr.Project.Slug, tr.Project.ThumbnailURL,
		tr.Project.Status, nil, updaterArg, tr.Project.PublishedAtMS, tr.Project.CreatedAtMS, tr.Project.UpdatedAtMS,
	)
	if err != nil {
		return fmt.Errorf("insert project: %w", err)
	}

	// Search index in the same transaction; `> 0` is the belt-and-braces
	// mirror of the store's `published_at_ms IS NOT NULL` rule (see the
	// blog-post comment).
	if tr.Project.PublishedAtMS > 0 {
		rowid, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("project rowid: %w", err)
		}
		if err := store.IndexProjectTx(ctx, tx, rowid,
			tr.Project.Title, tr.Project.Description); err != nil {
			return err
		}
	}

	for _, dl := range tr.Project.Downloads {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO project_downloads (id, project_id, name, magnet_link, torrent_link, position, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			dl.ID, tr.Project.ID, dl.Name, dl.MagnetLink, dl.TorrentLink, dl.Position, tr.Project.CreatedAtMS,
		); err != nil {
			return fmt.Errorf("insert project download: %w", err)
		}
	}
	report.ProjectDownloads.Rows += len(tr.Project.Downloads)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import tx: %w", err)
	}

	report.Source.Imported++
	report.Identifiers.SourceToTarget = append(report.Identifiers.SourceToTarget, IDMapping{
		SourceID: rec.ID.OID,
		TargetID: tr.Project.ID,
	})
	return nil
}

// ImportUsers transforms and inserts every legacy user record. Users import
// BEFORE content: blog/project creator and updater references resolve
// against the imported users table.
//
// Each record imports in its own transaction and stops on the first
// unexplained database error. Rejected records are reported and skipped,
// never half-imported. User IDs are PRESERVED legacy ObjectIds, so the
// source→target mapping entries are identity pairs — the coverage still
// reports. No target session rows are created: sessions originate only
// from post-cutover sign-ins.
//
// Verifiers are preserved byte-for-byte in every category; the report
// classifies them, and sign-in denies the non-supported categories at
// runtime.
func ImportUsers(ctx context.Context, db *sql.DB, records []LegacyUser) (*ReconciliationReport, error) {
	report := newReport()
	report.Source.Users = len(records)

	// Canonical-key collision rule: the FIRST record keeps a lowercased
	// username/email; later duplicates reject per-record with a concise
	// reason (quarantined for manual resolution).
	seenCanon := make(map[string]bool)
	seenEmail := make(map[string]bool)
	for _, rec := range records {
		if err := importUserOne(ctx, db, rec, report, seenCanon, seenEmail); err != nil {
			return report, fmt.Errorf("migration: user record %q: %w", rec.ID.OID, err)
		}
	}
	return report, nil
}

// importUserOne maps and inserts one user record.
func importUserOne(ctx context.Context, db *sql.DB, rec LegacyUser, report *ReconciliationReport, seenCanon, seenEmail map[string]bool) error {
	tr := TransformUser(rec)
	report.accumulateUserCounts(tr)
	report.Warnings = append(report.Warnings, tr.Warnings...)

	if tr.RejectReason != "" {
		report.Source.Rejected++
		report.RejectedRecords = append(report.RejectedRecords, RejectedRecord{
			SourceID: rec.ID.OID,
			Reason:   tr.RejectReason,
		})
		return nil
	}

	// Duplicate canonical keys: reject per-record — the canonical forms are
	// the VALUES here, and the report reason names the key kind, never the
	// value (usernames/emails are personal data).
	if seenCanon[tr.User.UsernameCanon] {
		rejectUser(report, rec.ID.OID, "duplicate canonical username — an earlier record imported")
		return nil
	}
	if seenEmail[tr.User.Email] {
		rejectUser(report, rec.ID.OID, "duplicate canonical email — an earlier record imported")
		return nil
	}
	seenCanon[tr.User.UsernameCanon] = true
	seenEmail[tr.User.Email] = true

	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("begin import tx: %w", err)
	}
	defer tx.Rollback()

	// avatar_url is NULL: the legacy value is an absolute same-VPS MinIO URL
	// and the target column holds a relative media path — avatar media
	// migration belongs to the media import; the report counts the deferral.
	// Password is the preserved verifier string (never logged or printed).
	// email_verified_at_ms is stamped with created_at_ms: every imported
	// account is grandfathered as verified (real legacy emails, no
	// banner for community members).
	_, err = tx.ExecContext(ctx,
		`INSERT INTO users (id, username, username_canon, email, password,
			role, status, auth_version, avatar_url, email_verified_at_ms, created_at_ms, updated_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)`,
		tr.User.ID, tr.User.Username, tr.User.UsernameCanon, tr.User.Email, tr.User.Password,
		tr.User.Role, tr.User.Status, tr.User.AuthVersion, tr.User.CreatedAtMS, tr.User.CreatedAtMS, tr.User.UpdatedAtMS,
	)
	if err != nil {
		// A UNIQUE violation here means the target already held a row this
		// record collides with (a re-run, or an import batch before/after
		// this one) — a data conflict, not an operator-stopping bug. Report
		// it per-record; the pre-existing target row wins. The constraint
		// name refines the reason the store's generic canary-pinned
		// discriminator cannot.
		if store.IsUniqueViolation(err) {
			rejectUser(report, rec.ID.OID, uniqueUserCollisionReason(err))
			return nil
		}
		return fmt.Errorf("insert user: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import tx: %w", err)
	}

	report.Source.Imported++
	// Users preserve their legacy ObjectId as the target ID — the mapping
	// entry is an identity pair, kept for uniform coverage reporting.
	report.Identifiers.SourceToTarget = append(report.Identifiers.SourceToTarget, IDMapping{
		SourceID: rec.ID.OID,
		TargetID: tr.User.ID,
	})
	return nil
}

// rejectUser records one per-record user rejection.
func rejectUser(report *ReconciliationReport, sourceID, reason string) {
	report.Source.Rejected++
	report.RejectedRecords = append(report.RejectedRecords, RejectedRecord{
		SourceID: sourceID,
		Reason:   reason,
	})
}

// uniqueUserCollisionReason names the users constraint a rejected INSERT
// collided with. The error text is SQLite's own constraint report, reached
// through the store's canary-pinned UNIQUE discriminator; the default covers
// any shape the three known constraints do not name.
func uniqueUserCollisionReason(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "users.username_canon"):
		return "duplicate canonical username against an existing target row"
	case strings.Contains(message, "users.email"):
		return "duplicate canonical email against an existing target row"
	case strings.Contains(message, "users.id"):
		return "user id already present in the target"
	default:
		return "duplicate key against an existing target row"
	}
}
