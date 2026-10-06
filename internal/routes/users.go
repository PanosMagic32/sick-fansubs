package routes

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/middleware"
)

// Users registers user endpoints under /api/v1/users/.
//
// Routes:
//
//	GET    /api/v1/users         — staff user list
//	POST   /api/v1/users/{id}/reset-password — admin password reset
//	PATCH  /api/v1/users/{id}/role — role change
//	DELETE /api/v1/users/{id}      — user deletion
//	POST   /api/v1/users/{id}/suspend — suspension
//	POST   /api/v1/users/{id}/reactivate — reactivation
//	GET    /api/v1/users/me — current user's profile
//	PUT    /api/v1/users/me/email — email change
//	PUT    /api/v1/users/me/avatar — self-service avatar upload
//	GET    /api/v1/users/me/sessions — own active sessions
//	DELETE /api/v1/users/me/sessions/{id} — revoke one own session (idempotent)
//	GET    /api/v1/users/me/favorites/blog-posts            — keyset list
//	GET    /api/v1/users/me/favorites/blog-posts/{id}       — favorite status
//	PUT    /api/v1/users/me/favorites/blog-posts/{id}       — add (idempotent)
//	DELETE /api/v1/users/me/favorites/blog-posts/{id}       — remove (idempotent)
//	GET/PUT/DELETE /api/v1/users/me/favorites/projects[/{id}] — same contract
//
// Chain: authenticatedChain (RequestID → TrustedOrigin → Session →
// ForcePasswordChange → CSRF).
//
// CSRF only validates unsafe methods (GETs pass through untouched), so the
// whole subtree shares one chain. No rate limit — authenticated per-user
// operations protected by origin + CSRF. The
// forced-change gate applies to the subtree: profile and
// favorites are protected until a flagged user changes their password — the
// four exempt auth paths live on the auth chain, not here. Unknown paths
// inside the namespace return the JSON 404 problem, never the SPA HTML
// response.
//
// publicBaseURL is the configured canonical public origin used to emit
// absolute thumbnail URLs in the favorites lists and the absolute avatar URLs
// (avatarUrl) in the staff user list + staff user write responses — the same
// value the blog/projects routes receive.
//
// sender + verifySendLimiter serve the email-change surface: the
// emailSvc carries the real mailer (the admin-reset
// authSvc above deliberately keeps the nil-sender guard — break-glass
// account operations never send mail), and verifySendLimiter is the
// SHARED verification-send bucket constructed in cmd/api (1/user/30min —
// the same instance routes.Auth receives for resend).
func Users(mux *http.ServeMux, db *sql.DB, secure bool, trustedOrigin string, publicBaseURL string, dataDir string, sender mail.Sender, verifySendLimiter *middleware.RateLimiter) {
	// The admin-reset path never sends mail or builds links — the nil
	// sender turns an accidental ForgotPassword call on this surface into
	// an honest 500 (the service guards it), never a logged credential.
	authSvc := auth.New(db, nil, "")
	// The email-change surface DOES send mail — a second
	// service with the real sender + base URL.
	emailSvc := auth.New(db, sender, publicBaseURL)

	// Staff list lives on the BARE namespace root (the blog-list precedent:
	// collections address the bare path). The exact "/api/v1/users"
	// catch-all below still answers non-GET methods with the JSON 404 — the
	// method-aware mux's 405 text/plain never reaches the client.
	staffList := authenticatedChain(handler.StaffUserList(db, publicBaseURL), db, secure, trustedOrigin)
	mux.Handle("GET /api/v1/users", staffList)

	usersMux := http.NewServeMux()
	usersMux.HandleFunc("GET /me", handler.CurrentUserProfile(db, publicBaseURL))

	// Self-service avatar upload.
	usersMux.HandleFunc("PUT /me/avatar", handler.AvatarUpload(db, dataDir, publicBaseURL, nil))

	// Email change — current-password gated,
	// shared verification-send bucket, CSRF'd like every mutation on this
	// chain.
	usersMux.HandleFunc("PUT /me/email", handler.EmailChange(db, emailSvc, publicBaseURL, verifySendLimiter))

	// Self-service session list — the account page's
	// "Συσκευές" block. The DELETE is CSRF'd by the chain and clears the
	// cookie when the caller revokes the current session.
	usersMux.HandleFunc("GET /me/sessions", handler.SessionsList(db, nil))
	usersMux.HandleFunc("DELETE /me/sessions/{id}", handler.SessionRevoke(db, secure))

	// Password reset — admin+ with peer
	// protection, CSRF'd like every mutation on this chain.
	usersMux.HandleFunc("POST /{id}/reset-password", handler.UserPasswordReset(db, authSvc))

	// Role change + user deletion. The role
	// response projects avatarUrl, so it takes the public base URL; deletion
	// answers 204 and needs none.
	usersMux.HandleFunc("PATCH /{id}/role", handler.UserRoleChange(db, publicBaseURL))
	usersMux.HandleFunc("DELETE /{id}", handler.UserDelete(db))

	// Suspension + reactivation — moderator+
	// with peer protection (moderators manage users only), the guard 409 on
	// the last active super-admin.
	usersMux.HandleFunc("POST /{id}/suspend", handler.UserSuspend(db, publicBaseURL))
	usersMux.HandleFunc("POST /{id}/reactivate", handler.UserReactivate(db, publicBaseURL))

	// Favorites.
	usersMux.HandleFunc("GET /me/favorites/blog-posts", handler.FavoritesList(handler.BlogFavoritesKind, db, publicBaseURL))
	usersMux.HandleFunc("GET /me/favorites/blog-posts/{id}", handler.FavoritesStatus(handler.BlogFavoritesKind, db))
	usersMux.HandleFunc("PUT /me/favorites/blog-posts/{id}", handler.FavoritesAdd(handler.BlogFavoritesKind, db, nil))
	usersMux.HandleFunc("DELETE /me/favorites/blog-posts/{id}", handler.FavoritesRemove(handler.BlogFavoritesKind, db))
	usersMux.HandleFunc("GET /me/favorites/projects", handler.FavoritesList(handler.ProjectFavoritesKind, db, publicBaseURL))
	usersMux.HandleFunc("GET /me/favorites/projects/{id}", handler.FavoritesStatus(handler.ProjectFavoritesKind, db))
	usersMux.HandleFunc("PUT /me/favorites/projects/{id}", handler.FavoritesAdd(handler.ProjectFavoritesKind, db, nil))
	usersMux.HandleFunc("DELETE /me/favorites/projects/{id}", handler.FavoritesRemove(handler.ProjectFavoritesKind, db))

	// Follows — the bell
	// toggle driving the comment/content_updated kinds.
	usersMux.HandleFunc("GET /me/follows/blog-posts/{id}", handler.FollowsStatus(handler.BlogFollowsKind, db))
	usersMux.HandleFunc("PUT /me/follows/blog-posts/{id}", handler.FollowsAdd(handler.BlogFollowsKind, db, nil))
	usersMux.HandleFunc("DELETE /me/follows/blog-posts/{id}", handler.FollowsRemove(handler.BlogFollowsKind, db))
	usersMux.HandleFunc("GET /me/follows/projects/{id}", handler.FollowsStatus(handler.ProjectFollowsKind, db))
	usersMux.HandleFunc("PUT /me/follows/projects/{id}", handler.FollowsAdd(handler.ProjectFollowsKind, db, nil))
	usersMux.HandleFunc("DELETE /me/follows/projects/{id}", handler.FollowsRemove(handler.ProjectFollowsKind, db))
	usersMux.Handle("/", notFound())

	chain := authenticatedChain(usersMux, db, secure, trustedOrigin)

	// Both the exact namespace root and the subtree are registered: the
	// exact pattern catches /api/v1/users itself, which the subtree
	// pattern does not match (mirrors APIFallback).
	mux.Handle("/api/v1/users", notFound())
	mux.Handle("/api/v1/users/", http.StripPrefix("/api/v1/users", chain))
}
