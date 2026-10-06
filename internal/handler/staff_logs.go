package handler

import (
	"net/http"
	"unicode/utf8"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
)

// Super-admin logs viewer: GET
// /api/v1/staff/logs — the last N records of the application's rotated
// JSON-lines log file, filtered by minimum level and an optional substring.
//
// The response is a bounded TAIL READ over the sink the logging package
// writes; there is no log database and no index. Because it is a
// tail and not a keyset collection, the envelope is items-only: no `after`,
// no `pageInfo`, no `total` (the tail-read exception to the keyset
// envelope). A `hasMore` signal would cost a full scan of every file to be
// honest, so it is deliberately absent.
//
// The floor is super-admin, not the dashboard's moderator+: the log
// carries client addresses and operational detail that the metrics surface
// does not. The precise wire contract lives in docs/api/openapi.yaml
// (operationId getStaffLogs).

const (
	// defaultStaffLogLevel keeps the default view to warn and error — the
	// records an operator opens the viewer for.
	defaultStaffLogLevel = "warn"

	// The tail size: `limit` is how many matching records to return, not a
	// cursor. The default/maximum pair is an accepted exception to the
	// 20/100 keyset defaults:
	// twenty log lines answer nothing, and the sink's 20 MB ceiling bounds
	// the read no matter how large the limit is.
	defaultStaffLogLimit = 100
	maxStaffLogLimit     = 1000

	// maxStaffLogQueryLen bounds `q` in characters (inputs are
	// bounded). It is generous for a diagnostic substring and keeps a huge
	// query string from being lower-cased and scanned against every line.
	maxStaffLogQueryLen = 256
)

// staffLogItem is one log record on the wire. Fields carries every remaining
// slog attribute untouched, so the viewer shows what the process actually
// wrote instead of a lossy projection.
type staffLogItem struct {
	Time   string         `json:"time"`
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields"`
}

// staffLogs is the GET /api/v1/staff/logs response body. Items is always
// present — a fresh install with no log file answers `{"items":[]}`.
type staffLogs struct {
	Items []staffLogItem `json:"items"`
}

// StaffLogs returns the handler for GET /api/v1/staff/logs. logDir is the
// resolved sink directory (data/logs).
//
// Outcomes:
//   - 200 with the newest matching records for an authenticated super-admin.
//   - 401 generic problem for an unauthenticated request.
//   - 403 for any authenticated role below super-admin.
//   - 400 for an unknown/duplicated query parameter, a malformed query
//     string, or a non-integer `limit`.
//   - 422 for an unknown `level`, an out-of-range `limit`, or a `q` longer
//     than 256 characters.
//   - 500 generic problem only if the sink itself cannot be read (a missing
//     directory is not that case — it answers an empty list).
//
// The floor check runs BEFORE the query is parsed or the file is touched, so
// a below-floor request never reaches the sink.
func StaffLogs(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "staff logs requires authentication")
		if !ok {
			return
		}
		if !identity.CanViewLogs(su.Role) {
			writeForbidden(w, r, "staff logs requires super-admin")
			return
		}

		query, ok := strictQueryParams(w, r, "level", "q", "limit")
		if !ok {
			return
		}

		levelName := defaultStaffLogLevel
		if raw, present := query["level"]; present {
			levelName = raw[0]
		}
		minLevel, ok := logging.ParseLevel(levelName)
		if !ok {
			writeValidationErrors(w, r, []Violation{{Field: "level", Code: "invalidValue"}})
			return
		}

		limit, ok := parseLimitParam(w, r, query, defaultStaffLogLimit, maxStaffLogLimit)
		if !ok {
			return
		}

		search := query.Get("q")
		if utf8.RuneCountInString(search) > maxStaffLogQueryLen {
			writeValidationErrors(w, r, []Violation{{Field: "q", Code: "maxLength"}})
			return
		}

		entries, err := logging.Read(r.Context(), logDir, logging.Filter{
			MinLevel: minLevel,
			Query:    search,
			Limit:    limit,
		})
		if err != nil {
			writeInternalError(w, r, logger, "staff logs read failed", "error", err)
			return
		}

		items := make([]staffLogItem, 0, len(entries))
		for _, e := range entries {
			items = append(items, staffLogItem{
				Time:   formatAPITime(e.Time.UnixMilli()),
				Level:  e.Level,
				Msg:    e.Msg,
				Fields: e.Fields,
			})
		}

		writeJSON(w, r, http.StatusOK, staffLogs{Items: items})
	}
}
