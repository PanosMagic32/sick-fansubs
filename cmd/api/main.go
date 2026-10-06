// Command api runs the Sick-Fansubs HTTP server: startup and shutdown, the
// database open path, the periodic maintenance tasks, and the embedded
// frontend.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/config"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/mail"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/push"
	"sick-fansubs/internal/routes"
	"sick-fansubs/internal/store"
)

// Version is set at build time via ldflags:
//
//	go build -ldflags="-X main.Version=1.2.3"
//
// It is injected into the served frontend at serve time: the app-version
// meta tag and the service-worker version placeholder. The default "dev"
// value appears in local development.
var Version = "dev"

// pushSubscriber is the VAPID JWT `sub` contact URI (no mailto, no Gmail):
// the URI a push service can reach the operator
// at if the sender's traffic misbehaves.
const pushSubscriber = "https://sickfansubs.com"

// main is the application entry point. It owns three responsibilities:
//
// 1. Configuration — validated by internal/config at startup.
// 2. Wiring — calling route registration functions with concrete dependencies.
// 3. Lifecycle — starting the HTTP server and shutting it down gracefully.
//
// Route registration lives in internal/routes/ — each domain (auth, content,
// admin) exports a Register function that receives the mux and its dependencies.
// This keeps main.go focused on the process lifecycle; shutdown runs through
// [http.Server.Shutdown] and signals arrive via [signal.NotifyContext].
func main() {
	// Structured JSON logging to stderr; the rotated file sink is attached
	// below, once the data directory exists.
	stderrHandler := slog.NewJSONHandler(os.Stderr, logHandlerOptions)
	logger := slog.New(stderrHandler)
	slog.SetDefault(logger)

	// Load and validate configuration.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration validation failed", "error", err)
		os.Exit(1)
	}

	// API-surface mail requirements: production
	// host/from, loopback-only dev fallback. Separate from Load so the
	// break-glass maintenance commands that share the config are never
	// blocked by mail they do not send.
	if err := cfg.ValidateMailRelay(); err != nil {
		slog.Error("mail configuration validation failed", "error", err)
		os.Exit(1)
	}

	// Create the data directory (owner-only) before touching SQLite.
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		slog.Error("data directory create failed", "dataDir", cfg.DataDir, "error", err)
		os.Exit(1)
	}

	// Attach the rotated log FILE to the stderr sink: the same JSON records
	// on both, so the super-admin viewer can read
	// the tail without shelling into the host. Installed BEFORE any component
	// captures the default logger, and never fatal — a sink that cannot be
	// opened leaves stderr working.
	fileSink, closeLog, err := fileSinkHandler(cfg.DataDir, stderrHandler)
	if err != nil {
		logger.Warn("log file sink disabled", "error", err)
	}
	defer closeLog()
	slog.SetDefault(slog.New(fileSink))

	// Signal context is created BEFORE the cleanup task so the task can
	// observe shutdown (cleanup uses application cancellation and stops
	// during graceful shutdown).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Open the database through the strict application opener ONLY. The
	// serving process never runs migrations (docs/patterns/go/sqlite.md):
	// deployment runs cmd/migrate while the app is stopped, and this opener
	// fails closed on any schema drift.
	db, err := database.OpenApplication(database.Config{DataDir: cfg.DataDir})
	if err != nil {
		slog.Error("database open failed", "dataDir", cfg.DataDir, "error", err)
		os.Exit(1)
	}
	// Kept as the last-resort fallback on a return path; the os.Exit branches
	// below run no defers (the OS reclaims the descriptors), and the graceful
	// path closes explicitly so its error is reported.
	defer database.CloseDatabase(db)

	// Durable audit trail: register the SQLite writer once
	// at startup, before any request is served. Every audit event then
	// dual-writes — slog inside internal/audit, plus a row here. A table-write
	// failure is logged by the audit package and never fails a request.
	audit.SetWriter(&store.AuditWriter{DB: db})

	// Startup pass: delete already-expired session rows in bounded batches.
	// A failure here is non-fatal —
	// expired rows are never accepted by the session lookup, so cleanup is
	// hygiene, not correctness.
	startupCtx, cancelStartup := context.WithTimeout(ctx, startupPassBudget)
	n, cleanupErr := cleanupExpiredSessions(startupCtx, db)
	cancelStartup()
	if cleanupErr != nil {
		slog.Warn("startup expired-session cleanup failed", "error", cleanupErr)
	} else if n > 0 {
		slog.Info("startup expired-session cleanup", "deleted", n)
	}

	// Startup pass: sweep orphaned media files. A failure
	// here is non-fatal too — orphans are reclaimed on the next tick, and
	// the sweep is hygiene, not correctness.
	sweepCtx, cancelSweep := context.WithTimeout(ctx, startupPassBudget)
	stats, sweepErr := sweepMediaOrphans(sweepCtx, db, cfg.DataDir)
	cancelSweep()
	if sweepErr != nil {
		slog.Warn("startup media orphan sweep failed", "error", sweepErr,
			"scanned", stats.Scanned, "deleted", stats.Deleted,
			"tempDeleted", stats.TempDeleted, "failed", stats.Failed)
	} else {
		slog.Info("startup media orphan sweep",
			"scanned", stats.Scanned, "deleted", stats.Deleted,
			"tempDeleted", stats.TempDeleted, "failed", stats.Failed)
	}

	// Startup pass: audit-retention cleanup — the 365-day
	// window. Non-fatal like the others: retention is hygiene; a missed
	// pass deletes nothing, the next tick catches up.
	auditCtx, cancelAudit := context.WithTimeout(ctx, startupPassBudget)
	auditN, auditErr := cleanupAuditEvents(auditCtx, db)
	cancelAudit()
	if auditErr != nil {
		slog.Warn("startup audit cleanup failed", "error", auditErr)
	} else if auditN > 0 {
		slog.Info("startup audit cleanup", "deleted", auditN)
	}

	// SMTP sender wiring: a real relay when
	// SMTP_HOST is configured (production always — config validation); the
	// dev LogLink fallback otherwise. The fallback is loopback-only: config
	// validation refuses any other origin without a host, so a reset link
	// can never land in logs anywhere else. A configured relay with an
	// empty API key fails closed at startup — broken mail must not deploy.
	var sender mail.Sender
	if cfg.SMTPHost != "" {
		key, err := cfg.LoadSecret("smtp_api_key")
		if err != nil {
			slog.Error("SMTP API key load failed", "error", err)
			os.Exit(1)
		}
		if key == "" {
			slog.Error("SMTP configuration failed", "error", errors.New("SMTP_HOST is set but smtp_api_key is empty — refusing to start with a broken mailer"))
			os.Exit(1)
		}
		relay, err := mail.NewRelay(mail.Config{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: key,
			From:     cfg.SMTPFrom,
		})
		if err != nil {
			slog.Error("SMTP configuration validation failed", "error", err)
			os.Exit(1)
		}
		sender = relay
	} else {
		sender = mail.LogLink{}
	}

	// Web push wiring: the VAPID
	// private key is the compose secret vapid_private_key — fail-closed in
	// production (a deployed app must never lose its signing key silently);
	// development without the secret disables DELIVERY only (nil seams)
	// — the subscription, preference, and self-test endpoints still work
	// (the self-test then reports zero delivered).
	var notify push.Notifier
	var tester push.TestSender
	vapidPrivate, err := cfg.LoadSecret("vapid_private_key")
	if err != nil {
		slog.Error("VAPID private key load failed", "error", err)
		os.Exit(1)
	}
	if vapidPrivate == "" {
		if cfg.AppEnv == config.EnvProduction {
			slog.Error("web push configuration failed", "error", errors.New("vapid_private_key is empty — refusing to start without the web push signing key"))
			os.Exit(1)
		}
		slog.Warn("vapid_private_key not set — web push delivery disabled (development)")
	} else {
		// The pins keep the four PUBLIC copies equal and the wire format is
		// validated in config.Load, but only this check can see a mismatched
		// PAIR — both halves are pasted by hand into the VPS .env, and a
		// mismatch means every push send is rejected by the push service
		// (silently dead push, not a startup error).
		if err := config.ValidateVAPIDPair(cfg.VAPIDPublicKey, vapidPrivate); err != nil {
			slog.Error("VAPID keypair validation failed", "error", err)
			os.Exit(1)
		}
		pushSender := push.NewSender(db, cfg.VAPIDPublicKey, vapidPrivate, pushSubscriber)
		notify = pushSender
		tester = pushSender
	}

	// Periodic background tasks — session cleanup, the media orphan sweep,
	// the audit retention delete, and the WAL-size readout — all hourly, all
	// sharing the signal context; main waits for them during graceful
	// shutdown via cleanupWG. They start after every fail-closed startup
	// check, so an error exit never leaves tasks running.
	var cleanupWG sync.WaitGroup
	cleanupWG.Go(func() { startSessionCleanup(ctx, db) })
	cleanupWG.Go(func() { startMediaSweep(ctx, db, cfg.DataDir) })
	cleanupWG.Go(func() { startAuditCleanup(ctx, db) })
	cleanupWG.Go(func() { startWALMonitor(ctx, cfg.DataDir) })

	mux := http.NewServeMux()

	// The SHARED verification-send bucket: one
	// verification email per user per 30 minutes across EVERY surface —
	// the resend (routes.Auth) and the email-change send (routes.Users)
	// share this single instance, so the limit cannot be bypassed by
	// switching endpoints.
	verifySendLimiter := middleware.NewRateLimiter(1, 30*time.Minute, time.Minute)

	// Health: unversioned paths on the public chain — no domain middleware.
	routes.Health(mux, db, cfg.DataDir)

	// Auth: /api/v1/auth/* on the auth chain (path-scoped IP limits) +
	// handler rate limits.
	// Secure cookies and the trusted origin are config-driven: APP_ENV=
	// production enables the Secure attribute and requires an explicit
	// https TRUSTED_ORIGIN (fail-closed in internal/config). The mailer
	// serves the forgot-password flow.
	routes.Auth(mux, db, cfg.SecureCookies(), cfg.TrustedOrigin, cfg.PublicBaseURL, sender, verifySendLimiter)

	// Users: /api/v1/users/me — the current user's profile + favorites
	// + the self-service avatar upload + the email change.
	routes.Users(mux, db, cfg.SecureCookies(), cfg.TrustedOrigin, cfg.PublicBaseURL, cfg.DataDir, sender, verifySendLimiter)

	// Blog: public read endpoints (the public chain) + staff CRUD/admin reads.
	routes.Blog(mux, db, cfg.DataDir, cfg.SecureCookies(), cfg.TrustedOrigin, cfg.PublicBaseURL, notify)

	// Projects: public read endpoints (the public chain) + staff CRUD/admin reads.
	routes.Projects(mux, db, cfg.DataDir, cfg.SecureCookies(), cfg.TrustedOrigin, cfg.PublicBaseURL, notify)

	// Search: public merged search over published content (the public chain).
	routes.Search(mux, db, cfg.PublicBaseURL)

	// Notifications: the unified feed — the five account audit events for
	// staff plus per-recipient comment notifications.
	routes.Notifications(mux, db, cfg.SecureCookies(), cfg.TrustedOrigin)

	// Web push: subscription + preference endpoints.
	routes.Push(mux, db, cfg.SecureCookies(), cfg.TrustedOrigin, tester)

	// Staff: the dashboard metrics endpoint and the log
	// viewer — the /api/v1/staff namespace; the audit browser
	// joins it there. The logs handler reads the sink this process
	// writes, so it receives the resolved log directory.
	routes.Staff(mux, db, cfg.SecureCookies(), cfg.TrustedOrigin,
		filepath.Join(cfg.DataDir, logging.DirName))

	// Comments: the inline thread on blog posts and projects — public reads,
	// optional-session list, authenticated writes;
	// the reply/heart/create handlers fan out to the push channel when a
	// notification row landed.
	routes.Comments(mux, db, cfg.SecureCookies(), cfg.TrustedOrigin, cfg.PublicBaseURL, notify)

	// Media: public immutable asset serving from the local media directory
	// plus the staff upload endpoint.
	routes.Media(mux, cfg.DataDir, db, cfg.SecureCookies(), cfg.TrustedOrigin, cfg.PublicBaseURL)

	// API namespace fallback: unknown /api/v1/* paths get a JSON 404 problem,
	// never the SPA HTML response.
	routes.APIFallback(mux)

	// Frontend: catch-all for SPA client-side routing.
	// Version is injected into index.html at serve time (ldflags default: "dev").
	mux.Handle("/", frontendHandler(Version))

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Start server in background goroutine.
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Wait for signal or fatal error.
	fatalServer := false
	select {
	case err := <-serverErr:
		slog.Error("http server failed", "error", err)
		fatalServer = true
		// Release the background tasks so shutdown can join them.
		stop()
	case <-ctx.Done():
		slog.Info("shutting down gracefully")
	}

	// Graceful shutdown with a bounded context.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		// os.Exit skips the cleanupWG join and every deferred close; the OS
		// reclaims the descriptors, and SQLite's WAL recovery covers a process
		// that exits without a clean close.
		slog.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}

	// The signal context is already cancelled, so the cleanup task exits
	// after its current bounded batch. Wait so the database is not closed
	// under an in-flight DELETE (busy timeout bounds this to ~5s worst case).
	cleanupWG.Wait()

	// Explicit close so a close error is reported. CloseDatabase is
	// idempotent; the deferred call is a return-path no-op — the os.Exit
	// branches never run it.
	if err := database.CloseDatabase(db); err != nil {
		slog.Error("database close failed", "error", err)
	}

	slog.Info("server stopped")
	if fatalServer {
		os.Exit(1)
	}
}
