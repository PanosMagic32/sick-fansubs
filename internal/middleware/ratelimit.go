package middleware

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter is an in-memory fixed-window rate limiter.
//
// It tracks hit counts per key within a fixed time window. When the
// window expires, the counter resets automatically on the next access.
// A background goroutine periodically cleans up expired entries to
// bound memory growth.
//
// Rate limits reset on process restart. This is
// acceptable for abuse prevention, not for strict quota enforcement.
type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateEntry
	window  time.Duration
	maxHits int
	stop    chan struct{}
}

type rateEntry struct {
	hits    int
	resetAt time.Time
}

// NewRateLimiter creates a rate limiter allowing at most maxHits within
// the given window. Expired entries are cleaned up every cleanupInterval.
func NewRateLimiter(maxHits int, window, cleanupInterval time.Duration) *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*rateEntry),
		window:  window,
		maxHits: maxHits,
		stop:    make(chan struct{}),
	}
	go rl.cleanupLoop(cleanupInterval)
	return rl
}

// stopCleanup stops the background cleanup goroutine. Allow keeps working;
// only the automatic removal of expired entries stops. Production limiters
// live for the process lifetime; tests call this to settle the goroutine.
func (rl *RateLimiter) stopCleanup() {
	close(rl.stop)
}

// Allow checks whether the given key is within the rate limit.
// If allowed, it increments the counter and returns (true, 0).
// If denied, it returns (false, retryAfter) where retryAfter is the
// remaining time until the window resets.
func (rl *RateLimiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	if rl == nil {
		// Fail closed: a silently skipped limiter would disable abuse protection.
		panic("middleware: nil RateLimiter")
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.entries[key]
	if !exists || now.After(entry.resetAt) {
		rl.entries[key] = &rateEntry{hits: 1, resetAt: now.Add(rl.window)}
		return true, 0
	}

	if entry.hits >= rl.maxHits {
		return false, time.Until(entry.resetAt)
	}

	entry.hits++
	return true, 0
}

// cleanupLoop periodically removes expired entries until stopCleanup.
func (rl *RateLimiter) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stop:
			return
		}
	}
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, entry := range rl.entries {
		if now.After(entry.resetAt) {
			delete(rl.entries, key)
		}
	}
}

// RateLimitIPByPath returns middleware that applies IP-based rate limiting
// only to requests whose URL path (after StripPrefix) matches one of the
// given paths. The client address comes from middleware.ClientAddr — the
// shared resolver the audit writers use too, so a bucket
// and an audit row can never disagree about who the client was.
//
// Intended for selectively applying IP limits to sign-in and registration
// without affecting session, sign-out, and password-change routes.
func RateLimitIPByPath(limiter *RateLimiter, paths ...string) func(http.Handler) http.Handler {
	pathSet := make(map[string]bool, len(paths))
	for _, p := range paths {
		pathSet[p] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pathSet[r.URL.Path] {
				ip := ClientAddr(r)
				allowed, retryAfter := limiter.Allow(ip)
				if !allowed {
					writeRateLimited(w, r, retryAfter)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
