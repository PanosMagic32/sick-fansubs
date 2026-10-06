package routes

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/push"
)

// Blog registers the blog endpoints under /api/v1/blog-posts and the staff
// read surface under /api/v1/admin/blog-posts.
//
// Routes:
//
//	GET    /api/v1/blog-posts          — public keyset-paginated list of published posts
//	GET    /api/v1/blog-posts/{id}     — public detail of one published post
//	POST   /api/v1/blog-posts          — create (admin+, CSRF)
//	PUT    /api/v1/blog-posts/{id}     — update (moderator+, If-Match, CSRF)
//	DELETE /api/v1/blog-posts/{id}     — hard-delete (admin+, If-Match, CSRF)
//	GET    /api/v1/admin/blog-posts          — staff list, all statuses (staff gate)
//	GET    /api/v1/admin/blog-posts/{id}     — staff detail + ETag (staff gate)
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
// operations protected by origin + CSRF.
//
// notify is the web push seam: the create handlers fan out new_content
// broadcasts after the create transaction commits, and the update handler
// fans out content_updated events to followers or the new_content broadcast
// on a first publish.
//
// The exact-path registrations and the {id} wildcards coexist through Go
// ServeMux precedence (the most specific pattern wins); paths the blog
// endpoints do not own fall through to the API catch-all — they must never
// produce the SPA HTML response.
func Blog(mux *http.ServeMux, db *sql.DB, dataDir string, secure bool, trustedOrigin string, publicBaseURL string, notify push.Notifier) {

	mux.Handle("GET /api/v1/blog-posts", publicChain(handler.BlogList(db, publicBaseURL)))
	mux.Handle("GET /api/v1/blog-posts/{id}", publicChain(handler.BlogDetail(db, publicBaseURL)))

	// Content mutations. The write handlers
	// pre-read through the staff read, so the authorization gate
	// is server-side end to end.
	var create http.Handler = handler.BlogCreate(db, dataDir, publicBaseURL, notify, nil)
	mux.Handle("POST /api/v1/blog-posts", authenticatedChain(create, db, secure, trustedOrigin))

	var update http.Handler = handler.BlogUpdate(db, dataDir, publicBaseURL, notify, nil)
	mux.Handle("PUT /api/v1/blog-posts/{id}", authenticatedChain(update, db, secure, trustedOrigin))

	var del http.Handler = handler.BlogDelete(db)
	mux.Handle("DELETE /api/v1/blog-posts/{id}", authenticatedChain(del, db, secure, trustedOrigin))

	// Staff read surface. Both GETs carry
	// authenticatedChain; CSRF only validates unsafe methods so it passes the
	// reads through. Non-GET methods on these paths and unknown /api/v1/admin
	// paths fall to the shared /api/v1/ JSON 404 (APIFallback) — the
	// method-aware mux's 405 text/plain never reaches the client.
	adminList := authenticatedChain(handler.BlogStaffList(db, publicBaseURL), db, secure, trustedOrigin)
	mux.Handle("GET /api/v1/admin/blog-posts", adminList)
	adminDetail := authenticatedChain(handler.BlogStaffDetail(db, publicBaseURL), db, secure, trustedOrigin)
	mux.Handle("GET /api/v1/admin/blog-posts/{id}", adminDetail)
}
