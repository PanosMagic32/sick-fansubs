// Package mail owns outbound transactional email: the SMTP
// relay surface and the server-side user-facing copy. It is the first
// server-side home for user-facing copy — the Lit message catalog cannot
// serve server-rendered email, so the centralized-copy rule gains one
// server-side source here.
//
// The relay is provider-neutral: one stdlib net/smtp implementation serves
// any provider, selected by config. Implicit TLS (ports 465/2465) dials
// tls.Dial + smtp.NewClient; STARTTLS (25/587/2587) uses the same explicit
// client commands (smtp.SendMail cannot time-bind a context, so both paths
// share the explicit command sequence). DKIM is signed by the relay, never
// by this code.
//
// LogLink is the local-development sender: it logs the message instead of
// sending, so dev can exercise the forgot-password flow without an SMTP
// secret. cmd/api constructs it ONLY under APP_ENV=development with the
// loopback dev origin — never otherwise.
package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"sick-fansubs/internal/logging"
)

// Config is the SMTP relay configuration for the stdlib net/smtp client.
// It is populated from internal/config (env vars); the API key arrives
// through the Compose file-secrets loader, not env.
type Config struct {
	// Host is the relay host, e.g. smtp.resend.com.
	Host string
	// Port selects the TLS mode: 465/2465 implicit TLS, 25/587/2587 STARTTLS.
	Port int
	// Username is the SMTP auth username (Resend uses "resend").
	Username string
	// Password is the SMTP API key (Resend: the API key IS the password).
	Password string
	// From is the envelope/header From address.
	From string
}

// Sender is the consumer-owned send seam (interfaces are consumer-owned and
// small; tests inject a fake).
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Relay sends mail through one SMTP relay. Zero dependencies beyond stdlib.
type Relay struct {
	cfg Config
	// dialAddr overrides the dial target (host:port). Empty = derive from
	// cfg.Host + cfg.Port. Test-only seam: the TLS mode must follow the real
	// port constants while the fake server listens on a dynamic port.
	dialAddr string
	// rootCAs overrides the TLS trust roots (test-only: the fake server's
	// self-signed cert is its own CA). nil = system roots.
	rootCAs *x509.CertPool
	// sendTimeout bounds one delivery attempt. NewRelay sets the production
	// default (10 s); tests inject a short value so the
	// stalling-server deadline test does not wait the real budget.
	sendTimeout time.Duration
}

// NewRelay validates the config and returns a Relay. Host and From must be
// non-empty — an empty From breaks the SMTP envelope/headers.
func NewRelay(cfg Config) (*Relay, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("mail: SMTP host is required")
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("mail: SMTP from is required")
	}
	return &Relay{cfg: cfg, sendTimeout: defaultSendTimeout}, nil
}

// defaultSendTimeout bounds one delivery attempt: one attempt, bounded
// timeout, no retry loop. The Relay field exists so
// tests can shrink the wait without weakening the production contract.
const defaultSendTimeout = 10 * time.Second

// Send delivers one plain-text message. It never retries: a failure is
// returned to the caller, which decides the public outcome (the
// forgot-password 500 path).
func (r *Relay) Send(ctx context.Context, to, subject, body string) error {
	// Header-injection defense at the last boundary: the recipient comes
	// from the users table (migrated emails bypass the registration charset
	// rule), so CR/LF is rejected here rather than trusted.
	if strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("mail: invalid recipient address")
	}

	ctx, cancel := context.WithTimeout(ctx, r.sendTimeout)
	defer cancel()

	msg := buildMessage(r.cfg.From, to, subject, body)
	return r.send(ctx, to, msg)
}

// send speaks SMTP explicitly: dial (implicit TLS or plain), greet,
// STARTTLS when the server advertises it, auth, envelope, data, quit. One
// command sequence serves both TLS modes — only the dial differs, so the
// timeout budget applies to every step.
func (r *Relay) send(ctx context.Context, to string, msg []byte) error {
	conn, err := r.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// stdlib net/smtp has no context support — only the dial consumed the
	// deadline. The absolute deadline bounds EVERY post-dial command (greet,
	// auth, envelope, data, quit), so a relay that accepts TCP and stalls
	// cannot hold the request goroutine past the send budget.
	if err := conn.SetDeadline(time.Now().Add(r.sendTimeout)); err != nil {
		return fmt.Errorf("smtp set deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, r.cfg.Host)
	if err != nil {
		return smtpReplyError("client", err)
	}
	defer client.Close()

	// STARTTLS upgrade on the plain ports (25/587/2587). NEVER on the
	// implicit-TLS ports — a server that wrongly advertises STARTTLS there
	// would trigger TLS-over-TLS (handshake garbage → false 500). The
	// upgrade is REQUIRED before auth on the plain ports: smtp.PlainAuth
	// refuses credentials over an unencrypted connection unless the server
	// is localhost (stdlib defense in depth). The dial-time deadline still
	// governs the wrapped TLS connection: tls.Conn delegates SetDeadline
	// to the underlying conn, so no re-set is needed here.
	if !r.implicitTLS() {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(r.tlsClientConfig()); err != nil {
				return smtpReplyError("starttls", err)
			}
		}
	}

	if err := client.Auth(r.auth()); err != nil {
		return smtpReplyError("auth", err)
	}
	if err := client.Mail(r.cfg.From); err != nil {
		return smtpReplyError("mail from", err)
	}
	// RCPT failures echo the recipient in the server response text — the
	// handler logs send errors, and the address is PII. The error keeps the
	// category without the server text.
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt to rejected")
	}
	w, err := client.Data()
	if err != nil {
		return smtpReplyError("data", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return smtpReplyError("write message", err)
	}
	if err := w.Close(); err != nil {
		return smtpReplyError("close message", err)
	}
	// QUIT failure AFTER the relay accepted the message is NOT a send
	// failure — the email is delivered. Surface it as a warning only;
	// answering 500 here would make the user retry and receive a
	// duplicate.
	if err := client.Quit(); err != nil {
		logging.From(ctx).WarnContext(ctx, "smtp quit failed", "error", smtpReplyError("quit", err))
	}
	return nil
}

// smtpReplyError reduces a server reply error to its stage and, for a
// well-formed reply, the SMTP reply code. A malformed reply line is a
// ProtocolError whose text embeds the raw server bytes, so it is flattened to
// the stage alone — a relay's response text can echo credential or message
// material, and these errors reach the application log. A transport error (no
// reply) is wrapped as-is so the stage stays diagnosable.
func smtpReplyError(stage string, err error) error {
	if reply, ok := errors.AsType[*textproto.Error](err); ok {
		return fmt.Errorf("smtp %s: server replied %d", stage, reply.Code)
	}
	if _, ok := errors.AsType[textproto.ProtocolError](err); ok {
		return fmt.Errorf("smtp %s: malformed server reply", stage)
	}
	return fmt.Errorf("smtp %s: %w", stage, err)
}

// implicitTLS reports whether the configured port dials TLS first
// (465/2465). The STARTTLS upgrade must be skipped there.
func (r *Relay) implicitTLS() bool {
	return r.cfg.Port == 465 || r.cfg.Port == 2465
}

// dial opens the connection in the mode the port selects: implicit TLS for
// 465/2465 (TLS first, then SMTP), plain TCP for the STARTTLS ports (the
// client upgrades before auth).
func (r *Relay) dial(ctx context.Context) (net.Conn, error) {
	addr := r.dialAddr
	if addr == "" {
		addr = net.JoinHostPort(r.cfg.Host, fmt.Sprintf("%d", r.cfg.Port))
	}
	var d net.Dialer
	if r.implicitTLS() {
		tlsDialer := tls.Dialer{NetDialer: &d, Config: r.tlsClientConfig()}
		conn, err := tlsDialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("tls dial %s: %w", addr, err)
		}
		return conn, nil
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

// tlsClientConfig builds the client TLS config: ServerName = the relay host
// (certificate identity), trust roots = system or the test-only pool.
func (r *Relay) tlsClientConfig() *tls.Config {
	return &tls.Config{ServerName: r.cfg.Host, RootCAs: r.rootCAs}
}

// auth returns the relay's PlainAuth.
func (r *Relay) auth() smtp.Auth {
	return smtp.PlainAuth("", r.cfg.Username, r.cfg.Password, r.cfg.Host)
}

// buildMessage assembles one plain-text RFC 5322 message. Header injection
// is impossible by construction: to, subject, and body come from the
// fixed template + the link (which carries only a base64url token and the
// config-validated base URL), and the From address is validated at config
// load. No Reply-To header is emitted: the From address
// is a no-reply sender and replies are meant to bounce. The subject is RFC
// 2047 encoded-word (Greek copy needs non-ASCII headers).
func buildMessage(from, to, subject, body string) []byte {
	subject = mime.QEncoding.Encode("utf-8", subject)

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: 8bit\r\n")
	fmt.Fprintf(&b, "\r\n%s", body)
	return []byte(b.String())
}

// LogLink is the local-development Sender: it writes the message to slog at
// Info instead of sending. The reset link lives in the body — this is the
// ONLY path that ever logs it (APP_ENV=development +
// loopback origin only, wired by cmd/api; never production).
type LogLink struct{}

// Send logs the message and reports success (the flow continues as if sent).
//
// The body IS logged — it carries the reset/verification link, which is the
// whole point of the dev fallback — but the RECIPIENT is not: the link is
// useless without it, and an address in a log line is the exact class the
// log hygiene rule forbids. This is the rule's one audited
// exemption, and it cannot reach production (the caller gates it on
// APP_ENV != production plus a loopback base URL).
func (LogLink) Send(ctx context.Context, _, subject, body string) error {
	logging.From(ctx).InfoContext(ctx, "dev mail not sent (no SMTP configured)",
		"subject", subject,
		"body", body,
	)
	return nil
}
