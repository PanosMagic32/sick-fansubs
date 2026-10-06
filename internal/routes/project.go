package routes

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/push"
)

// Projects registers the project endpoints under /api/v1/projects and the
// staff read surface under /api/v1/admin/projects.
//
// Routes:
//
//	GET    /api/v1/projects          — public keyset-paginated list of published projects
//	GET    /api/v1/projects/{id}     — public detail of one published project
//	POST   /api/v1/projects          — create, slug generated (admin+, CSRF)
//	PUT    /api/v1/projects/{id}     — update (moderator+, slug admin+, If-Match, CSRF)
//	DELETE /api/v1/projects/{id}     — hard-delete (admin+, If-Match, CSRF)
//	GET    /api/v1/admin/projects          — staff list, all statuses (staff gate)
//	GET    /api/v1/admin/projects/{id}     — staff detail + ETag (staff gate)
//
// publicBaseURL is the configured public origin used to emit absolute
// thumbnail URLs.
//
// Middleware: the public GETs keep the public chain (RequestID only). The
// mutations carry authenticatedChain (RequestID → TrustedOrigin → Session →
// ForcePasswordChange → CSRF; the method-aware mux picks the chain per method
// on the same path). The admin subtree shares the same chain — CSRF only
// validates unsafe methods, so the staff GETs pass through it untouched (the
// favorites pattern). No rate limit on any of these: authenticated per-user
// operations protected by origin + CSRF. Unknown /api/v1 paths — the admin
// subtree included — fall to the shared /api/v1/ JSON 404 (APIFallback).
//
// The exact-path registrations and the {id} wildcards coexist through Go
// ServeMux precedence (the most specific pattern wins); paths the project
// endpoints do not own fall through to the API catch-all — they must never
// produce the SPA HTML response.
//
// notify is the web push seam: the create handlers fan out new_content
// broadcasts after the create transaction commits, and the update handler
// fans out content_updated events to followers or the new_content broadcast
// on a first publish.
func Projects(mux *http.ServeMux, db *sql.DB, dataDir string, secure bool, trustedOrigin string, publicBaseURL string, notify push.Notifier) {

	listHandler := publicChain(handler.ProjectList(db, publicBaseURL))
	mux.Handle("GET /api/v1/projects", listHandler)

	detailHandler := publicChain(handler.ProjectDetail(db, publicBaseURL))
	mux.Handle("GET /api/v1/projects/{id}", detailHandler)

	// Content mutations. The write handlers
	// pre-read through the staff read, so the authorization gate
	// is server-side end to end.
	var create http.Handler = handler.ProjectCreate(db, dataDir, publicBaseURL, notify, nil)
	mux.Handle("POST /api/v1/projects", authenticatedChain(create, db, secure, trustedOrigin))

	var update http.Handler = handler.ProjectUpdate(db, dataDir, publicBaseURL, notify, nil)
	mux.Handle("PUT /api/v1/projects/{id}", authenticatedChain(update, db, secure, trustedOrigin))

	var del http.Handler = handler.ProjectDelete(db)
	mux.Handle("DELETE /api/v1/projects/{id}", authenticatedChain(del, db, secure, trustedOrigin))

	// Staff read surface. Both GETs carry
	// authenticatedChain; CSRF only validates unsafe methods so it passes the
	// reads through. Non-GET methods on these paths and unknown /api/v1/admin
	// paths fall to the shared /api/v1/ JSON 404 (APIFallback) — the
	// method-aware mux's 405 text/plain never reaches the client.
	adminList := authenticatedChain(handler.ProjectStaffList(db, publicBaseURL), db, secure, trustedOrigin)
	mux.Handle("GET /api/v1/admin/projects", adminList)
	adminDetail := authenticatedChain(handler.ProjectStaffDetail(db, publicBaseURL), db, secure, trustedOrigin)
	mux.Handle("GET /api/v1/admin/projects/{id}", adminDetail)
}
