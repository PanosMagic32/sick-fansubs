package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sick-fansubs/internal/media"
	"sick-fansubs/internal/migration"
)

// TestRun_FetchesThumbnails drives the full command path: source file →
// thumbnailURLs → download → report, against a local httptest origin.
func TestRun_FetchesExternalHostURL(t *testing.T) {
	t.Parallel()

	// A postimg-shaped path — not the legacy /media/images/<key> form. The
	// fetch must derive the same hashed key the import later joins on.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/N00vRLSn/image.png" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("png-bytes-c"))
	}))
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "legacy.jsonl")
	if err := os.WriteFile(src, []byte(`{"_id":{"$oid":"aaa"},"thumbnail":"`+server.URL+`/N00vRLSn/image.png"}`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	outDir := t.TempDir()
	rep, err := run(discardLogger(), src, "blog-posts", outDir)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Outcome.Downloaded != 1 || rep.Outcome.Failed != 0 {
		t.Errorf("outcome = %+v, want 1 downloaded / 0 failed", rep.Outcome)
	}

	key, err := migration.LegacyMediaKey(server.URL + "/N00vRLSn/image.png")
	if err != nil {
		t.Fatalf("LegacyMediaKey: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, key))
	if err != nil || string(raw) != "png-bytes-c" {
		t.Errorf("downloaded bytes = %q, %v; want png-bytes-c under key %q", raw, err, key)
	}
}

// TestRun_LogsProgress pins the stderr progress contract: a start line and
// a progress line every 25 objects — the download phase of a large run must
// not be silent (the report stays stdout-only).
func TestRun_LogsProgress(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	t.Cleanup(server.Close)

	var lines []string
	for i := range 30 {
		lines = append(lines, fmt.Sprintf(`{"_id":{"$oid":"%03d"},"thumbnail":"%s/media/images/k%d.jpg"}`, i, server.URL, i))
	}
	src := filepath.Join(t.TempDir(), "legacy.jsonl")
	if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	rep, err := run(logger, src, "blog-posts", t.TempDir())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Outcome.Downloaded != 30 {
		t.Errorf("downloaded = %d, want 30", rep.Outcome.Downloaded)
	}
	msgs := map[string]int{}
	for _, rec := range decodeLogRecords(t, buf.String()) {
		if msg, ok := rec["msg"].(string); ok {
			msgs[msg]++
		}
	}
	if msgs["fetch started"] != 1 {
		t.Errorf("fetch started records = %d, want 1", msgs["fetch started"])
	}
	if msgs["fetch progress"] != 1 {
		t.Errorf("fetch progress records = %d, want 1 (one line at object 25)", msgs["fetch progress"])
	}
}

func TestRun_FetchesThumbnails(t *testing.T) {
	t.Parallel()

	const (
		keyA = "1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
		keyB = "1778832678231-6d1c4453-5d43-4b6b-9f89-146a77655f1f.png"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media/images/" + keyA:
			_, _ = w.Write([]byte("jpeg-bytes-a"))
		case "/media/images/" + keyB:
			_, _ = w.Write([]byte("png-bytes-b"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "legacy.jsonl")
	lines := []string{
		`{"_id":{"$oid":"aaa"},"thumbnail":"` + server.URL + `/media/images/` + keyA + `"}`,
		`{"_id":{"$oid":"bbb"},"thumbnail":"` + server.URL + `/media/images/` + keyB + `"}`,
		// Duplicate URL: one distinct URL, one download.
		`{"_id":{"$oid":"ccc"},"thumbnail":"` + server.URL + `/media/images/` + keyA + `"}`,
	}
	if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	outDir := t.TempDir()
	report, err := run(discardLogger(), src, "blog-posts", outDir)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Source.Records != 3 || report.Source.DistinctURLs != 2 {
		t.Errorf("source = %+v, want 3 records / 2 distinct URLs", report.Source)
	}
	if report.Outcome.Downloaded != 2 || report.Outcome.AlreadyPresent != 0 || report.Outcome.Failed != 0 {
		t.Errorf("outcome = %+v, want 2 downloaded / 0 failed", report.Outcome)
	}
	for key, want := range map[string]string{keyA: "jpeg-bytes-a", keyB: "png-bytes-b"} {
		raw, err := os.ReadFile(filepath.Join(outDir, key))
		if err != nil {
			t.Fatalf("read downloaded %s: %v", key, err)
		}
		if string(raw) != want {
			t.Errorf("%s bytes = %q, want %q", key, raw, want)
		}
	}
}

// TestRun_AlreadyPresentSkips pins the idempotent re-run contract: existing
// files are counted and never re-fetched.
func TestRun_AlreadyPresentSkips(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Any request reaching the server is a contract break.
		t.Errorf("unexpected request: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	const key = "1778523938726-903c8e7a-71ac-4e3f-a9ed-72a018ec0fbc.jpg"
	src := filepath.Join(t.TempDir(), "legacy.jsonl")
	if err := os.WriteFile(src, []byte(`{"_id":{"$oid":"aaa"},"thumbnail":"`+server.URL+`/media/images/`+key+`"}`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, key), []byte("present"), 0o644); err != nil {
		t.Fatalf("pre-seed file: %v", err)
	}

	report, err := run(discardLogger(), src, "blog-posts", outDir)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Outcome.AlreadyPresent != 1 || report.Outcome.Downloaded != 0 || report.Outcome.Failed != 0 {
		t.Errorf("outcome = %+v, want 1 alreadyPresent / 0 downloaded / 0 failed", report.Outcome)
	}
}

// TestRun_FailuresAreCountedNotFatal covers the per-object failure classes:
// a 404 (legacy and external-host shapes), a non-200 status, an oversized
// body, a non-absolute reference, and an unsupported scheme — each counted,
// none aborting the run.
func TestRun_FailuresAreCountedNotFatal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/media/images/missing.jpg":
			http.NotFound(w, r)
		case "/media/images/server-error.jpg":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/media/images/oversized.jpg":
			_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", media.MaxInputBytes+1)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "legacy.jsonl")
	lines := []string{
		`{"_id":{"$oid":"aaa"},"thumbnail":"` + server.URL + `/media/images/missing.jpg"}`,
		`{"_id":{"$oid":"bbb"},"thumbnail":"` + server.URL + `/media/images/server-error.jpg"}`,
		`{"_id":{"$oid":"ccc"},"thumbnail":"` + server.URL + `/media/images/oversized.jpg"}`,
		// External-host shape: derived key, fetch
		// attempted — a dead external object counts as notFound, never a skip.
		`{"_id":{"$oid":"ddd"},"thumbnail":"` + server.URL + `/external/image.png"}`,
		`{"_id":{"$oid":"eee"},"thumbnail":"relative/k.jpg"}`,
		`{"_id":{"$oid":"fff"},"thumbnail":"ftp://example.com/media/images/k.jpg"}`,
	}
	if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	report, err := run(discardLogger(), src, "blog-posts", t.TempDir())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Outcome.Failed != 6 || report.Outcome.Downloaded != 0 {
		t.Errorf("outcome = %+v, want 6 failed / 0 downloaded", report.Outcome)
	}
	reasons := map[string]int{}
	for _, f := range report.Failures {
		reasons[f.Reason] = f.Count
	}
	want := map[string]int{
		"notFound":          2,
		"httpStatus":        1,
		"oversized":         1,
		"badReferenceShape": 1,
		"unsupportedScheme": 1,
	}
	for reason, count := range want {
		if reasons[reason] != count {
			t.Errorf("failure %q = %d, want %d (all: %+v)", reason, reasons[reason], count, report.Failures)
		}
	}
}

// TestRun_ProjectsKind proves the -kind projects path reads the project
// shape and fetches its thumbnails.
func TestRun_ProjectsKind(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("project-jpeg"))
	}))
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "legacy-projects.jsonl")
	lines := []string{
		`{"_id":{"$oid":"aaa"},"title":"T","slug":"s","thumbnail":"` + server.URL + `/media/images/k.jpg"}`,
	}
	if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	report, err := run(discardLogger(), src, "projects", t.TempDir())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Source.Kind != "projects" || report.Outcome.Downloaded != 1 {
		t.Errorf("report = %+v, want projects kind + 1 download", report)
	}
}

// TestRun_UsersKind proves the -kind users path reads the avatar field of
// the user shape (not `thumbnail`) and fetches it — the avatars path
// reuses the same key derivation and download machinery.
func TestRun_UsersKind(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("avatar-jpeg"))
	}))
	t.Cleanup(server.Close)

	src := filepath.Join(t.TempDir(), "legacy-users.jsonl")
	lines := []string{
		`{"_id":{"$oid":"65a0b1c2d3e4f5a6b7c8d9e1"},"avatar":"` + server.URL + `/media/images/1778-a.jpg"}`,
		`{"_id":{"$oid":"65a0b1c2d3e4f5a6b7c8d9e2"}}`, // no avatar — never fetched
	}
	if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	report, err := run(discardLogger(), src, "users", t.TempDir())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Source.Kind != "users" || report.Source.Records != 2 || report.Source.DistinctURLs != 1 {
		t.Errorf("source = %+v, want users kind, 2 records, 1 distinct URL", report.Source)
	}
	if report.Outcome.Downloaded != 1 || report.Outcome.Failed != 0 {
		t.Errorf("outcome = %+v, want 1 download, 0 failed", report.Outcome)
	}
}
