package push

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// Sender fans out events to the recipient's subscribed devices through
// webpush-go (RFC 8291 encryption + VAPID signing). Constructed
// once in cmd/api; disabled sends are simply not wired (nil Notifier).
type Sender struct {
	db         *sql.DB
	subscriber string // VAPID sub claim
	publicKey  string // VAPID public key — the frontend's applicationServerKey
	privateKey string // VAPID private key — compose secret vapid_private_key
	// backoff returns the sleep before retry attempt N (N = 1..maxAttempts-1).
	// The default is the production 1 s/2 s ladder; tests inject a zero
	// backoff so the retry pins do not sleep real time.
	backoff func(attempt int) time.Duration
}

// NewSender builds a Sender. subscriber is the VAPID `sub` contact URI;
// publicKey/privateKey are the URL-safe base64 VAPID keypair.
func NewSender(db *sql.DB, publicKey, privateKey, subscriber string) *Sender {
	return &Sender{
		db:         db,
		subscriber: subscriber,
		publicKey:  publicKey,
		privateKey: privateKey,
		backoff: func(attempt int) time.Duration {
			return time.Duration(attempt) * time.Second
		},
	}
}

// NotifyPush queues one committed notification event for delivery. It
// returns immediately — the request's transaction is already committed, so
// the fan-out never blocks the response. The
// request context travels into the goroutine for LOGGING only (request-ID
// correlation); the fan-out's timeouts and database calls use a fresh
// bounded context — a cancelled request must never cancel the delivery
// that follows its own commit.
func (s *Sender) NotifyPush(ctx context.Context, ev Event) {
	if ev.RecipientID == "" || ev.Kind == "" {
		return
	}
	logging.From(ctx).DebugContext(ctx, "push fan-out queued", "kind", ev.Kind)
	go s.deliver(ctx, ev)
}

// deliver runs the full fan-out for one event: load the recipient's
// effective preference (row else the kind's default), load the subscription
// rows, and send to each under a small concurrency cap. logCtx is the
// originating request context (logs only); the work runs under a fresh
// bounded context.
func (s *Sender) deliver(logCtx context.Context, ev Event) {
	ctx, cancel := context.WithTimeout(context.Background(), deliverTimeout)
	defer cancel()

	u, err := store.UserByID(ctx, s.db, ev.RecipientID)
	if err != nil {
		logging.From(logCtx).ErrorContext(logCtx, "push recipient lookup failed", "error", err)
		return
	}
	// A suspended recipient is skipped: the emitters already exclude one at
	// commit time, and this re-check narrows the window between a commit and
	// the delivery goroutine (or a retry) — suspension is a full revocation.
	if u.Status != identity.StatusActive {
		logging.From(logCtx).DebugContext(logCtx, "push recipient not active", "kind", ev.Kind)
		return
	}
	// The kind default is the handler's one source, identity.CanModerateContent:
	// moderator+ default ON for the audience kinds, BUT the draft notice is
	// opt-in for EVERY role. The default only applies when no preference row
	// exists; a draft event always carries one (its recipient query requires
	// it), so this arm is the guarded fallback after a row vanishes.
	defaultOn := identity.CanModerateContent(u.Role) && ev.Kind != "draft_activity"
	enabled, err := store.EffectivePushEnabled(ctx, s.db, ev.RecipientID, ev.Kind, defaultOn)
	if err != nil {
		logging.From(logCtx).ErrorContext(logCtx, "push preference lookup failed", "error", err)
		return
	}
	if !enabled {
		return
	}

	subs, err := store.PushSubscriptionsForUser(ctx, s.db, ev.RecipientID)
	if err != nil {
		logging.From(logCtx).ErrorContext(logCtx, "push subscription load failed", "error", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	body, dropped, err := payloadFor(ev)
	if err != nil {
		logging.From(logCtx).ErrorContext(logCtx, "push payload marshal failed", "error", err)
		return
	}
	if dropped > 0 {
		// The title was shortened to keep the body inside the bridged limit.
		// Logged WITHOUT the title: the kind locates the content,
		// the byte count records the magnitude of the cut.
		logging.From(logCtx).WarnContext(logCtx, "push payload title trimmed", "kind", ev.Kind, "titleBytesDropped", dropped)
	}

	sem := make(chan struct{}, concurrencyCap)
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			s.sendOne(logCtx, ctx, ev, sub, body)
		})
	}
	wg.Wait()
}

// sendOne delivers one message with retries and the dead-endpoint rule:
// 404/410 deletes the row — the subscription is gone at the push service;
// 429/5xx retry with backoff; every other status is logged and dropped.
// Logs carry the subscription id, the push-service FAMILY and the
// status/class only — never the endpoint, and network errors are logged by
// CLASS (the raw error embeds the endpoint URL).
//
// It reports the endpoint's outcome — the self-test hands the whole report
// back to the caller; the feed fan-out ignores it.
func (s *Sender) sendOne(logCtx, ctx context.Context, ev Event, sub store.PushSubscriptionKeys, body []byte) DeliveryOutcome {
	service := pushServiceFamily(sub.Endpoint)
	outcome := DeliveryOutcome{SubscriptionID: sub.ID, Service: service}
	options := &webpush.Options{
		HTTPClient: &http.Client{
			Timeout: requestTimeout,
			// A subscription endpoint is user-supplied: refusing redirects keeps
			// it from aiming the send at a SECOND host (a 307/308 would re-POST
			// to an internal target). A 3xx stays a 3xx and the default arm below
			// logs and drops it.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		Subscriber: s.subscriber,
		TTL:        ttlSeconds(ev.Kind),
		Topic:      ev.ContentID, // collapse a burst on one content
		// The ECE record size IS the request body size (see recordSizeFor).
		RecordSize:      recordSizeFor(len(body)),
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
	}
	target := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{Auth: sub.Auth, P256dh: sub.P256dh},
	}

	var lastStatus int
	for attempt := range maxAttempts {
		if attempt > 0 {
			// Backoff between attempts; ctx bounds the total window.
			select {
			case <-ctx.Done():
				logging.From(logCtx).WarnContext(logCtx, "push delivery aborted", "subscriptionId", sub.ID, "kind", ev.Kind, "service", service)
				outcome.Status = lastStatus
				return outcome
			case <-time.After(s.backoff(attempt)):
			}
		}

		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		resp, err := webpush.SendNotificationWithContext(reqCtx, body, target, options)
		cancel()
		if err != nil {
			// Network-level failure — retryable, like a 5xx. Log the CLASS,
			// not the error: webpush-go's errors embed the endpoint URL
			// (the subscription id is the only identifier that
			// may reach a log line).
			logging.From(logCtx).WarnContext(logCtx, "push delivery request failed", "subscriptionId", sub.ID, "kind", ev.Kind, "service", service, "class", pushErrorClass(err))
			continue
		}
		lastStatus = resp.StatusCode
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK, http.StatusCreated:
			outcome.Accepted = true
			outcome.Status = resp.StatusCode
			return outcome
		case http.StatusNotFound, http.StatusGone:
			// Dead endpoint — the push service no longer knows it. Delete
			// under a short FRESH timeout: the row must not survive to be
			// retried on the next event, and the delete must not outlive
			// the fan-out's own window.
			deleteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := store.DeletePushSubscription(deleteCtx, s.db, ev.RecipientID, sub.ID)
			cancel()
			outcome.Status = resp.StatusCode
			if err != nil {
				logging.From(logCtx).ErrorContext(logCtx, "push dead-subscription cleanup failed", "subscriptionId", sub.ID, "service", service, "error", err)
			} else {
				outcome.Removed = true
				logging.From(logCtx).InfoContext(logCtx, "push subscription removed (dead endpoint)", "subscriptionId", sub.ID, "kind", ev.Kind, "service", service)
			}
			return outcome
		case http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			continue
		default:
			logging.From(logCtx).WarnContext(logCtx, "push delivery rejected", "subscriptionId", sub.ID, "kind", ev.Kind, "service", service, "status", lastStatus)
			outcome.Status = lastStatus
			return outcome
		}
	}

	logging.From(logCtx).WarnContext(logCtx, "push delivery failed", "subscriptionId", sub.ID, "kind", ev.Kind, "service", service, "status", lastStatus, "attempts", maxAttempts)
	outcome.Status = lastStatus
	return outcome
}

// SendTest delivers one test message to every device the user has
// subscribed and reports how many endpoints ACCEPTED it (200/201) together
// with one outcome per endpoint. Anything but
// 200/201 — including a network failure or an exhausted retry ladder — is not
// a delivery, so zero can also mean "devices exist, none accepted", the
// diagnosis the OpenAPI wording names. The per-endpoint list is what lets the
// caller tell whether THEIR OWN device was the refusal: the aggregate count
// alone cannot, since a refused device may sit beside devices that accepted.
//
// Unlike NotifyPush it is SYNCHRONOUS: the settings button renders the
// result, so the send happens inside the request, bounded by
// testDeliverTimeout (below the server's write deadline) and by the
// caller's own context. The preference gate is deliberately absent: the
// user asked for this message,
// so the per-kind toggles — which answer "may this event reach me" — do not
// apply. Dead endpoints are cleaned up by the send path exactly as in the
// fan-out (a device that the push service no longer knows disappears from
// the settings list, and its outcome is reported as removed).
func (s *Sender) SendTest(ctx context.Context, userID string) (TestReport, error) {
	subs, err := store.PushSubscriptionsForUser(ctx, s.db, userID)
	if err != nil {
		return TestReport{}, err
	}
	report := TestReport{Endpoints: make([]DeliveryOutcome, 0, len(subs))}
	if len(subs) == 0 {
		return report, nil
	}

	body, _, err := payloadFor(Event{Kind: testKind})
	if err != nil {
		return TestReport{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, testDeliverTimeout)
	defer cancel()

	// RecipientID is what the dead-endpoint cleanup scopes its delete to —
	// the test event must carry it like any fan-out event.
	ev := Event{RecipientID: userID, Kind: testKind}
	sem := make(chan struct{}, concurrencyCap)
	var wg sync.WaitGroup
	// Outcomes are written by INDEX: one goroutine per subscription, one
	// slot each — the report keeps the store's order and needs no mutex.
	outcomes := make([]DeliveryOutcome, len(subs))
	for i, sub := range subs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			// The self-test has only ONE context — the request's, which is
			// also the log context here (the fan-out keeps them apart).
			outcomes[i] = s.sendOne(ctx, ctx, ev, sub, body)
		})
	}
	wg.Wait()

	for _, o := range outcomes {
		if o.Accepted {
			report.Delivered++
		}
		report.Endpoints = append(report.Endpoints, o)
	}
	return report, nil
}

// pushErrorClass reduces a webpush-go error to a loggable class without
// the embedded endpoint URL. DeadlineExceeded is the one distinguishable
// class worth its own label; everything else is "network".
func pushErrorClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "network"
}

// compile-time check that Sender satisfies the seams.
var (
	_ Notifier   = (*Sender)(nil)
	_ TestSender = (*Sender)(nil)
)
