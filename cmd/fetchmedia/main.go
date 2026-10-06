// Command fetchmedia downloads the legacy thumbnail/avatar objects referenced
// by a legacy export file into a local media-source directory, keyed by the
// join key migration.LegacyMediaKey derives (the legacy object key for
// MinIO-shaped URLs, a URL-hash key for external-host URLs), the same join
// the media import (cmd/importmedia)
// and the avatar import (cmd/importavatars) use.
//
// This is the rehearsal half of the media migration: the import
// commands later process the downloaded bytes through the media
// pipeline and rewrite the database references. At cutover the S3-side
// fetch replaces this HTTP fetch; the key layout and the processing steps
// stay the same.
//
// Only http(s) URLs are fetched; every other shape is counted
// with a stable reason code and never aborts the run (failures are
// per-object: a 404 on one image must not hide the rest). Files already
// present in the target directory are skipped, so re-runs are idempotent —
// the media import hashes the bytes, so a stale local copy only matters
// when the legacy object itself changed.
//
// Usage:
//
//	MEDIA_SOURCE_DIR=./media-source IMPORT_FILE=/path/export.json cmd/fetchmedia
//	MEDIA_SOURCE_DIR=./media-source IMPORT_FILE=/path/export.json cmd/fetchmedia -kind projects
//	MEDIA_SOURCE_DIR=./media-source IMPORT_FILE=/path/users.json cmd/fetchmedia -kind users
//
// The source file is the legacy export as a JSON array or JSON-lines.
// Exit codes: 0 when the run completes (per-object failures are counted in
// the report), 1 on a fatal error. The report goes to stdout as
// indented JSON; operational logs go to stderr — a start line, then a
// progress line every 25 objects (the download phase is silent otherwise,
// and a full 355-object run takes minutes).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"sick-fansubs/internal/media"
	"sick-fansubs/internal/migration"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	kind := flag.String("kind", "blog-posts", "record kind to fetch media for: blog-posts (default), projects, or users (avatars)")
	flag.Parse()

	importPath := os.Getenv("IMPORT_FILE")
	if importPath == "" {
		slog.Error("fetch failed", "error", errors.New("IMPORT_FILE is required (legacy export file, JSON array or JSON-lines)"))
		os.Exit(1)
	}
	outDir := os.Getenv("MEDIA_SOURCE_DIR")
	if outDir == "" {
		outDir = "./media-source"
	}

	if _, err := run(logger, importPath, *kind, outDir); err != nil {
		slog.Error("fetch failed", "error", err)
		os.Exit(1)
	}
}

// fetchReport is the reconciliation output of one fetch run. Reasons are
// machine codes, never URLs or filesystem paths.
type fetchReport struct {
	Source struct {
		Kind         string `json:"kind"`
		Records      int    `json:"records"`
		DistinctURLs int    `json:"distinctUrls"`
	} `json:"source"`
	Outcome struct {
		Downloaded     int `json:"downloaded"`
		AlreadyPresent int `json:"alreadyPresent"`
		Failed         int `json:"failed"`
	} `json:"outcome"`
	Failures []fetchFailure `json:"failures,omitempty"`
}

type fetchFailure struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// Download-failure reason codes. Sentinels: the Error() text IS the report
// code, so the failure map stays one source of truth.
var (
	errBadReferenceShape = errors.New("badReferenceShape")
	errUnsupportedScheme = errors.New("unsupportedScheme")
	errNotFound          = errors.New("notFound")
	errHTTPStatus        = errors.New("httpStatus")
	errNetwork           = errors.New("networkError")
	errOversized         = errors.New("oversized")
	errReadBody          = errors.New("readBody")
	errWriteFile         = errors.New("writeFile")
)

// run reads the export, downloads every distinct media object, prints the
// report, and returns it. It is the testable seam — main() only wires the
// environment.
func run(logger *slog.Logger, importPath, kind, outDir string) (*fetchReport, error) {
	urls, records, err := mediaURLs(importPath, kind)
	if err != nil {
		return nil, err
	}
	if records == 0 {
		return nil, errors.New("source file contains no records")
	}

	report := &fetchReport{}
	report.Source.Kind = kind
	report.Source.Records = records
	report.Source.DistinctURLs = len(urls)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create media source directory: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	failures := map[string]int{}
	addFailure := func(reason string) {
		failures[reason]++
		report.Outcome.Failed++
	}

	// The download phase can take minutes with zero visible output — log a
	// start line and a progress line every 25 objects so the operator can
	// see the run move (report stays stdout-only; logs are stderr).
	logger.Info("fetch started",
		"kind", kind,
		"records", records,
		"distinctUrls", len(urls),
	)

	for i, rawURL := range urls {
		key, err := migration.LegacyMediaKey(rawURL)
		if err != nil {
			addFailure(errBadReferenceShape.Error())
			continue
		}
		u, err := url.Parse(rawURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			addFailure(errUnsupportedScheme.Error())
			continue
		}

		dest := filepath.Join(outDir, key)
		if _, err := os.Stat(dest); err == nil {
			report.Outcome.AlreadyPresent++
		} else if err := downloadOne(client, rawURL, dest); err != nil {
			addFailure(err.Error())
		} else {
			report.Outcome.Downloaded++
		}

		if (i+1)%25 == 0 {
			logger.Info("fetch progress",
				"objects", i+1,
				"of", len(urls),
				"downloaded", report.Outcome.Downloaded,
				"alreadyPresent", report.Outcome.AlreadyPresent,
				"failed", report.Outcome.Failed,
			)
		}
	}

	report.Failures = sortedFetchFailures(failures)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return report, fmt.Errorf("write report: %w", err)
	}
	logger.Info("fetch complete",
		"kind", kind,
		"records", records,
		"distinctUrls", len(urls),
		"downloaded", report.Outcome.Downloaded,
		"alreadyPresent", report.Outcome.AlreadyPresent,
		"failed", report.Outcome.Failed,
	)
	return report, nil
}

// mediaURLs reads the export for the given kind and returns the
// distinct non-empty media URLs in first-seen order (the thumbnail field
// for content kinds, the avatar field for users).
func mediaURLs(importPath, kind string) ([]string, int, error) {
	var (
		urls    []string
		seen    = map[string]bool{}
		records int
	)
	add := func(mediaURL string) {
		if strings.TrimSpace(mediaURL) != "" && !seen[mediaURL] {
			seen[mediaURL] = true
			urls = append(urls, mediaURL)
		}
	}
	switch kind {
	case "blog-posts":
		recs, err := migration.ReadLegacyBlogPostsFile(importPath)
		if err != nil {
			return nil, 0, err
		}
		records = len(recs)
		for _, r := range recs {
			add(r.Thumbnail)
		}
	case "projects":
		recs, err := migration.ReadLegacyProjectsFile(importPath)
		if err != nil {
			return nil, 0, err
		}
		records = len(recs)
		for _, r := range recs {
			add(r.Thumbnail)
		}
	case "users":
		// Avatars: the legacy field is `avatar`, not `thumbnail` — the join key
		// derivation is identical (all nine production avatars are clean MinIO
		// URLs, so the key is the object key).
		recs, err := migration.ReadLegacyUsersFile(importPath)
		if err != nil {
			return nil, 0, err
		}
		records = len(recs)
		for _, r := range recs {
			add(r.Avatar)
		}
	default:
		return nil, 0, fmt.Errorf("unknown record kind %q (want blog-posts, projects, or users)", kind)
	}
	return urls, records, nil
}

// downloadOne fetches one object and writes it atomically (tmp + rename on
// the same directory) — a partial download must never poison the media
// import.
func downloadOne(client *http.Client, rawURL, dest string) error {
	resp, err := client.Get(rawURL)
	if err != nil {
		return errNetwork
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return errHTTPStatus
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, media.MaxInputBytes+1))
	if err != nil {
		return errReadBody
	}
	if len(raw) > media.MaxInputBytes {
		return errOversized
	}

	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return errWriteFile
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return errWriteFile
	}
	return nil
}

// sortedFetchFailures renders the failure map deterministically for stable
// reports across runs.
func sortedFetchFailures(failures map[string]int) []fetchFailure {
	if len(failures) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(failures))
	for reason := range failures {
		reasons = append(reasons, reason)
	}
	slices.Sort(reasons)
	out := make([]fetchFailure, 0, len(reasons))
	for _, reason := range reasons {
		out = append(out, fetchFailure{Reason: reason, Count: failures[reason]})
	}
	return out
}
