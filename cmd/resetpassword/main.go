// Command resetpassword resets one account's password with a server-generated
// temp password and forces a change on next sign-in — the
// operator-run break-glass path.
// It is the recovery path for a lone super-admin and any operator-driven
// reset; it requires the same SSH/filesystem trust as backup restore but
// loses no post-backup data.
//
// The command looks the account up by username, generates a 16-character
// temp password via the auth service, stores a fresh verifier, sets the
// forced-change flag, bumps auth_version, and deletes the account's sessions
// in one transaction. The plaintext prints ONCE to stdout for the operator
// to hand over out of band — never to logs. A password_reset audit event is
// emitted with actor "maintenance" through the dual-write path (the writer
// is registered here, so the event persists to audit_events).
//
// It opens through the strict application opener (current migration history
// required; it never applies migrations) and is an offline maintenance step:
// run it while the application is stopped, like cmd/rebuildsearch.
//
// Usage:
//
//	DATA_DIR=/app/data cmd/resetpassword -username <name>
//
// Exit codes: 0 success, 1 on any failure. Logs go to stderr.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"sick-fansubs/internal/audit"
	"sick-fansubs/internal/auth"
	"sick-fansubs/internal/config"
	"sick-fansubs/internal/database"
	"sick-fansubs/internal/logging"
	"sick-fansubs/internal/store"
)

// resetDeadline bounds the whole operation — the expensive part is the
// bcrypt hash; this guards an operator mistake (e.g. pointing at a huge
// file that is not a real database).
const resetDeadline = time.Minute

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	username := flag.String("username", "", "username of the account to reset")
	flag.Parse()

	if *username == "" {
		logger.Error("password reset failed", "error", errors.New("username is required (-username <name>)"))
		os.Exit(1)
	}

	if err := run(logger, *username, os.Stdout); err != nil {
		logger.Error("password reset failed", "error", err)
		os.Exit(1)
	}
}

// run is the testable seam — main() only wires the environment. The temp
// password prints to out exactly once.
func run(logger *slog.Logger, username string, out io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// The strict application opener: current migration history required,
	// never migrates (`make migrate-local` is the
	// operator's explicit step).
	db, err := database.OpenApplication(database.Config{DataDir: cfg.DataDir})
	if err != nil {
		return fmt.Errorf("open application: %w", err)
	}
	defer database.CloseDatabase(db)

	// Durable audit: register the writer so the
	// password_reset event persists to audit_events, not just slog.
	audit.SetWriter(&store.AuditWriter{DB: db})
	defer audit.SetWriter(nil)

	ctx, cancel := context.WithTimeout(context.Background(), resetDeadline)
	defer cancel()

	target, err := store.UserByCanonical(ctx, db, strings.ToLower(username))
	if errors.Is(err, store.ErrNotFound) {
		// Imported usernames keep legacy spacing the operator cannot retype
		// exactly — the same lenient bridge the sign-in path uses.
		target, err = store.UserByCanonicalLenient(ctx, db, strings.ToLower(username))
	}
	if err != nil {
		if store.IsAmbiguousUser(err) {
			return fmt.Errorf("username %q matches several imported accounts; use the exact spelling", username)
		}
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no user with username %q", username)
		}
		return fmt.Errorf("lookup user: %w", err)
	}

	// The admin-reset path never sends mail or builds links — the nil
	// sender turns an accidental ForgotPassword call from this maintenance
	// tool into an honest 500 (the service guards it).
	authSvc := auth.New(db, nil, "")
	temp, err := authSvc.ResetPassword(ctx, target.ID)
	if err != nil {
		return err
	}

	// The audit event carries the maintenance actor — the same record the
	// in-app endpoint writes for a staff reset, with an empty request ID and
	// address (there is no HTTP request). The command's own logger rides the
	// context, so the audit record lands in the same destination as the rest
	// of the command's output.
	audit.EventWithTarget(logging.With(ctx, logger),
		audit.EventPasswordReset, audit.ResultSuccess,
		"maintenance", target.ID, target.Role, "", "")

	// Close explicitly so a close failure is reported (CloseDatabase is
	// idempotent and the deferred close covers the early-error paths). The
	// temp-password handover runs even when the close failed: it is the only
	// copy of the new secret, and the close failure is returned alongside it.
	closeErr := database.CloseDatabase(db)

	logger.Info("password reset complete", "targetId", target.ID, "forcedChange", true)
	if _, err := fmt.Fprintf(out, "Temporary password for %s: %s\n", target.Username, temp); err != nil {
		if closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return fmt.Errorf("the reset succeeded but the temporary password write failed — re-run to issue a new one: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("the reset succeeded but closing the database failed: %w", closeErr)
	}
	return nil
}
