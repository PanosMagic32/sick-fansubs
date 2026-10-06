package routes

import (
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/middleware"
)

// Auth registers all authentication endpoints under /api/v1/auth/.
//
// Routes:
//
// POST /api/v1/auth/sign-in         — authenticate, create session
// GET  /api/v1/auth/session         — current session bootstrap
// POST /api/v1/auth/sign-out        — delete current session
// POST /api/v1/auth/sign-out-all    — revoke all sessions
// POST /api/v1/auth/register        — create account + auto sign-in
// PUT  /api/v1/auth/password        — change password, revoke sessions
// POST /api/v1/auth/forgot-password — email a reset link
// POST /api/v1/auth/reset-password  — consume a reset token
// POST /api/v1/auth/verify-email    — consume a verification token
// POST /api/v1/auth/resend-verification — re-send the verification email
//
// Chain: authChain (RequestID → TrustedOrigin → the path-scoped IP limits →
// Session → CSRF). The forced-change gate is absent by design: the auth
// subtree is where the exempt paths live.
//
// Rate limits:
//   - Sign-in:  10/IP (middleware) + 5/identifier (handler), 15-minute window
//   - Register: 3/IP (middleware), 1-hour window
//   - Password: 5/user (handler), 15-minute window
//   - Forgot:   3/IP (middleware) + 3/identifier (handler), 1-hour window
//   - Reset:    5/IP (middleware), 15-minute window (bcrypt-burn bound)
//   - Verify:   5/IP (middleware), 15-minute window (hygiene: the
//     token is unguessable and there is no bcrypt burn)
//   - Resend:   1/user/30min (handler, the shared verification-send bucket —
//     constructed once in cmd/api and passed to BOTH
//     route trees so every verification-send surface shares one limit)
//   - Email change: same shared bucket, checked in the handler.
func Auth(mux *http.ServeMux, db *sql.DB, secure bool, trustedOrigin, publicBaseURL string, sender mail.Sender, verifySendLimiter *middleware.RateLimiter) {
	authSvc := auth.New(db, sender, publicBaseURL)

	// Rate limiters (in-memory, reset on restart).
	signInIPLimiter := middleware.NewRateLimiter(10, 15*time.Minute, time.Minute)
	registerIPLimiter := middleware.NewRateLimiter(3, time.Hour, time.Minute)
	forgotIPLimiter := middleware.NewRateLimiter(3, time.Hour, time.Minute)
	resetIPLimiter := middleware.NewRateLimiter(5, 15*time.Minute, time.Minute)
	verifyIPLimiter := middleware.NewRateLimiter(5, 15*time.Minute, time.Minute)
	signInIDLimiter := middleware.NewRateLimiter(5, 15*time.Minute, time.Minute)
	pwChangeLimiter := middleware.NewRateLimiter(5, 15*time.Minute, time.Minute)
	forgotIDLimiter := middleware.NewRateLimiter(3, time.Hour, time.Minute)

	authHandler := handler.NewAuth(authSvc, secure, signInIDLimiter, pwChangeLimiter, forgotIDLimiter, verifySendLimiter)

	// Build the auth sub-mux with stripped paths.
	authMux := http.NewServeMux()
	authMux.HandleFunc("POST /sign-in", authHandler.HandleSignIn)
	authMux.HandleFunc("GET /session", authHandler.HandleSession)
	authMux.HandleFunc("POST /sign-out", authHandler.HandleSignOut)
	authMux.HandleFunc("POST /sign-out-all", authHandler.HandleSignOutAll)
	authMux.HandleFunc("POST /register", authHandler.HandleRegister)
	authMux.HandleFunc("PUT /password", authHandler.HandlePasswordChange)
	authMux.HandleFunc("POST /forgot-password", authHandler.HandleForgotPassword)
	authMux.HandleFunc("POST /reset-password", authHandler.HandleResetPassword)
	authMux.HandleFunc("POST /verify-email", authHandler.HandleVerifyEmail)
	authMux.HandleFunc("POST /resend-verification", authHandler.HandleResendVerification)
	// Unknown paths inside the auth namespace must return the JSON 404
	// problem, not the ServeMux default text/plain response (unknown API
	// paths never produce a non-API body).
	authMux.Handle("/", notFound())

	h := authChain(authMux, db, secure, trustedOrigin,
		pathLimit{signInIPLimiter, "/sign-in"},
		pathLimit{registerIPLimiter, "/register"},
		pathLimit{forgotIPLimiter, "/forgot-password"},
		pathLimit{resetIPLimiter, "/reset-password"},
		pathLimit{verifyIPLimiter, "/verify-email"},
	)

	mux.Handle("/api/v1/auth/", http.StripPrefix("/api/v1/auth", h))
}
