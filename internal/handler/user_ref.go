package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"sick-fansubs/internal/store"
)

// uploaderFallbackUsernameCanon names the account behind public content
// bylines with no uploader avatar (internal/handler/AGENTS.md): a ref
// without an avatar borrows its picture, an absent ref becomes the account
// as a whole. The lookup is exact-canonical — a renamed, deleted, or
// canon-mismatched account degrades to the neutral rendering — and account
// status does not suppress the fallback (suspension is not erasure).
const uploaderFallbackUsernameCanon = "kushoyarou"

// userRefDTO is the public user projection on content: id, username, and
// avatar (nullable). Role is deliberately absent — public content must not
// reveal which users hold privileged roles.
type userRefDTO struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	AvatarURL *string `json:"avatarUrl"`
}

// userRefDTOFrom projects a store user ref to the wire. Nil stays nil — the
// JSON emits null, never a fake user.
func userRefDTOFrom(publicBaseURL string, ref *store.UserRef) *userRefDTO {
	if ref == nil {
		return nil
	}
	return &userRefDTO{
		ID:        ref.ID,
		Username:  ref.Username,
		AvatarURL: avatarURLOrNull(publicBaseURL, ref.AvatarURL),
	}
}

// uploaderFallback resolves the fallback account once per public content
// read. ErrNotFound degrades to no fallback — the byline keeps its neutral
// rendering rather than failing the read.
func uploaderFallback(ctx context.Context, db *sql.DB) (*store.UserRef, error) {
	ref, err := store.UserRefByCanonical(ctx, db, uploaderFallbackUsernameCanon)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ref, nil
}

// requireUploaderFallback wraps uploaderFallback for the public content
// handlers: a store failure answers the standard masked 500 and false.
func requireUploaderFallback(w http.ResponseWriter, r *http.Request, db *sql.DB) (*store.UserRef, bool) {
	ref, err := uploaderFallback(r.Context(), db)
	if err != nil {
		writeStoreError(w, r, err, "uploader fallback lookup", "error", err)
		return nil, false
	}
	return ref, true
}

// contentRefFrom projects a public content byline ref through the uploader
// fallback: a ref with no avatar — including one the scheme guard rejects —
// keeps its identity and borrows the fallback picture; an absent ref becomes
// the fallback account as a whole. Without a fallback account both cases
// keep the neutral rendering.
func contentRefFrom(publicBaseURL string, ref, fallback *store.UserRef) *userRefDTO {
	if ref == nil {
		ref = fallback
	}
	if ref == nil {
		return nil
	}
	dto := userRefDTOFrom(publicBaseURL, ref)
	if dto.AvatarURL == nil && fallback != nil {
		dto.AvatarURL = avatarURLOrNull(publicBaseURL, fallback.AvatarURL)
	}
	return dto
}
