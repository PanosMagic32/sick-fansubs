package routes

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/handler"
)

// Search registers the public search endpoint under /api/v1/search.
//
// Chain: the public chain (RequestID only). Search is a public GET: no
// session, no CSRF/origin checks (those protect unsafe methods), and no rate
// limit (public reads are deliberately unthrottled — internal/handler/AGENTS.md).
// Unknown /api/v1/search subpaths fall through to the API catch-all — they
// must never produce the SPA HTML response.
func Search(mux *http.ServeMux, db *sql.DB, publicBaseURL string) {
	mux.Handle("GET /api/v1/search", publicChain(handler.Search(db, publicBaseURL)))
}
