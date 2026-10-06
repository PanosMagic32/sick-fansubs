// Package push delivers web push notifications: the fan-out sender for the
// feed's user_notifications events.
//
// Push is a DELIVERY CHANNEL, not a pipeline — the in-app feed row is the
// source of truth; the sender only transports. The fan-out runs after the
// emitting transaction commits (the handler hands the event over after the
// store call returns), so a pushed message can never exist for an event
// that did not land, and a crashed fan-out loses nothing (the feed still
// carries the row — push is at-most-once best effort).
//
// No queue infrastructure: tens of users, small concurrency cap,
// per-endpoint error handling, TTL bounds staleness at the push services.
package push

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Event is the fan-out input — one committed user_notifications row (the
// self-test's synthetic event is the exception: it has no row — see Kind).
// The handler fills ActorUsername from the session (the actor is the
// requester); ContentTitle comes from the emitting store transaction's gate
// query. comment_removed is the exception: its notice is actorless by design,
// so ActorUsername stays empty.
type Event struct {
	RecipientID   string // the user the notice is for
	Kind          string // "heart" | "comment_reply" | "comment" | "content_updated" | "new_content" | "comment_removed" | "draft_activity", or testKind for the settings-button self-test (which has no feed row)
	ActorUsername string
	ContentTitle  string
	ContentKind   string // "blog-posts" | "projects"
	ContentID     string // the content the notice points at
	CommentID     string // the comment for the comment kinds; empty otherwise
	// DraftAction names the unpublished transition a draft_activity event
	// reports ("created" | "updated" | "unpublished") and is carried to the
	// worker as draftAction; empty for every other kind.
	DraftAction string
}

// Notifier is the handler seam (the mail.Sender pattern): a nil Notifier
// means push is disabled (development without the VAPID secret, and tests).
type Notifier interface {
	NotifyPush(ctx context.Context, ev Event)
}

// TestSender is the handler seam for the self-test push (the account page's
// settings button): one message to the caller's OWN devices, with the
// per-endpoint outcome report as the result. A nil TestSender means delivery
// is disabled (development without the VAPID secret, and tests) — the handler
// reports zero instead of failing.
type TestSender interface {
	// SendTest reports how many endpoints ACCEPTED the message (200/201),
	// plus one outcome per endpoint (the caller must be able to tell whether
	// THEIR device was the one refused — the aggregate count alone hid
	// exactly that). The error is a store or marshal failure only: delivery
	// failures are logged by class inside the send path and never returned
	// (an error here would carry no endpoint, but the handler's generic 500
	// path must not become a delivery channel either).
	SendTest(ctx context.Context, userID string) (TestReport, error)
}

// testKind is the self-test message class.
// It is NOT a feed kind: nothing emits it into user_notifications, it has no
// notification_preferences row, and the service worker renders it from its
// own template. It rides the ordinary send path so the test really exercises
// VAPID signing, the stored subscription, and the worker.
const testKind = "test"

// deliveryTimeouts bound the delivery paths: each endpoint request, the
// retry backoff steps, the whole fan-out call (which a handler never waits
// on — NotifyPush spawns the goroutine), and the SELF-TEST send, which runs
// inside the user's request and therefore gets its own shorter bound.
const (
	requestTimeout = 10 * time.Second
	deliverTimeout = 60 * time.Second
	maxAttempts    = 3 // 1 initial + 2 retries
	concurrencyCap = 4

	// testDeliverTimeout bounds the SELF-TEST send, which runs inside the
	// request the user is waiting on. The server's WriteTimeout is 30 s
	// (cmd/api), and one endpoint can spend 3 attempts × requestTimeout plus
	// backoff — so the interactive path gets its own, shorter bound. The
	// context deadline stops the retry ladder mid-way; a press the user can
	// simply repeat does not need the fan-out's full window.
	testDeliverTimeout = 20 * time.Second
)

// ECE record sizing.
// webpush-go encrypts the payload into ONE aes128gcm record and then PADS it
// up to Options.RecordSize — the option is the request-body size, not a cap.
// Its default is MaxRecordSize (4096), and a 4096-byte body is rejected with
// 413 by Mozilla autopush for every BRIDGED (FCM/APNs) subscription: bridged
// connections are limited to about 2744 bytes because the data is
// base64-transcoded, while direct connections allow 4028. That padding is what
// blacked out Firefox for Android for every kind while desktop
// Firefox, Chrome and Apple delivery kept working. See recordSizeFor.
const (
	// payloadFraming is what ECE adds around the payload bytes themselves:
	// an 86-byte content header (salt 16, record size 4, key id 1, public
	// key 65), a 1-byte padding delimiter, and the 16-byte AEAD tag.
	payloadFraming = 86 + 1 + 16
	// minRecordSize is the body floor: it also keeps length-hiding padding
	// for the typical payload, which is well under this.
	minRecordSize = 1024
	// recordOverhead is the slack the composed record leaves above the
	// marshalled payload — comfortably more than payloadFraming, so the
	// library's pad() never fails on a payload that fits.
	recordOverhead = 256
	// maxBridgedRecordSize is the ceiling the composed body must stay under:
	// Mozilla autopush documents ~2744 bytes for bridged connections. Every
	// payload the app can build fits well below it; the only way past it is an
	// imported legacy title, which payloadFor trims.
	maxBridgedRecordSize = 2688
	// maxPayloadBytes is the marshalled body budget that keeps the composed
	// record under the ceiling above; payloadFor enforces it.
	maxPayloadBytes = maxBridgedRecordSize - recordOverhead
)

// recordSizeFor returns the ECE record size for one marshalled payload: the
// body webpush-go will actually POST. Keeping it just above the payload costs
// nothing on the wire and is what makes bridged (FCM/APNs) delivery possible.
func recordSizeFor(payloadLen int) uint32 {
	size := max(payloadLen+recordOverhead, minRecordSize)
	return uint32(size)
}

// payloadFor marshals the worker-facing body for one event.
// The JSON keys ARE the service worker's contract; the marshalled
// length is also what sizes the ECE record, so the fan-out and the self-test
// must build the body the SAME way — one function, one size.
//
// A title long enough to compose a record past maxBridgedRecordSize is cut
// back to the LONGEST rune prefix that fits (a binary search, ~log2(runes)
// marshals — imported legacy titles are unbounded in the schema, and an
// oversized body is refused with 413 by every push service: the silent-loss
// class this bound closes). The returned count is the bytes the
// accepted title lost, so the caller can log the magnitude without ever
// logging the title itself. Every other field is bounded, so the
// over-budget escape below is unreachable in practice.
func payloadFor(ev Event) (body []byte, titleBytesDropped int, err error) {
	body, err = marshalPayload(ev)
	if err != nil || len(body) <= maxPayloadBytes || ev.ContentTitle == "" {
		return body, 0, err
	}

	full := ev.ContentTitle
	runes := []rune(full)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		ev.ContentTitle = string(runes[:mid])
		candidate, mErr := marshalPayload(ev)
		if mErr != nil {
			return nil, 0, mErr
		}
		if len(candidate) <= maxPayloadBytes {
			lo, body = mid, candidate
		} else {
			hi = mid - 1
		}
	}
	ev.ContentTitle = string(runes[:lo])
	return body, len(full) - len(ev.ContentTitle), nil
}

// marshalPayload is the one place the event-to-body mapping lives (the JSON
// keys are the worker's contract — pinned by TestPayloadJSONKeys). It is
// called repeatedly by payloadFor's trim search, which is why it is not inlined
// into the loop.
func marshalPayload(ev Event) ([]byte, error) {
	return json.Marshal(payload{
		Kind:          ev.Kind,
		ActorUsername: ev.ActorUsername,
		ContentTitle:  ev.ContentTitle,
		ContentKind:   ev.ContentKind,
		ContentID:     ev.ContentID,
		CommentID:     ev.CommentID,
		DraftAction:   ev.DraftAction,
	})
}

// defaultTTLSeconds is the push-service retention for the reaction and
// follow kinds: 24 h — the event's value decays; the in-app feed is the
// durable copy. new_content carries its own number — see ttlSeconds.
const defaultTTLSeconds = 86400

// newContentTTLSeconds is the push-service retention for the new_content
// broadcast: 7 days — a publish's value decays slower than a reaction's.
const newContentTTLSeconds = 604800

// ttlSeconds returns the TTL header value per message class: every kind
// except new_content keeps the 24 h default — comment_removed rides the same
// value-decay class.
func ttlSeconds(kind string) int {
	if kind == "new_content" {
		return newContentTTLSeconds
	}
	return defaultTTLSeconds
}

// payload is the push message body: the data the
// service worker composes the Greek sentence from — never finished copy.
// The JSON keys ARE the contract (the worker reads contentId, not
// contentID) — pinned by TestPayloadJSONKeys. CommentID is OMITTED when
// empty: content_updated and new_content carry no comment (the content is the
// deep-link target). DraftAction is OMITTED the same way, for every kind
// except draft_activity.
type payload struct {
	Kind          string `json:"kind"`
	ActorUsername string `json:"actorUsername"`
	ContentTitle  string `json:"contentTitle"`
	ContentKind   string `json:"contentKind"`
	ContentID     string `json:"contentId"`
	CommentID     string `json:"commentId,omitempty"`
	DraftAction   string `json:"draftAction,omitempty"`
}

// DeliveryOutcome is one endpoint's result inside a self-test report.
// It carries the row id, the coarse push-service
// FAMILY, and the transport verdict — never the endpoint and never key
// material: the family is the only device detail that leaves the sender.
type DeliveryOutcome struct {
	SubscriptionID string // the stored row the outcome belongs to
	Service        string // pushServiceFamily: mozilla | fcm | apple | other
	Accepted       bool   // the push service answered 200/201
	// Status is the LAST response status the push service returned, sticky
	// across retries; 0 when no attempt ever received a response (a pure
	// transport failure or a window the caller cancelled).
	Status  int
	Removed bool // the dead-endpoint rule deleted the row
}

// TestReport is the self-test result:
// how many endpoints accepted the message, plus one outcome per attempted
// endpoint in the store's order. On the success path Endpoints is never nil —
// an empty list means nothing is subscribed; the handler normalizes an error
// path's zero value to an empty array before it reaches the wire.
type TestReport struct {
	Delivered int // how many endpoints accepted (200/201)
	Endpoints []DeliveryOutcome
}

// Push-service families. The vocabulary is
// closed and coarse ON PURPOSE: it tells a human which push service refused a
// device (which is the whole point of the report) without publishing the
// endpoint, its path, or its token.
const (
	serviceMozilla = "mozilla"
	serviceFCM     = "fcm"
	serviceApple   = "apple"
	serviceOther   = "other"
)

// pushServiceFamily classifies a stored endpoint by its host. An endpoint that
// does not parse (impossible through the create endpoint, which validates
// https + host) is "other" rather than an error: the report must never fail
// because a label could not be derived. The families are the push services a
// subscription can realistically point at — Chrome/Chromium endpoints live
// under googleapis.com, Mozilla's under mozilla.com (current) or mozaws.net
// (legacy), and Apple's under push.apple.com.
func pushServiceFamily(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return serviceOther
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case strings.HasSuffix(host, ".mozilla.com") || strings.HasSuffix(host, ".mozaws.net"):
		return serviceMozilla
	case strings.HasSuffix(host, ".googleapis.com"):
		return serviceFCM
	case strings.HasSuffix(host, ".push.apple.com"):
		return serviceApple
	default:
		return serviceOther
	}
}
