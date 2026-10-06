package migration

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sick-fansubs/internal/media"
)

// AvatarImportReport is the reconciliation output of one avatar import run.
// Reasons are machine codes, never URLs, user ids, or filesystem paths.
type AvatarImportReport struct {
	Source struct {
		ExportRecords    int `json:"exportRecords"`
		UsersWithAvatars int `json:"usersWithLegacyAvatars"`
		MatchedUsers     int `json:"matchedUsers"`
		DistinctURLs     int `json:"distinctLegacyUrls"`
	} `json:"source"`
	Outcome struct {
		Processed     int `json:"processed"`
		Failed        int `json:"failed"`
		RowsRewritten int `json:"rowsRewritten"`
	} `json:"outcome"`
	Failures []MediaFailure `json:"failures"`
}

// ImportAvatars reads the target users that still have no avatar (NULL —
// the deferred import state), joins each to its legacy URL from
// legacyByUserID (export ObjectId → avatar URL, empty string when the
// record carries none), processes every distinct URL once from sourceDir,
// and rewrites the owning rows. It returns the report even when individual
// objects fail — failures are counted, not fatal, so one broken object
// cannot hide the rest of the run. The join and processing contracts are in
// internal/migration/AGENTS.md.
func ImportAvatars(ctx context.Context, db *sql.DB, dataDir, sourceDir string, legacyByUserID map[string]string) (*AvatarImportReport, error) {
	report := &AvatarImportReport{}
	report.Source.ExportRecords = len(legacyByUserID)

	// avatarRow names the target user id next to its legacy URL.
	type avatarRow struct {
		id string
	}
	byURL := map[string][]avatarRow{}
	var urlOrder []string
	var matchCount int

	for id, rawURL := range legacyByUserID {
		if rawURL == "" {
			continue
		}
		report.Source.UsersWithAvatars++
		if _, ok := byURL[rawURL]; !ok {
			urlOrder = append(urlOrder, rawURL)
		}
		byURL[rawURL] = append(byURL[rawURL], avatarRow{id: id})
	}

	failures := map[string]int{}
	addFailure := func(reason string) {
		failures[reason]++
		report.Outcome.Failed++
	}

	for _, u := range urlOrder {
		refs := byURL[u]
		report.Source.DistinctURLs++

		// A legacy URL whose user id does not exist in the target DB is a
		// rejected/unimported user (or a stale export) — skip with a
		// machine reason, never a URL or id.
		var matched []avatarRow
		for _, r := range refs {
			var one int
			err := db.QueryRowContext(ctx,
				`SELECT 1 FROM users WHERE id = ? AND avatar_url IS NULL`, r.id).Scan(&one)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				continue
			case err != nil:
				return nil, fmt.Errorf("migration: match avatar user: %w", err)
			default:
				matched = append(matched, r)
				matchCount++
			}
		}
		if len(matched) == 0 {
			continue
		}

		key, err := LegacyMediaKey(u)
		if err != nil {
			addFailure("badReferenceShape")
			continue
		}

		raw, err := readSourceBounded(filepath.Join(sourceDir, key))
		if err != nil {
			switch {
			case errors.Is(err, os.ErrNotExist):
				addFailure("missingSourceFile")
			case errors.Is(err, media.ErrInputTooLarge):
				addFailure("oversizedSource")
			default:
				addFailure("readSourceFile")
			}
			continue
		}

		// The ID derives from the source bytes, not the key (see
		// MediaIDForBytes): a changed object gets a fresh ID instead of
		// violating the immutable-cache contract.
		id := MediaIDForBytes(raw)
		rel, err := media.ProcessAvatar(dataDir, id, bytes.NewReader(raw))
		if err != nil {
			addFailure(processFailureReason(err))
			continue
		}

		// One distinct URL may be shared across several users — every row
		// referencing it is rewritten together.
		for _, r := range matched {
			res, err := db.ExecContext(ctx,
				`UPDATE users SET avatar_url = ? WHERE id = ? AND avatar_url IS NULL`, rel, r.id)
			if err != nil {
				return nil, fmt.Errorf("migration: rewrite avatar_url: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return nil, fmt.Errorf("migration: rows affected by avatar_url rewrite: %w", err)
			}
			report.Outcome.RowsRewritten += int(n)
		}

		report.Outcome.Processed++
	}

	report.Source.MatchedUsers = matchCount
	report.Failures = sortedFailures(failures)
	return report, nil
}
