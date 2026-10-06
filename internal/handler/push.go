package handler

import (
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"sick-fansubs/internal/id"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/store"
)

// Push notification handlers: the
// subscription and preference endpoints under /api/v1/push-subscriptions
// and /api/v1/notification-preferences, plus the self-test send that spends
// one real push on the caller's own
// devices.
//
// Chains mirror the notifications feed: reads on RequestID → TrustedOrigin
// → Session → ForcePasswordChange, writes additionally CSRF. Rate limits:
// only the self-test send carries a bucket (user-keyed — it spends a push
// message); the other endpoints are authenticated origin+CSRF-protected
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

// Subscription sizes: the endpoint URL bound, and
// the RFC 8291 key-material bounds — p256dh decodes to a 65-byte
// uncompressed EC point, auth to the 16-byte authentication secret.
const (
	maxPushEndpointLen = 512
	p256dhDecodedLen   = 65
	authDecodedLen     = 16
)

// pushSubscriptionRequest is the POST body — the browser's
// PushSubscription JSON shape (endpoint + keys).
type pushSubscriptionRequest struct {
	Endpoint string          `json:"endpoint"`
	Keys     pushKeysRequest `json:"keys"`
}

type pushKeysRequest struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// pushSubscriptionDTO is the create response body — the stored row id the
// client keeps for DELETE.
type pushSubscriptionDTO struct {
	ID string `json:"id"`
}

// pushSubscriptionsListDTO is the settings-list envelope (own rows only).
type pushSubscriptionsListDTO struct {
	Subscriptions []pushSubscriptionItemDTO `json:"subscriptions"`
}

type pushSubscriptionItemDTO struct {
	ID        string `json:"id"`
	Endpoint  string `json:"endpoint"`
	CreatedAt string `json:"createdAt"`
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

// PushSubscriptionCreate returns POST /api/v1/push-subscriptions —
// store/resubscribe one device subscription. 201 + {"id"}; the id is the
// stored row (the minted id on a fresh insert, the existing row's id on a
// same-endpoint resubscribe). An endpoint owned by another user is
// reassigned by the store.
func PushSubscriptionCreate(db *sql.DB, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "push subscription create requires authentication")
		if !ok {
			return
		}

		var req pushSubscriptionRequest
		if err := readJSONLimit(w, r, &req, maxAuthBodySize); err != nil {
			writeJSONBodyError(w, r, err)
			return
		}
		if !validatePushSubscriptionBody(w, r, req) {
			return
		}

		subscriptionID, err := id.New()
		if err != nil {
			writeInternalError(w, r, logger, "push subscription id generation failed", "error", err)
			return
		}

		// The trimmed endpoint is what gets STORED — validation and
		// persistence must agree on the value (the send path uses the
		// stored copy verbatim).
		endpoint := strings.TrimSpace(req.Endpoint)
		storedID, err := store.UpsertPushSubscription(r.Context(), db, subscriptionID, su.UserID,
			endpoint, req.Keys.P256dh, req.Keys.Auth, now().UnixMilli())
		if err != nil {
			writeStoreAccountError(w, r, err, "push subscription upsert", "error", err)
			return
		}
		writeJSON(w, r, http.StatusCreated, pushSubscriptionDTO{ID: storedID})
	}
}

// validatePushSubscriptionBody enforces the wire bounds and writes the 422
// on failure. The base64url checks decode
// strictly: p256dh must be a 65-byte uncompressed point, auth a 16-byte
// secret — anything else is not a usable subscription.
func validatePushSubscriptionBody(w http.ResponseWriter, r *http.Request, req pushSubscriptionRequest) bool {
	var violations []Violation

	endpoint := strings.TrimSpace(req.Endpoint)
	switch {
	case endpoint == "":
		violations = append(violations, Violation{Field: "endpoint", Code: "required"})
	case len(endpoint) > maxPushEndpointLen:
		violations = append(violations, Violation{Field: "endpoint", Code: "maxLength"})
	default:
		u, err := url.Parse(endpoint)
		// Userinfo and fragments never belong in a push endpoint — the
		// browser's subscription URLs are scheme+host+path.
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			violations = append(violations, Violation{Field: "endpoint", Code: "invalidValue"})
		}
	}

	if !validPushKey(req.Keys.P256dh, p256dhDecodedLen) {
		violations = append(violations, Violation{Field: "keys.p256dh", Code: "invalidFormat"})
	}
	if !validPushKey(req.Keys.Auth, authDecodedLen) {
		violations = append(violations, Violation{Field: "keys.auth", Code: "invalidFormat"})
	}

	if len(violations) > 0 {
		writeValidationErrors(w, r, violations)
		return false
	}
	return true
}

// validPushKey reports whether key is base64url that decodes to exactly
// want bytes. URL-safe decoding first (no padding — browsers send raw),
// then padded variants the library also accepts (p256dh/auth base64url
// as received).
func validPushKey(key string, want int) bool {
	if key == "" || len(key) > 512 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		// Retry with the padding-tolerant standard decoding the send path
		// uses (webpush-go pads before decoding).
		raw, err = base64.URLEncoding.DecodeString(key)
		if err != nil {
			return false
		}
	}
	return len(raw) == want
}

// PushSubscriptionsList returns GET /api/v1/push-subscriptions — the
// user's subscription rows for the settings list.
func PushSubscriptionsList(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "push subscriptions list requires authentication")
		if !ok {
			return
		}
		if !allowNoQueryParams(w, r) {
			return
		}
		subs, err := store.ListPushSubscriptions(r.Context(), db, su.UserID)
		if err != nil {
			writeInternalError(w, r, logger, "push subscriptions list failed", "error", err)
			return
		}
		items := make([]pushSubscriptionItemDTO, 0, len(subs))
		for _, s := range subs {
			items = append(items, pushSubscriptionItemDTO{
				ID:        s.ID,
				Endpoint:  s.Endpoint,
				CreatedAt: formatAPITime(s.CreatedAtMS),
			})
		}
		writeJSON(w, r, http.StatusOK, pushSubscriptionsListDTO{Subscriptions: items})
	}
}

// PushSubscriptionDelete returns DELETE /api/v1/push-subscriptions/{id} —
// remove one subscription row. 204 always (idempotent; unknown/foreign ids
// are masked — no existence disclosure).
func PushSubscriptionDelete(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
		su, ok := requireSession(w, r, "push subscription delete requires authentication")
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid push subscription id")
			return
		}
		if err := store.DeletePushSubscription(r.Context(), db, su.UserID, id); err != nil {
			writeInternalError(w, r, logger, "push subscription delete failed", "error", err)
			return
		}
		writeNoContent(w, r)
	}
}

// NotificationPreferencesGet returns GET /api/v1/notification-preferences —
// every known kind with its effective push toggle.
func NotificationPreferencesGet(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := logging.From(r.Context())
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
			writeInternalError(w, r, logger, "notification preferences read failed", "error", err)
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
				writeInternalError(w, r, logger, "push test send failed", "error", err)
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
