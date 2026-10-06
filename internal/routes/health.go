package routes

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/handler"
)

// Health registers liveness and readiness probes at unversioned paths.
//
// Routes:
//
//	GET /health/live  — 200 {"status":"ok"} when the process can respond
//	GET /health/ready — 200 {"status":"ready"} when dependencies are healthy,
//	                   503 {"status":"not_ready"} otherwise
//
// Health applies publicChain: probes stay as lightweight as possible — no
// session, origin, CSRF, or rate-limit work — and still carry the request ID
// the chain mints, so the routes need no handler-side fallback. dataDir feeds
// the readiness checker's restore-marker gate.
func Health(mux *http.ServeMux, db *sql.DB, dataDir string) {
	mux.Handle("GET /health/live", publicChain(http.HandlerFunc(handler.HealthLive)))
	mux.Handle("GET /health/ready", publicChain(handler.HealthReady(database.DBHealthChecker{DB: db, DataDir: dataDir})))
}
