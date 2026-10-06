package routes

import (
	"database/sql"
	"net/http"

	"sick-fansubs/internal/handler"
)

// Notifications registers the notifications-feed endpoints under
// /api/v1/notifications.
//
// Routes:
//
//	GET    /api/v1/notifications              — keyset feed list (authenticated)
//	GET    /api/v1/notifications/unread-count — the header badge count
//	POST   /api/v1/notifications/{id}/read    — mark one visible event read
//	POST   /api/v1/notifications/read-all     — mark every visible event read
//	DELETE /api/v1/notifications/{id}         — delete/dismiss one feed entry
//	POST   /api/v1/notifications/clear-read   — delete every READ event row
//	POST   /api/v1/notifications/delete-all   — empty the viewer's whole feed
//
// Chains: the two GETs on authenticatedReadsChain (RequestID → TrustedOrigin
// → Session → ForcePasswordChange — the reads stay free of CSRF); the five
// writes on authenticatedChain, which adds it. The forced-change gate applies
// to the whole subtree — its "every non-exempt path" rule.
//
// Unknown paths and methods inside the namespace fall to the JSON 404
// problem — never the SPA HTML, and never the method-aware mux's 405
// text/plain. Both the exact namespace root and the
// subtree are registered (the users precedent).
func Notifications(mux *http.ServeMux, db *sql.DB, secure bool, trustedOrigin string) {

	mux.Handle("GET /api/v1/notifications",
		authenticatedReadsChain(handler.NotificationsList(db), db, secure, trustedOrigin))
	mux.Handle("GET /api/v1/notifications/unread-count",
		authenticatedReadsChain(handler.NotificationsUnreadCount(db), db, secure, trustedOrigin))

	// The writes additionally CSRF (authenticatedChain's full composition).
	mux.Handle("POST /api/v1/notifications/read-all",
		authenticatedChain(handler.NotificationsMarkAllRead(db, nil), db, secure, trustedOrigin))
	mux.Handle("POST /api/v1/notifications/{id}/read",
		authenticatedChain(handler.NotificationsMarkRead(db, nil), db, secure, trustedOrigin))
	mux.Handle("DELETE /api/v1/notifications/{id}",
		authenticatedChain(handler.NotificationsDelete(db, nil), db, secure, trustedOrigin))
	mux.Handle("POST /api/v1/notifications/clear-read",
		authenticatedChain(handler.NotificationsClearRead(db), db, secure, trustedOrigin))
	mux.Handle("POST /api/v1/notifications/delete-all",
		authenticatedChain(handler.NotificationsDeleteAll(db, nil), db, secure, trustedOrigin))

	// JSON 404 for every other method/path in the namespace (the users
	// precedent: exact root + subtree).
	mux.Handle("/api/v1/notifications", notFound())
	mux.Handle("/api/v1/notifications/", notFound())
}
