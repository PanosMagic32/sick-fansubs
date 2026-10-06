// Package middleware provides cross-cutting HTTP behavior for the
// Sick-Fansubs application.
//
// Middleware functions follow the standard Go pattern:
//
//	func M(args) func(http.Handler) http.Handler
//
// They own request inspection, context enrichment, and early rejection.
// They do not own business logic, SQL, or response body formatting.
package middleware

import (
	"context"

	"sick-fansubs/internal/identity"
)

// Context keys are unexported types to prevent collisions between packages.
type (
	ctxKeyRequestID struct{}
	ctxKeySession   struct{}
)

// SetRequestID attaches a server-generated request ID to the context.
func SetRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID{}, id)
}

// GetRequestID retrieves the request ID from the context.
// Returns "" if no request ID was set.
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID{}).(string)
	return id
}

// SetSession attaches an authenticated session to the context.
func SetSession(ctx context.Context, su *identity.SessionUser) context.Context {
	return context.WithValue(ctx, ctxKeySession{}, su)
}

// GetSession retrieves the authenticated session from the context.
// Returns nil if the request is unauthenticated.
func GetSession(ctx context.Context) *identity.SessionUser {
	su, _ := ctx.Value(ctxKeySession{}).(*identity.SessionUser)
	return su
}
