package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"sick-fansubs/internal/database"
	"sick-fansubs/internal/store"
	"sick-fansubs/internal/store/storetest"
)

// testDataDir returns an owner-only temporary directory for tests that open a
// database: database.Open refuses a data directory with group or other access,
// and t.TempDir can inherit wider bits from the environment.
func testDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod test data dir: %v", err)
	}
	return dir
}

func pushFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(database.Config{DataDir: testDataDir(t)})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.Apply(db); err != nil {
		db.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedUser inserts a user row through the shared fixture — the push tests
// need only the id and role (the preference default), nothing else
// user-shaped.
func seedUser(t *testing.T, db *sql.DB, id, role string) {
	t.Helper()
	storetest.InsertUser(t, db, storetest.UserSpec{
		ID:       id,
		Username: "u-" + id,
		Email:    id + "@example.com",
		Role:     role,
	})
}

// testKeypair generates a throwaway VAPID pair for one test (the same
// shape the bring-up generator produces: URL-safe base64, 65-byte point /
// 32-byte scalar).
func testKeypair(t *testing.T) (public, private string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	public = base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), priv.PublicKey.X, priv.PublicKey.Y))
	private = base64.RawURLEncoding.EncodeToString(priv.D.Bytes())
	return public, private
}

// capturedRequest is what one fake-service request proves: the VAPID
// Authorization header shape, the TTL, the topic, and a non-empty body.
type capturedRequest struct {
	authorization string
	ttl           string
	topic         string
	bodyLen       int
}

type fakePushService struct {
	t        *testing.T
	mu       sync.Mutex
	requests []capturedRequest
	statuses []int // consumed in order; the last one repeats
}

func newFakePushService(t *testing.T, statuses ...int) (*fakePushService, *httptest.Server) {
	t.Helper()
	fake := &fakePushService{t: t, statuses: statuses}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, capturedRequest{
			authorization: r.Header.Get("Authorization"),
			ttl:           r.Header.Get("TTL"),
			topic:         r.Header.Get("Topic"),
			bodyLen:       len(body),
		})
		idx := len(fake.requests) - 1
		if idx >= len(statuses) {
			idx = len(statuses) - 1 // the last status repeats
		}
		status := statuses[idx]
		fake.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return fake, srv
}

func (f *fakePushService) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// snapshot returns a copy of every captured request (the self-test asserts
// per-request headers, not just the newest one).
func (f *fakePushService) snapshot() []capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]capturedRequest(nil), f.requests...)
}

func (f *fakePushService) last() capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return capturedRequest{}
	}
	return f.requests[len(f.requests)-1]
}

func newTestSender(t *testing.T, db *sql.DB) *Sender {
	t.Helper()
	public, private := testKeypair(t)
	return NewSender(db, public, private, "https://sickfansubs.com")
}

// subscribeTo inserts one subscription row through the store (the same
// path the endpoint uses) pointing at the fake service's endpoint, with
// REAL key material — the encryption path validates the EC point before
// any request leaves the process.
func subscribeTo(t *testing.T, db *sql.DB, userID, endpoint string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate subscription key: %v", err)
	}
	p256dh := base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), priv.PublicKey.X, priv.PublicKey.Y))
	authBytes := make([]byte, 16)
	if _, err := rand.Read(authBytes); err != nil {
		t.Fatalf("generate auth secret: %v", err)
	}
	auth := base64.RawURLEncoding.EncodeToString(authBytes)
	// Row ids are unique per endpoint — the concurrency test seeds eight
	// devices for one user.
	sum := sha256.Sum256([]byte(endpoint))
	id := "sub-" + hex.EncodeToString(sum[:8])
	if _, err := store.UpsertPushSubscription(t.Context(), db,
		id, userID, endpoint, p256dh, auth, 1000); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
}

func heartEvent(recipient string) Event {
	return Event{
		RecipientID:   recipient,
		Kind:          "heart",
		ActorUsername: "actor",
		ContentTitle:  "Ένα επεισόδιο",
		ContentKind:   "blog-posts",
		ContentID:     "b1",
		CommentID:     "c1",
	}
}

// newContentEvent builds the new_content broadcast event: no comment (the
// content is the deep-link target).
func newContentEvent(recipient string) Event {
	return Event{
		RecipientID:   recipient,
		Kind:          "new_content",
		ActorUsername: "actor",
		ContentTitle:  "Νέο επεισόδιο",
		ContentKind:   "blog-posts",
		ContentID:     "b1",
	}
}

func draftActivityEvent(recipient string) Event {
	return Event{
		RecipientID:   recipient,
		Kind:          "draft_activity",
		ActorUsername: "actor",
		ContentTitle:  "Πρόχειρο επεισόδιο",
		ContentKind:   "blog-posts",
		ContentID:     "b1",
		DraftAction:   "created",
	}
}

// vapidClaims decodes the unsigned VAPID JWT's claim set (no signature
// verification — the fake service never checks it; the test pins the claim
// VALUES the sender builds).
func vapidClaims(t *testing.T, authorization string) map[string]any {
	t.Helper()
	parts := strings.SplitN(authorization, " ", 2)
	if len(parts) != 2 {
		t.Fatalf("Authorization shape: %q", authorization)
	}
	kv := strings.SplitN(parts[1], ",", 2) // "t=<jwt>, k=<key>"
	jwtPart := strings.TrimPrefix(kv[0], "t=")
	segments := strings.Split(jwtPart, ".")
	if len(segments) != 3 {
		t.Fatalf("JWT shape: %q", jwtPart)
	}
	raw, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}
