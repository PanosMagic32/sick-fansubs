// Package routes registers the HTTP routes and composes the middleware chains
// that front them. The chain shapes and their order are the pattern in
// docs/patterns/go/route-chains.md; each namespace's contracts are in
// internal/routes/AGENTS.md.
package routes

import (
	"net/http"

	"sick-fansubs/internal/handler"
)

// notFound is the JSON 404 fallback every namespace registers. It carries the
// public chain, so an unmatched path still answers with the same X-Request-ID
// header and problem requestId as a handled route — the handler package mints
// no ID of its own.
func notFound() http.Handler {
	return publicChain(http.HandlerFunc(handler.NotFound))
}

// APIFallback registers the /api/v1 catch-all handlers.
//
// Go's method-aware ServeMux gives more specific registered patterns (e.g.
// /api/v1/auth/sign-in) precedence over the trailing-slash pattern, so this
// only catches paths no endpoint owns. Unknown API paths must return a JSON
// problem — not the SPA HTML navigation response.
//
// Both "/api/v1" (exact) and "/api/v1/" (subtree) are registered: the
// exact pattern catches the namespace root itself, which the subtree
// pattern does not match.
func APIFallback(mux *http.ServeMux) {
	mux.Handle("/api/v1", notFound())
	mux.Handle("/api/v1/", notFound())
}
