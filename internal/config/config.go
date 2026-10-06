// Package config provides typed, validated runtime configuration for the
// Sick-Fansubs application. It reads from environment variables, applies
// sensible defaults for development, and fails fast on invalid input.
//
// # Design principles
//
//   - Single source of truth for application runtime config: the API and the
//     maintenance tools read their environment here. The one-off commands with
//     their own documented env contracts (internal/config/AGENTS.md) read
//     those variables themselves.
//   - Validate at startup: the app refuses to start with invalid config.
//   - Sensible dev defaults: zero-config `go run ./cmd/api/` just works.
//   - Production expects explicit values: APP_ENV=production fails closed —
//     it requires explicit https PUBLIC_BASE_URL and TRUSTED_ORIGIN values
//     and enables Secure session cookies.
//
// # Why not put secrets in env vars?
//
// Environment variables leak to child processes, crash dumps, and
// /proc/<pid>/environ on Linux. When the app needs secret material
// (session keys, API tokens, SMTP passwords), prefer files:
//
//   - Docker secrets mounted at /run/secrets/<name>
//   - Or a dedicated secret manager (Vault, cloud KMS)
//
// See internal/config/AGENTS.md for the loader contract. The shape follows
// [12-factor config]; values arrive through [os.Getenv].
//
// [12-factor config]: https://12factor.net/config
package config

import (
	"crypto/ecdh"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all runtime configuration for the application.
// Zero values are not valid — use Load() to create a validated instance.
type Config struct {
	// Port is the TCP port the HTTP server listens on.
	// Env: PORT. Default: 8080. Must be 1–65535.
	Port int

	// DataDir is the absolute path to the SQLite data directory.
	// Env: DATA_DIR. Default: ./data (resolved to absolute at load time);
	// surrounding whitespace is refused, not trimmed.
	DataDir string

	// PublicBaseURL is the canonical public origin used to construct
	// absolute media URLs (the DB stores relative paths;
	// API responses emit base-URL-joined absolute URLs).
	// Env: PUBLIC_BASE_URL. Default: http://localhost:3000 — the browser
	// origin in local development, where Vite proxies /media to Go.
	// Must be an absolute http(s) URL without query or fragment; a trailing
	// "/" is normalized away. APP_ENV=production additionally requires
	// https (fail-closed).
	PublicBaseURL string

	// AppEnv is the runtime environment mode.
	// Env: APP_ENV. Default: "development". Valid values: "development",
	// "production". Production enables Secure session cookies and the
	// fail-closed https rules below.
	AppEnv string

	// TrustedOrigin is the canonical frontend origin the auth middleware
	// accepts for unsafe requests: an absolute http(s) origin without path,
	// query, or fragment.
	// Env: TRUSTED_ORIGIN. Development default: http://localhost:3000.
	// Production has NO default — it must be set explicitly and use https
	// (fail-closed).
	TrustedOrigin string

	// SMTP relay settings for outbound transactional email.
	// The API key is NOT an env var — it arrives through the Compose
	// file-secrets loader (LoadSecret).
	//
	// SMTPHost (Env: SMTP_HOST): relay host, e.g. smtp.resend.com.
	// Empty = no relay: the dev-only LogLink sender takes over, and that is
	// only legal when PUBLIC_BASE_URL is the loopback dev origin
	// (a reset link is a credential — it must never land in logs anywhere
	// else). Production always requires a host.
	SMTPHost string

	// SMTPPort (Env: SMTP_PORT): 465/2465 implicit TLS, 25/587/2587
	// STARTTLS. Default 587. Anything outside the whitelist fails
	// validation — no silent else-branch.
	SMTPPort int

	// SMTPUsername (Env: SMTP_USERNAME): SMTP auth username. Resend uses
	// the literal "resend"; the default keeps the provider swappable
	// behind config.
	SMTPUsername string

	// SMTPFrom (Env: SMTP_FROM): envelope/header From. Required in
	// production (an empty From breaks SMTP); development defaults to
	// noreply@sickfansubs.com (the verified domain is the
	// only sender; the no-reply address takes no replies).
	SMTPFrom string

	// VAPIDPublicKey (Env: VAPID_PUBLIC_KEY): the web push VAPID public key —
	// the URL-safe base64 65-byte uncompressed P-256 point the frontend
	// passes as applicationServerKey. PUBLIC material, not a secret: the value
	// is baked into the web bundle as well. The default is the PRODUCTION
	// keypair's public half; the rotation flow regenerates both halves and
	// swaps every copy. The PRIVATE half is the compose secret
	// vapid_private_key via LoadSecret — never an env var.
	VAPIDPublicKey string
}

// Environment modes for APP_ENV.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Load reads configuration from environment variables, applies defaults,
// validates every field, and returns a Config or a descriptive error.
//
// Call once in main(). If it returns an error, log it and exit — the app
// cannot recover from invalid configuration.
func Load() (*Config, error) {
	appEnv := appEnvFromEnv()
	dataDir, err := dataDirFromEnv()
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Port:           portFromEnv(),
		DataDir:        dataDir,
		PublicBaseURL:  publicBaseURLFromEnv(),
		AppEnv:         appEnv,
		TrustedOrigin:  trustedOriginFromEnv(appEnv),
		SMTPHost:       smtpHostFromEnv(),
		SMTPPort:       smtpPortFromEnv(),
		SMTPUsername:   smtpUsernameFromEnv(),
		SMTPFrom:       smtpFromFromEnv(appEnv),
		VAPIDPublicKey: vapidPublicKeyFromEnv(),
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate checks that every field is within accepted bounds.
// It returns the first error found — fix one, re-run, get the next.
func (c *Config) validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: PORT must be 1–65535, got %d", c.Port)
	}
	if c.DataDir == "" {
		return fmt.Errorf("config: DATA_DIR must not be empty")
	}
	if err := validatePublicBaseURL(c.PublicBaseURL); err != nil {
		return err
	}
	if c.AppEnv != EnvDevelopment && c.AppEnv != EnvProduction {
		return fmt.Errorf("config: APP_ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, c.AppEnv)
	}
	if c.AppEnv == EnvProduction && c.TrustedOrigin == "" {
		return fmt.Errorf("config: APP_ENV=%s requires TRUSTED_ORIGIN to be set", EnvProduction)
	}
	if err := validateTrustedOrigin(c.TrustedOrigin); err != nil {
		return err
	}
	if c.AppEnv == EnvProduction {
		// Fail-closed production contract: the canonical origin
		// and the trusted auth origin must both be HTTPS. Both values have
		// already passed the absolute-http(s)-URL validators above, so a
		// scheme prefix check is sufficient here.
		if !strings.HasPrefix(c.TrustedOrigin, "https://") {
			return fmt.Errorf("config: APP_ENV=%s requires TRUSTED_ORIGIN to use https, got %q", EnvProduction, c.TrustedOrigin)
		}
		if !strings.HasPrefix(c.PublicBaseURL, "https://") {
			return fmt.Errorf("config: APP_ENV=%s requires PUBLIC_BASE_URL to use https, got %q", EnvProduction, c.PublicBaseURL)
		}
	}
	if err := c.validateSMTP(); err != nil {
		return err
	}
	if err := validateVAPIDPublicKey(c.VAPIDPublicKey); err != nil {
		return err
	}
	return nil
}

// SecureCookies reports whether session cookies carry the Secure attribute.
// The mapping lives here so every route registration reads the same answer and
// the production switch cannot silently drift per call site.
func (c *Config) SecureCookies() bool {
	return c.AppEnv == EnvProduction
}

// smtpPorts is the accepted SMTP port set (Resend's
// implicit-TLS ports 465/2465 and STARTTLS ports 25/587/2587). The
// whitelist exists so no unknown port silently falls into the wrong TLS
// mode.
var smtpPorts = map[int]bool{25: true, 465: true, 587: true, 2465: true, 2587: true}

// validateSMTP enforces the relay hygiene rules that hold for EVERY process
// that loads config:
//   - a port outside the whitelist is rejected;
//   - CR/LF in From is rejected (header injection defense at the earliest
//     boundary; there is no Reply-To header).
//
// The environment requirements (production host/from; the loopback-only dev
// fallback) live in ValidateMailRelay — they bind the API surface only,
// because the break-glass maintenance commands (cmd/restore, cmd/migrate,
// cmd/resetpassword, …) load the same config under APP_ENV=production and
// must never fail on mail they do not send.
func (c *Config) validateSMTP() error {
	if !smtpPorts[c.SMTPPort] {
		return fmt.Errorf("config: SMTP_PORT must be one of 25, 465, 587, 2465, 2587, got %d", c.SMTPPort)
	}
	if strings.ContainsAny(c.SMTPFrom, "\r\n") {
		return fmt.Errorf("config: SMTP_FROM must not contain CR/LF")
	}
	return nil
}

// ValidateMailRelay enforces the environment-level mail requirements for the
// API surface (cmd/api calls it after Load):
//   - production requires SMTP_HOST and SMTP_FROM (an empty From breaks
//     SMTP, and a missing host has no fail-closed meaning);
//   - development without SMTP_HOST falls back to the LogLink sender, which
//     is ONLY legal with the loopback dev origin — a reset link is a
//     credential and must never land in logs anywhere else.
func (c *Config) ValidateMailRelay() error {
	if c.AppEnv == EnvProduction && c.SMTPHost == "" {
		return fmt.Errorf("config: APP_ENV=%s requires SMTP_HOST to be set", EnvProduction)
	}
	if c.AppEnv == EnvProduction && c.SMTPFrom == "" {
		return fmt.Errorf("config: APP_ENV=%s requires SMTP_FROM to be set", EnvProduction)
	}
	if c.AppEnv == EnvDevelopment && c.SMTPHost == "" && c.PublicBaseURL != defaultPublicBaseURL {
		return fmt.Errorf("config: SMTP_HOST is required unless PUBLIC_BASE_URL is %q (the dev log-link fallback is loopback-only)", defaultPublicBaseURL)
	}
	return nil
}

// defaultPublicBaseURL is the development default origin — the only origin
// the dev log-link mail fallback may run under.
const defaultPublicBaseURL = "http://localhost:3000"

// dataDirFromEnv reads DATA_DIR, applies the default, and resolves it to
// an absolute path. The directory itself is NOT created here — main.go
// handles mkdir as a separate startup step. A resolution failure is an error:
// the one-absolute-data-directory rule leaves no relative fallback. A value
// with surrounding whitespace is refused: trimming it silently could resolve
// to a different directory, and resolving it as-is would create and open a
// fresh empty database.
func dataDirFromEnv() (string, error) {
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		dir = "./data"
	}
	if strings.TrimSpace(dir) != dir {
		return "", fmt.Errorf("config: DATA_DIR must not have surrounding whitespace")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("config: resolve DATA_DIR: %w", err)
	}
	return abs, nil
}

// portFromEnv reads PORT with a default of 8080.
// Returns 0 on unparseable input so validate() can produce a clear error.
func portFromEnv() int {
	raw := os.Getenv("PORT")
	if raw == "" {
		return 8080
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return port
}

// publicBaseURLFromEnv reads PUBLIC_BASE_URL, applies the dev default, and
// normalizes a trailing "/" away. Invalid values are left as-is so
// validate() reports them instead of silently rewriting configuration.
func publicBaseURLFromEnv() string {
	raw := os.Getenv("PUBLIC_BASE_URL")
	if raw == "" {
		return defaultPublicBaseURL
	}
	return strings.TrimRight(raw, "/")
}

// smtpHostFromEnv reads SMTP_HOST (no default — empty means the dev
// log-link fallback, which validateSMTP scopes to loopback dev).
func smtpHostFromEnv() string {
	return strings.TrimSpace(os.Getenv("SMTP_HOST"))
}

// smtpPortFromEnv reads SMTP_PORT with a default of 587 (STARTTLS).
// Unparseable values map to -1 so validateSMTP rejects them.
func smtpPortFromEnv() int {
	raw := os.Getenv("SMTP_PORT")
	if raw == "" {
		return 587
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return port
}

// smtpUsernameFromEnv reads SMTP_USERNAME. Resend uses the literal
// "resend"; the default keeps the provider swappable behind
// config. Surrounding whitespace is trimmed like the other relay values, and
// an empty or whitespace-only value selects the default — a stray space must
// not fail SMTP auth at send time.
func smtpUsernameFromEnv() string {
	raw := strings.TrimSpace(os.Getenv("SMTP_USERNAME"))
	if raw == "" {
		return "resend"
	}
	return raw
}

// smtpFromFromEnv reads SMTP_FROM. Development defaults to
// noreply@sickfansubs.com (the verified root domain; Resend 403s anything
// but the account owner's own address); production has no default
// — ValidateMailRelay requires an explicit value.
func smtpFromFromEnv(appEnv string) string {
	raw := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if raw == "" && appEnv != EnvProduction {
		return "noreply@sickfansubs.com"
	}
	return raw
}

// defaultVAPIDPublicKey is the PRODUCTION keypair's public half, committed
// here, in the frontend constant (web/src/shared/config/vapid.ts), in
// .env.example, and as the compose fallback, all pinned equal by tests.
// The matching private half lives ONLY in the VPS .env (compose secret);
// a locally-generated pair does not match this constant.
const defaultVAPIDPublicKey = "BHTZD8cRI0vmAwkO9fcsY7KwL2vL7vx1OByBARaSItTq2kqQNiPqLJa7zHSIgj8yRlAxGgO5ZAvxwMyMeNqafdc"

// vapidPublicKeyFromEnv reads VAPID_PUBLIC_KEY with the committed production
// default. An invalid value is left as-is so validateVAPIDPublicKey reports it
// instead of silently rewriting configuration.
func vapidPublicKeyFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")); raw != "" {
		return raw
	}
	return defaultVAPIDPublicKey
}

// validateVAPIDPublicKey enforces the wire format the VAPID signing path
// depends on: URL-safe base64 decoding to exactly 65 bytes (the
// uncompressed P-256 point). A wrong-sized key would fail every push send
// at request time — fail fast at startup instead.
func validateVAPIDPublicKey(raw string) error {
	if raw == "" {
		return fmt.Errorf("config: VAPID_PUBLIC_KEY must not be empty")
	}
	if len(raw) > 512 {
		return fmt.Errorf("config: VAPID_PUBLIC_KEY is too long")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != 65 {
		return fmt.Errorf("config: VAPID_PUBLIC_KEY must be URL-safe base64 decoding to 65 bytes (the P-256 point length)")
	}
	return nil
}

// ValidateVAPIDPair checks that the configured public key is the public half
// of the loaded private key. The four public copies are pinned equal
// by tests and validateVAPIDPublicKey checks the wire format, but neither can
// see a mismatched PAIR: the operator pastes both halves by hand, and
// webpush-go signs with the private key while advertising the configured
// public key — the push services reject that, so a mismatch is silently dead
// push rather than a startup error. Call it once with the loaded secret
// (cmd/api); an empty private key is the caller's "delivery disabled" state,
// not a mismatch.
func ValidateVAPIDPair(publicKey, privateKey string) error {
	d, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(privateKey))
	if err != nil || len(d) != 32 {
		return fmt.Errorf("config: VAPID private key must be URL-safe base64 decoding to 32 bytes (the P-256 scalar)")
	}
	priv, err := ecdh.P256().NewPrivateKey(d)
	if err != nil {
		return fmt.Errorf("config: VAPID private key is not a valid P-256 scalar: %w", err)
	}
	derived := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	if derived != strings.TrimSpace(publicKey) {
		return fmt.Errorf("config: VAPID keypair mismatch — the loaded private key's public half is not the configured VAPID_PUBLIC_KEY")
	}
	return nil
}

// appEnvFromEnv reads APP_ENV with a development default. Unknown values
// are left as-is so validate() reports them instead of guessing.
func appEnvFromEnv() string {
	if raw := os.Getenv("APP_ENV"); raw != "" {
		return raw
	}
	return EnvDevelopment
}

// trustedOriginFromEnv reads TRUSTED_ORIGIN. Development falls back to the
// Vite dev origin; production has no default — the caller leaves it empty
// and validate() fails closed. A trailing "/" is normalized away.
func trustedOriginFromEnv(appEnv string) string {
	raw := os.Getenv("TRUSTED_ORIGIN")
	if raw == "" && appEnv != EnvProduction {
		raw = defaultPublicBaseURL
	}
	return strings.TrimRight(raw, "/")
}

// validatePublicBaseURL enforces the contract that media URL construction
// depends on: an absolute http(s) origin (an optional path is allowed for a
// future CDN prefix), with no query, fragment, or userinfo. "http://" alone,
// file:// and other schemes, and relative values are rejected.
func validatePublicBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("config: PUBLIC_BASE_URL is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("config: PUBLIC_BASE_URL must use http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("config: PUBLIC_BASE_URL must be an absolute URL with a host")
	}
	if u.User != nil {
		// The value is emitted into client-visible URLs; credentials must not ride it.
		return fmt.Errorf("config: PUBLIC_BASE_URL must not carry userinfo")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("config: PUBLIC_BASE_URL must not contain a query or fragment")
	}
	return nil
}

// validateTrustedOrigin enforces the contract the TrustedOrigin middleware
// depends on: an absolute http(s) origin — scheme + host(+port) only. A path,
// query, fragment, userinfo, the scheme's default port, or a trailing-dot host
// never appears in a browser's Origin header, so all are rejected.
func validateTrustedOrigin(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("config: TRUSTED_ORIGIN is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("config: TRUSTED_ORIGIN must use http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("config: TRUSTED_ORIGIN must be an absolute URL with a host")
	}
	if u.User != nil {
		return fmt.Errorf("config: TRUSTED_ORIGIN must not carry userinfo, got %q", raw)
	}
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		// Browsers omit the default port from Origin; keeping it here can
		// only fail closed at request time.
		return fmt.Errorf("config: TRUSTED_ORIGIN must not name the scheme's default port, got %q", raw)
	}
	if strings.HasSuffix(u.Hostname(), ".") {
		return fmt.Errorf("config: TRUSTED_ORIGIN must not carry a trailing-dot host, got %q", raw)
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("config: TRUSTED_ORIGIN must be an origin only (no path, query, or fragment), got %q", raw)
	}
	return nil
}
