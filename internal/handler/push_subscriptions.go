package handler

import (
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/id"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// Push subscription handlers: the caller's device rows under
// /api/v1/push-subscriptions — create/resubscribe, the settings list (the
// account page's device list), the per-row delete, and the per-row rename.
// The preference endpoints and the self-test send live in push.go.
//
// Reads ride the authenticated reads chain and writes the authenticated
// chain (the notifications feed pattern: trusted origin + session, writes
// additionally CSRF); none of these endpoints carries a rate limit — they
// are per-user origin+CSRF-protected operations (the favorites precedent).

// Subscription sizes: the endpoint URL bound, the RFC 8291 key-material
// bounds (p256dh decodes to a 65-byte uncompressed EC point, auth to the
// 16-byte authentication secret), and the device label bound.
const (
	maxPushEndpointLen = 512
	p256dhDecodedLen   = 65
	authDecodedLen     = 16
	// maxPushLabelRunes bounds a RENAMED device label in RUNES (the derived
	// classifier label is ASCII and far shorter). Runes, not bytes: a Greek
	// name of 32 characters is the same name of 64 bytes.
	maxPushLabelRunes = 32
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

// pushSubscriptionRenameRequest is the PATCH body. Label is a POINTER: nil
// distinguishes an absent field (422 required) from an empty string (also
// rejected — a device name must exist).
type pushSubscriptionRenameRequest struct {
	Label *string `json:"label"`
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

// pushSubscriptionItemDTO is one device row. Label is a POINTER so an
// unknown device is an explicit JSON null rather than an empty string — the
// client renders "unknown device" for null and shows the label verbatim
// otherwise (the sessions ClientLabel shape).
type pushSubscriptionItemDTO struct {
	ID        string  `json:"id"`
	Endpoint  string  `json:"endpoint"`
	Label     *string `json:"label"`
	CreatedAt string  `json:"createdAt"`
}

// PushSubscriptionCreate returns POST /api/v1/push-subscriptions —
// store/resubscribe one device subscription. 201 + {"id"}; the id is the
// stored row (the minted id on a fresh insert, the existing row's id on a
// same-endpoint resubscribe). An endpoint owned by another user is
// reassigned by the store. The row's device label is captured from THIS
// request's User-Agent through the sessions classifier (the raw header is
// never stored); a resubscribe keeps an existing label and only fills a
// missing one, so a rename survives the browser's rotation healing.
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
			endpoint, req.Keys.P256dh, req.Keys.Auth, auth.ClientLabel(r.UserAgent()), now().UnixMilli())
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
			var label *string
			if s.Label != "" {
				label = &s.Label
			}
			items = append(items, pushSubscriptionItemDTO{
				ID:        s.ID,
				Endpoint:  s.Endpoint,
				Label:     label,
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

// invalidLabelRune reports whether r may not appear in a device label:
// control characters and the invisible format family (Cf — zero-width
// spaces, bidi overrides). A Cf-only label survives TrimSpace, renders
// blank, and would masquerade as the unknown-device state; a bidi override
// can visually reorder the whole row. Comment bodies deliberately keep Cf
// (their contract preserves user prose verbatim); a device name is a short
// label read back in a list, so it does not.
func invalidLabelRune(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf)
}

// PushSubscriptionRename returns PATCH /api/v1/push-subscriptions/{id} —
// rename one of the caller's device rows. The label is trimmed, must be 1–32
// runes, and must be free of control and format characters (422 {field,
// code}) otherwise. An unknown or foreign id answers the masked 404 (both
// are the same answer — the row's existence is never disclosed), NOT the
// DELETE's idempotent 204: the delete's goal is the row's absence, which an
// absent row already satisfies, while a rename cannot succeed without its
// row.
func PushSubscriptionRename(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := requireSession(w, r, "push subscription rename requires authentication")
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid push subscription id")
			return
		}

		var req pushSubscriptionRenameRequest
		if err := readJSONLimit(w, r, &req, maxAuthBodySize); err != nil {
			writeJSONBodyError(w, r, err)
			return
		}
		label := ""
		if req.Label != nil {
			label = strings.TrimSpace(*req.Label)
		}
		var violations []Violation
		switch {
		case label == "":
			violations = append(violations, Violation{Field: "label", Code: "required"})
		case utf8.RuneCountInString(label) > maxPushLabelRunes:
			violations = append(violations, Violation{Field: "label", Code: "maxLength"})
		case strings.ContainsFunc(label, invalidLabelRune):
			violations = append(violations, Violation{Field: "label", Code: "invalidFormat"})
		}
		if len(violations) > 0 {
			writeValidationErrors(w, r, violations)
			return
		}

		if err := store.RenamePushSubscription(r.Context(), db, su.UserID, id, label); err != nil {
			writeStoreError(w, r, err, "push subscription rename", "error", err)
			return
		}
		writeNoContent(w, r)
	}
}
