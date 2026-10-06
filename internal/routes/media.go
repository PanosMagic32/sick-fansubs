package routes

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/handler"
)

// Media registers the media routes: public immutable serving,
// the staff upload endpoint, and the explicit
// deletion endpoint.
//
//	GET    /media/images/{file} — the served, processed media object
//	POST   /api/v1/media         — upload + process (admin+)
//	DELETE /api/v1/media/{file}  — explicit delete (admin+)
//
// Serving middleware: the public chain (RequestID only) — media is public
// read-only asset delivery outside /api/v1, so there is no session/CSRF/
// rate-limit chain. The handler enforces the strict ID/extension contract and
// immutable caching.
//
// Non-GET methods fall through to the subtree fallback and get a 404
// problem (never the SPA HTML response). A literal ".." segment never
// reaches the handler: the ServeMux cleans the URL path and redirects the
// client to the cleaned path, whose target is the 404 fallback — encoded
// separators (%2F) stay inside the segment and are rejected by the
// handler's parser with 400.
//
// Upload middleware: authenticatedChain (RequestID → TrustedOrigin → Session
// → ForcePasswordChange → CSRF). The upload is the POST /api/v1 media path;
// the exact POST pattern outranks the API fallback, and non-POST requests to
// /api/v1/media fall to the /api/v1/ catch-all (JSON 404). The DELETE
// /api/v1/media/{file} path carries the same chain — see the registration
// below.
func Media(mux *http.ServeMux, dataDir string, db *sql.DB, secure bool, trustedOrigin string, publicBaseURL string) {
	mux.Handle("GET /media/images/{file}", publicChain(handler.ServeMedia(dataDir)))
	mux.Handle("/media/", notFound())
	mux.Handle("/media", notFound())

	var upload http.Handler = handler.MediaUpload(dataDir, publicBaseURL)
	upload = authenticatedChain(upload, db, secure, trustedOrigin)
	mux.Handle("POST /api/v1/media", upload)

	var delete http.Handler = handler.MediaDelete(dataDir, db)
	delete = authenticatedChain(delete, db, secure, trustedOrigin)
	mux.Handle("DELETE /api/v1/media/{file}", delete)
	// The method-less pattern catches every non-POST/DELETE method: without
	// it the method-aware ServeMux would answer 405 in text/plain, breaking
	// the rule that unknown API paths return a JSON problem
	// (the specific POST/DELETE patterns still outrank this one).
	mux.Handle("/api/v1/media", notFound())
}
