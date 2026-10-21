package handler

import (
	"database/sql"
	"net/http"
	"slices"
	"time"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/store"
)

// Push notification handlers: the per-kind preference endpoints under
// /api/v1/notification-preferences and the self-test send that spends one
// real push on the caller's own devices. The subscription endpoints live in
// push_subscriptions.go.
//
// Chains mirror the notifications feed: reads on RequestID → TrustedOrigin
// → Session → ForcePasswordChange, writes additionally CSRF. Rate limits:
// only the self-test send carries a bucket (user-keyed — it spends a push
// message); the preference endpoints are authenticated origin+CSRF-protected
// per-user operations (favorites precedent).

// pushKinds is the known-kind list in the notification_preferences CHECK's
// order (the user_notifications CHECK keeps its own order — the
// migration-header contract; new kinds amend BOTH): the
// two shipped kinds plus comment, content_updated, new_content,
// comment_removed, and
// draft_activity — the one STAFF-ONLY, opt-in kind. It is the single source
// for the preferences read; unknown kinds on the write path answer the
// masked 404. The draft notice is additionally role-gated at the API
// surface (pushKindsFor), so a below-floor session never sees a toggle it
// could not use.
var pushKinds = []string{
	notificationKindHeart,
	notificationKindCommentReply,
	notificationKindComment,
	notificationKindContentUpdated,
	notificationKindNewContent,
	notificationKindCommentRemoved,
	notificationKindDraftActivity,
}

// pushKindsFor narrows pushKinds to the kinds a session's ROLE can hold:
// the whole list for staff, the list MINUS the draft
// notice below the staff floor. The draft notice is invisible to a
// below-floor session in both directions — the read omits the kind and the
// write answers the masked 404 — so the UI never offers a toggle that could
// not fire. A stored preference row survives a role change and becomes
// visible again on promotion; while the session is below the floor it is
// inert (the emitters choose recipients by the draft-visibility gate).
func pushKindsFor(role string) []string {
	if identity.CanModerateContent(role) {
		return pushKinds
	}
	kinds := make([]string, 0, len(pushKinds))
	for _, kind := range pushKinds {
		if kind != notificationKindDraftActivity {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// notificationPreferencesDTO is the GET envelope — every known kind with
// its EFFECTIVE toggle (row else role default).
type notificationPreferencesDTO struct {
	Preferences []notificationPreferenceDTO `json:"preferences"`
}

type notificationPreferenceDTO struct {
	Kind        string `json:"kind"`
	PushEnabled bool   `json:"pushEnabled"`
}

// notificationPreferenceRequest is the PUT body. PushEnabled is a POINTER:
// nil distinguishes an absent field (422 required) from an explicit false.
type notificationPreferenceRequest struct {
	PushEnabled *bool `json:"pushEnabled"`
}

// NotificationPreferencesGet returns GET /api/v1/notification-preferences —
// every known kind with its effective push toggle.
func NotificationPreferencesGet(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "notification preferences read requires authentication")
		if !ok {
			return
		}
		if !allowNoQueryParams(w, r) {
			return
		}
		kinds := pushKindsFor(su.Role)
		prefs, err := store.EffectiveNotificationPreferences(r.Context(), db, su.UserID, kinds, func(kind string) bool {
			return roleDefaultsPushOn(su.Role, kind)
		})
		if err != nil {
			writeInternalError(w, r, "notification preferences read failed", "error", err)
			return
		}
		items := make([]notificationPreferenceDTO, 0, len(prefs))
		for _, p := range prefs {
			items = append(items, notificationPreferenceDTO{Kind: p.Kind, PushEnabled: p.PushEnabled})
		}
		writeJSON(w, r, http.StatusOK, notificationPreferencesDTO{Preferences: items})
	}
}

// NotificationPreferenceSet returns PUT /api/v1/notification-preferences/{kind}
// — write one explicit toggle. 204; an unknown kind answers the masked 404
// (a path resource).
func NotificationPreferenceSet(db *sql.DB, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "notification preference write requires authentication")
		if !ok {
			return
		}
		kind := r.PathValue("kind")
		// A below-floor session cannot hold the staff-only kind at all: the
		// masked 404 is the same answer it gets for a kind that does not exist
		// (do not reveal a surface you cannot use).
		if !isKnownPushKind(kind) || (kind == notificationKindDraftActivity && !identity.CanModerateContent(su.Role)) {
			NotFound(w, r)
			return
		}

		var req notificationPreferenceRequest
		if err := readJSONLimit(w, r, &req, maxAuthBodySize); err != nil {
			writeJSONBodyError(w, r, err)
			return
		}
		if req.PushEnabled == nil {
			writeValidationErrors(w, r, []Violation{{Field: "pushEnabled", Code: "required"}})
			return
		}

		if err := store.SetNotificationPreference(r.Context(), db, su.UserID, kind, *req.PushEnabled, now().UnixMilli()); err != nil {
			writeStoreAccountError(w, r, err, "notification preference write", "error", err)
			return
		}
		writeNoContent(w, r)
	}
}

// roleDefaultsPushOn reports a kind's default when no preference row exists:
// the audience kinds default
// ON for moderator+ and OFF for everyone else, while the staff-only draft
// notice defaults OFF at EVERY role — opt-in, never a role default. The
// audience floor is `identity.CanModerateContent`, the one model predicate
// the handler and the sender's post-commit default both read.
func roleDefaultsPushOn(role, kind string) bool {
	if kind == notificationKindDraftActivity {
		return false
	}
	return identity.CanModerateContent(role)
}

// pushTestEndpointDTO is one endpoint's outcome in the self-test report.
// It carries the caller's OWN row id, the
// coarse push-service family, and the transport verdict — never the endpoint
// URL, never the key material, never another user's row.
type pushTestEndpointDTO struct {
	ID       string `json:"id"`
	Service  string `json:"service"`
	Accepted bool   `json:"accepted"`
	// Status is the LAST response status the push service returned, sticky
	// across retries; omitted when no attempt ever received a response (a pure
	// transport failure or a cancelled window). A deliberate zero sentinel:
	// 0 is not a status any server returns, so a pointer would buy nothing.
	Status int `json:"status,omitempty"`
	// Removed reports that the dead-endpoint rule deleted this row during
	// this very send.
	Removed bool `json:"removed,omitempty"`
}

// pushTestResultDTO is the POST /api/v1/push-subscriptions/test response:
// how many of the caller's devices ACCEPTED the test message, plus one
// outcome per attempted endpoint. 0 means
// nothing is subscribed, delivery is disabled on this deployment
// (development without the VAPID secret), or every device was rejected.
type pushTestResultDTO struct {
	Delivered int                   `json:"delivered"`
	Endpoints []pushTestEndpointDTO `json:"endpoints"`
}

// pushTestBucket is the self-test bucket label — ONE name for the log line
// (its 5/10 min value lives beside the limiter in routes.Push).
const pushTestBucket = "push-test"

// PushTestSend returns POST /api/v1/push-subscriptions/test — deliver one
// test notification to the caller's OWN devices (the account page's
// settings button). 200 + {"delivered": n, "endpoints": [...]}.
//
// The endpoint creates no feed row, reads no preferences, and writes no
// audit row: the user asked for the message, so the per-kind toggles do
// not apply. It takes no body and rejects query parameters (the
// namespace's strictness). The limiter is user-keyed and REQUIRED — a nil
// panics inside `allowUserBucket`.
func PushTestSend(tester push.TestSender, limiter *middleware.RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "push test send requires authentication")
		if !ok {
			return
		}
		if !allowNoQueryParams(w, r) {
			return
		}
		// The bucket runs AFTER the cheap validation (the comment-write
		// convention): a malformed press must not spend the user's budget.
		if !allowUserBucket(w, r, limiter, pushTestBucket, su.UserID) {
			return
		}

		report := push.TestReport{Endpoints: []push.DeliveryOutcome{}}
		if tester != nil {
			r2, err := tester.SendTest(r.Context(), su.UserID)
			if err != nil {
				writeInternalError(w, r, "push test send failed", "error", err)
				return
			}
			report = r2
			// Logged only when the seam is ENABLED: with delivery off nothing
			// was attempted, and "push test sent, delivered 0" would be a lie.
			logger.InfoContext(r.Context(), "push test sent", "userId", su.UserID, "delivered", report.Delivered, "endpointCount", len(report.Endpoints))
		}
		endpoints := make([]pushTestEndpointDTO, 0, len(report.Endpoints))
		for _, o := range report.Endpoints {
			endpoints = append(endpoints, pushTestEndpointDTO{
				ID:       o.SubscriptionID,
				Service:  o.Service,
				Accepted: o.Accepted,
				Status:   o.Status,
				Removed:  o.Removed,
			})
		}
		writeJSON(w, r, http.StatusOK, pushTestResultDTO{Delivered: report.Delivered, Endpoints: endpoints})
	}
}

func isKnownPushKind(kind string) bool {
	return slices.Contains(pushKinds, kind)
}
