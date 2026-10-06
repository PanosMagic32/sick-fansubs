package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Project write transactions (docs/patterns/go/content-kinds.md): the blog
// store's counterpart — same transactions, same gate, same revision guards,
// plus the slug contract. The per-kind names come from ProjectContent.
//
// Slug uniqueness: the UNIQUE constraint on projects.slug is the race-free
// backstop. Create callers retry with collision suffixes; update callers map
// ErrSlugTaken to a 422 {slug, alreadyTaken}.

// isSlugTakenViolation narrows a UNIQUE violation to the slug constraint
// (the message names the failed constraint). Any other unique violation
// (e.g. the primary key) must NOT be swallowed as a slug collision.
func isSlugTakenViolation(err error) bool {
	return err != nil && IsUniqueViolation(err) && strings.Contains(err.Error(), ProjectContent.table+".slug")
}

// CreateProjectParams is the target row for a staff create; the blog create
// contract applies, with Slug in place of Subtitle and NULL for 0's stamp.
type CreateProjectParams struct {
	ID            string
	Title         string
	Description   string
	Slug          string
	ThumbnailURL  string
	Status        string
	CreatorID     string
	UpdaterID     string
	PublishedAtMS int64
	NowMS         int64
	Downloads     []Download
}

// UpdateProjectParams is the target row for a staff update; the blog update
// contract applies, with Slug in place of Subtitle.
type UpdateProjectParams struct {
	Title        string
	Description  string
	Slug         string
	ThumbnailURL string
	Status       string
	UpdaterID    string
	NowMS        int64
	Downloads    []Download
}

// CreateProject inserts one row, its download rows, and — for a published
// row — its FTS entry, in one transaction. Same events as CreateBlogPost; a
// slug collision surfaces as ErrSlugTaken (the caller retries with a suffix).
func CreateProject(ctx context.Context, db *sql.DB, p CreateProjectParams) (*StaffProject, []NotificationEvent, error) {
	k := ProjectContent
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return nil, nil, fmt.Errorf("store: begin %s create: %w", k.label, err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO `+k.table+` (id, title, `+k.ownColumn+`, description, thumbnail_url, status,
			creator_id, updater_id, published_at_ms, created_at_ms, updated_at_ms, revision)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?, 1)`,
		p.ID, p.Title, p.Slug, p.Description, p.ThumbnailURL, p.Status,
		p.CreatorID, p.UpdaterID, p.PublishedAtMS, p.NowMS, p.NowMS)
	if err != nil {
		if isSlugTakenViolation(err) {
			return nil, nil, ErrSlugTaken
		}
		return nil, nil, fmt.Errorf("store: insert %s: %w", k.noun, err)
	}
	// Capture the content row's rowid before any other insert: the FTS row
	// is keyed to it, and the capture order must not depend on driver
	// internals.
	rowid, err := res.LastInsertId()
	if err != nil {
		return nil, nil, fmt.Errorf("store: %s rowid: %w", k.noun, err)
	}
	if err := insertContentDownloadsTx(ctx, tx, k, p.ID, p.NowMS, p.Downloads); err != nil {
		return nil, nil, err
	}
	// Index only rows with a publish time — a draft row stays out of the
	// index; the transition into published reindexes on update.
	if p.PublishedAtMS > 0 {
		if err := IndexProjectTx(ctx, tx, rowid, p.Title, p.Description); err != nil {
			return nil, nil, err
		}
	}

	// The emission legs are mutually exclusive: a published create
	// broadcasts, a draft create notices the staff who can see it.
	var events []NotificationEvent
	if p.PublishedAtMS > 0 {
		events, err = notifyNewContentTx(ctx, tx, p.CreatorID, k, p.ID, p.Title, p.NowMS)
	} else {
		events, err = notifyDraftActivityTx(ctx, tx, p.CreatorID, draftActionCreated, k, p.ID, p.Title, p.NowMS)
	}
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("store: commit %s create: %w", k.label, err)
	}

	// The row is our own — the ungated read-back needs no visibility gate.
	project, err := getStaffProjectUngated(ctx, db, p.ID)
	if err != nil {
		return nil, nil, err
	}
	return project, events, nil
}

// UpdateProject applies a revision-conditional update with the search,
// favorites, and download cascades in one transaction (the UpdateBlogPost
// contract, plus the slug backstop: ErrSlugTaken on a collision).
func UpdateProject(ctx context.Context, db *sql.DB, id string, revision int64, p UpdateProjectParams) (*StaffProject, []NotificationEvent, error) {
	k := ProjectContent
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return nil, nil, fmt.Errorf("store: begin %s update: %w", k.label, err)
	}
	defer tx.Rollback()

	var (
		rowid        int64
		curStatus    string
		curPub       sql.NullInt64
		curCreatedMS int64
	)
	err = tx.QueryRowContext(ctx,
		`SELECT rowid, status, published_at_ms, created_at_ms FROM `+k.table+` WHERE id = ?`, id).
		Scan(&rowid, &curStatus, &curPub, &curCreatedMS)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("store: read %s for update: %w", k.noun, err)
	}

	// The publish stamp is set on the first transition into published and
	// preserved afterwards.
	var newPub any
	switch {
	case curPub.Valid:
		newPub = curPub.Int64
	case p.Status == "published":
		newPub = p.NowMS
	default:
		newPub = nil
	}

	if curStatus == "published" && p.Status != "published" {
		// Leaving published drops the favorites: a favorite of inaccessible
		// content never lingers.
		if err := deleteContentFavoritesTx(ctx, tx, k, id); err != nil {
			return nil, nil, err
		}
	}

	// Index membership is publish-time-based: replace the old entry when one
	// exists, insert the new one when the row keeps or gains a publish time.
	if curPub.Valid {
		if err := DeleteProjectTx(ctx, tx, rowid); err != nil {
			return nil, nil, err
		}
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE `+k.table+`
		 SET title = ?, `+k.ownColumn+` = ?, description = ?, thumbnail_url = ?, status = ?,
		     published_at_ms = ?, updated_at_ms = ?, updater_id = ?, revision = revision + 1
		 WHERE id = ? AND revision = ?`,
		p.Title, p.Slug, p.Description, p.ThumbnailURL, p.Status,
		newPub, p.NowMS, p.UpdaterID, id, revision)
	if err != nil {
		if isSlugTakenViolation(err) {
			return nil, nil, ErrSlugTaken
		}
		return nil, nil, fmt.Errorf("store: update %s: %w", k.noun, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, nil, fmt.Errorf("store: update %s rows: %w", k.noun, err)
	}
	if affected == 0 {
		// The row existed in the SELECT a moment ago; only a concurrent
		// revision bump explains zero rows (benign TOCTOU).
		return nil, nil, ErrConflict
	}
	// The download set is replaced in the same transaction: a stale revision
	// rolls the whole change back, downloads included. New rows keep the
	// content row's created_at_ms (the import precedent).
	if err := replaceContentDownloadsTx(ctx, tx, k, id, curCreatedMS, p.Downloads); err != nil {
		return nil, nil, err
	}
	if newPub != nil {
		if err := IndexProjectTx(ctx, tx, rowid, p.Title, p.Description); err != nil {
			return nil, nil, err
		}
	}

	var events []NotificationEvent
	switch {
	case curStatus == "published" && p.Status == "published":
		events, err = notifyFollowersTx(ctx, tx, "content_updated", p.UpdaterID, k, id, nil, p.Title, p.NowMS)
	case !curPub.Valid && p.Status == "published":
		events, err = notifyNewContentTx(ctx, tx, p.UpdaterID, k, id, p.Title, p.NowMS)
	case p.Status != "published":
		action := draftActionUpdated
		if curStatus == "published" {
			action = draftActionUnpublished
		}
		events, err = notifyDraftActivityTx(ctx, tx, p.UpdaterID, action, k, id, p.Title, p.NowMS)
	}
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("store: commit %s update: %w", k.label, err)
	}

	project, err := getStaffProjectUngated(ctx, db, id)
	if err != nil {
		return nil, nil, err
	}
	return project, events, nil
}

// DeleteProject hard-deletes one row under the revision guard. Downloads and
// favorites cascade via their foreign keys; the FTS entry is removed in the
// same transaction. A stale revision is ErrConflict.
func DeleteProject(ctx context.Context, db *sql.DB, id string, revision int64) error {
	k := ProjectContent
	tx, err := db.BeginTx(ctx, nil) // nil = immediate (our DSN default)
	if err != nil {
		return fmt.Errorf("store: begin %s delete: %w", k.label, err)
	}
	defer tx.Rollback()

	var rowid int64
	err = tx.QueryRowContext(ctx,
		`SELECT rowid FROM `+k.table+` WHERE id = ?`, id).Scan(&rowid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("store: read %s for delete: %w", k.noun, err)
	}
	if err := DeleteProjectTx(ctx, tx, rowid); err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx,
		`DELETE FROM `+k.table+` WHERE id = ? AND revision = ?`, id, revision)
	if err != nil {
		return fmt.Errorf("store: delete %s: %w", k.noun, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete %s rows: %w", k.noun, err)
	}
	if affected == 0 {
		// Same benign TOCTOU as UpdateProject.
		return ErrConflict
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit %s delete: %w", k.label, err)
	}
	return nil
}
