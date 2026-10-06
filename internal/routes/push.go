package routes

import (
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/push"
)

// Push registers the web push endpoints under /api/v1/push-subscriptions
// and /api/v1/notification-preferences.
//
// Routes:
//
//	GET    /api/v1/push-subscriptions          — own subscription rows
//	POST   /api/v1/push-subscriptions          — store/resubscribe one device
//	POST   /api/v1/push-subscriptions/test     — send one self-test push (200)
//	DELETE /api/v1/push-subscriptions/{id}     — remove one subscription (204)
//	GET    /api/v1/notification-preferences    — effective per-kind toggles
//	PUT    /api/v1/notification-preferences/{kind} — write one explicit toggle
//
// Chains: reads on authenticatedReadsChain (RequestID → TrustedOrigin →
// Session → ForcePasswordChange); writes on authenticatedChain (the
// notifications feed precedent). The subscription/preference endpoints carry
// no rate limits — authenticated origin+CSRF-protected per-user operations
// (the favorites precedent); the self-test send is the one exception: it
// spends a real push message, so it carries a user-keyed bucket
// (5/10 min, the pwChangeLimiter precedent).
//
// tester is the push seam for the self-test endpoint (nil in development
// without the VAPID secret — the endpoint then reports zero delivered).
//
// Unknown paths and methods inside the namespaces fall to the JSON 404
// problem (the notifications/users precedent).
func Push(mux *http.ServeMux, db *sql.DB, secure bool, trustedOrigin string, tester push.TestSender) {

	mux.Handle("GET /api/v1/push-subscriptions",
		authenticatedReadsChain(handler.PushSubscriptionsList(db), db, secure, trustedOrigin))
	mux.Handle("GET /api/v1/notification-preferences",
		authenticatedReadsChain(handler.NotificationPreferencesGet(db), db, secure, trustedOrigin))

	mux.Handle("POST /api/v1/push-subscriptions",
		authenticatedChain(handler.PushSubscriptionCreate(db, nil), db, secure, trustedOrigin))

	// The self-test send: one shared user-keyed bucket per process (the
	// pwChangeLimiter shape — handlers share limiter instances, never mint
	// their own).
	testLimiter := middleware.NewRateLimiter(5, 10*time.Minute, time.Minute)
	mux.Handle("POST /api/v1/push-subscriptions/test",
		authenticatedChain(handler.PushTestSend(tester, testLimiter), db, secure, trustedOrigin))
	mux.Handle("DELETE /api/v1/push-subscriptions/{id}",
		authenticatedChain(handler.PushSubscriptionDelete(db), db, secure, trustedOrigin))
	mux.Handle("PUT /api/v1/notification-preferences/{kind}",
		authenticatedChain(handler.NotificationPreferenceSet(db, nil), db, secure, trustedOrigin))

	// JSON 404 for every other method/path in the namespaces (exact root +
	// subtree, the notifications precedent).
	mux.Handle("/api/v1/push-subscriptions", notFound())
	mux.Handle("/api/v1/push-subscriptions/", notFound())
	mux.Handle("/api/v1/notification-preferences", notFound())
	mux.Handle("/api/v1/notification-preferences/", notFound())
}
