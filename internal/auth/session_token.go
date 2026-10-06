package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"sick-fansubs/internal/id"
	"sick-fansubs/internal/identity"
	"sick-fansubs/internal/store"
)

// generateSessionToken creates a new 32-byte random session token.
//
// Returns:
//   - raw: 32 random bytes (never stored or logged)
//   - encoded: unpadded base64url of raw (goes in the sf_session cookie)
//   - digest: SHA-256 of raw (stored in sessions.token_digest)
func generateSessionToken() (raw []byte, encoded string, digest []byte, err error) {
	raw = make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", nil, fmt.Errorf("generate session token: %w", err)
	}
	encoded = base64.RawURLEncoding.EncodeToString(raw)
	h := sha256.Sum256(raw)
	digest = h[:]
	return raw, encoded, digest, nil
}

// generateCSRFToken creates a new 32-byte random CSRF token.
//
// Returns:
//   - raw: 32 random bytes (stored in sessions.csrf_token)
//   - encoded: unpadded base64url of raw (returned in response body,
//     sent by the client in the X-CSRF-Token header)
func generateCSRFToken() (raw []byte, encoded string, err error) {
	raw = make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("generate csrf token: %w", err)
	}
	encoded = base64.RawURLEncoding.EncodeToString(raw)
	return raw, encoded, nil
}

// sessionMaterial bundles the freshly generated randomness and timestamps
// needed to persist one session. Generation is kept separate from the store
// write so the same material can feed CreateSession,
// RehashPasswordAndCreateSession, or CreateUserAndSession.
type sessionMaterial struct {
	id             string
	encodedSession string
	digest         []byte
	rawCSRF        []byte
	encodedCSRF    string
	nowMS          int64
}

// generateSessionMaterial creates a new session ID, session token, and CSRF
// token. No database access — the caller decides which store operation
// persists them.
func generateSessionMaterial() (sessionMaterial, error) {
	_, encodedSession, digest, err := generateSessionToken()
	if err != nil {
		return sessionMaterial{}, err
	}
	rawCSRF, encodedCSRF, err := generateCSRFToken()
	if err != nil {
		return sessionMaterial{}, err
	}
	sessionID, err := id.New()
	if err != nil {
		return sessionMaterial{}, fmt.Errorf("auth: generate session id: %w", err)
	}
	return sessionMaterial{
		id:             sessionID,
		encodedSession: encodedSession,
		digest:         digest,
		rawCSRF:        rawCSRF,
		encodedCSRF:    encodedCSRF,
		nowMS:          time.Now().UnixMilli(),
	}, nil
}

// createSessionAfterSignIn persists a sign-in session. When a rehash is
// pending, the verifier update (which bumps auth_version) and the session
// insert run in ONE store transaction under the new version; otherwise the
// plain conditional CreateSession is used.
//
// clientLabel is the classifier's bounded device label;
// the empty string stores SQL NULL.
func (s *Service) createSessionAfterSignIn(ctx context.Context, u *identity.User, rehashNeeded bool, newHash, clientLabel string) (sessionToken, csrfToken string, err error) {
	m, err := generateSessionMaterial()
	if err != nil {
		return "", "", err
	}
	if rehashNeeded {
		err = store.RehashPasswordAndCreateSession(ctx, s.db, store.RehashPasswordAndCreateSessionParams{
			UserID:             u.ID,
			CurrentAuthVersion: u.AuthVersion,
			NewHash:            newHash,
			UpdatedAtMS:        m.nowMS,
			SessionID:          m.id,
			TokenDigest:        m.digest,
			CSRF:               m.rawCSRF,
			ClientLabel:        clientLabel,
			CreatedAtMS:        m.nowMS,
			ExpiresAtMS:        m.nowMS + SessionLifetime.Milliseconds(),
		})
	} else {
		err = store.CreateSession(ctx, s.db, store.CreateSessionParams{
			ID:          m.id,
			UserID:      u.ID,
			TokenDigest: m.digest,
			CSRF:        m.rawCSRF,
			AuthVersion: u.AuthVersion,
			ClientLabel: clientLabel,
			CreatedAtMS: m.nowMS,
			ExpiresAtMS: m.nowMS + SessionLifetime.Milliseconds(),
		})
	}
	if err != nil {
		return "", "", err
	}
	return m.encodedSession, m.encodedCSRF, nil
}
