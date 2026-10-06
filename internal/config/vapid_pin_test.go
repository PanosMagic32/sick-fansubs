package config

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The VAPID public key lives in FOUR places by design: the frontend constant
// (baked into the bundle as applicationServerKey),
// the Go config default (the server signs with the matching private half),
// .env.example (the bring-up documentation for the operator), and the
// compose service's `${VAPID_PUBLIC_KEY:-…}` fallback (what the container
// gets when the VPS .env omits the variable). These pins keep them equal — a
// drift would make every push subscription fail at the push service (the
// browser checks the VAPID key against the one it subscribed with), and a
// test failure is the cheap place to learn that (the static-duplicate
// precedent).

// frontendKeyPattern extracts the exported string literal — the anchored
// form rejects a stray mention in prose, and a duplicated value fails the
// equality check below.
var frontendKeyPattern = regexp.MustCompile(`(?m)^export const VAPID_PUBLIC_KEY\s*=\s*"([A-Za-z0-9_-]+)"`)

// composeKeyPattern extracts the fallback inside the compose service
// environment — the `VAPID_PUBLIC_KEY: ${VAPID_PUBLIC_KEY:-<key>}` mapping
// line, anchored so a commented example cannot satisfy it.
var composeKeyPattern = regexp.MustCompile(`(?m)^[ \t]+VAPID_PUBLIC_KEY: \$\{VAPID_PUBLIC_KEY:-([A-Za-z0-9_-]+)\}`)

func TestVAPIDPublicKeyMatchesFrontend(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "web", "src", "shared", "config", "vapid.ts"))
	if err != nil {
		t.Fatalf("read the frontend VAPID constant: %v", err)
	}
	match := frontendKeyPattern.FindSubmatch(data)
	if match == nil {
		t.Fatal("web/src/shared/config/vapid.ts must export `VAPID_PUBLIC_KEY` as a single string literal")
	}
	if string(match[1]) != defaultVAPIDPublicKey {
		t.Error("web/src/shared/config/vapid.ts must carry the SAME public key as the config default — regenerate both together on rotation")
	}
}

func TestVAPIDPublicKeyMatchesEnvExample(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	want := "VAPID_PUBLIC_KEY=" + defaultVAPIDPublicKey
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "VAPID_PUBLIC_KEY=") {
			if strings.TrimSpace(line) != want {
				t.Errorf(".env.example VAPID_PUBLIC_KEY line: got %q, want %q", strings.TrimSpace(line), want)
			}
			return
		}
	}
	t.Error(".env.example must document VAPID_PUBLIC_KEY with the same value as the config default")
}

// TestVAPIDPublicKeyMatchesComposeFallback pins the deploy-path copy: the
// container receives this fallback whenever the VPS .env omits
// VAPID_PUBLIC_KEY, and a stale key there is invisible until push sends start
// failing.
func TestVAPIDPublicKeyMatchesComposeFallback(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	match := composeKeyPattern.FindSubmatch(data)
	if match == nil {
		t.Fatal("compose.yaml must default VAPID_PUBLIC_KEY to the committed public key (${VAPID_PUBLIC_KEY:-…})")
	}
	if string(match[1]) != defaultVAPIDPublicKey {
		t.Error("compose.yaml's VAPID_PUBLIC_KEY fallback must equal the config default on rotation")
	}
}

func TestValidateVAPIDPublicKey(t *testing.T) {
	t.Parallel()

	// The committed default is a valid 65-byte point.
	if err := validateVAPIDPublicKey(defaultVAPIDPublicKey); err != nil {
		t.Errorf("default VAPID public key must validate: %v", err)
	}
	// …and a real point on the curve, not merely 65 bytes: a tampered
	// constant would otherwise only surface at push-send time.
	decoded, err := base64.RawURLEncoding.DecodeString(defaultVAPIDPublicKey)
	if err != nil {
		t.Fatalf("decode the default public key: %v", err)
	}
	if _, err := ecdh.P256().NewPublicKey(decoded); err != nil {
		t.Errorf("default VAPID public key must be a P-256 point: %v", err)
	}
	// Wrong sizes and encodings fail closed at startup; the exact 65-byte
	// boundary is pinned in both directions so a `< 65` mutation cannot pass.
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"empty", "", true},
		{"bad chars", "!!!!", true},
		{"64 decoded bytes rejected", base64.RawURLEncoding.EncodeToString(make([]byte, 64)), true},
		{"65 decoded bytes accepted", base64.RawURLEncoding.EncodeToString(make([]byte, 65)), false},
		{"66 decoded bytes rejected", base64.RawURLEncoding.EncodeToString(make([]byte, 66)), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVAPIDPublicKey(tt.raw)
			if tt.wantErr && err == nil {
				t.Errorf("validateVAPIDPublicKey(%q) = nil, want error", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateVAPIDPublicKey(%q) = %v, want nil", tt.raw, err)
			}
		})
	}
}

// TestValidateVAPIDPair pins the startup pair check (cmd/api): only a real
// derivation can catch a hand-pasted mismatch — all four public copies can
// agree while the private half belongs to a different keypair, and webpush-go
// would then be rejected by every push service.
func TestValidateVAPIDPair(t *testing.T) {
	t.Parallel()

	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test keypair: %v", err)
	}
	pub := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	d := base64.RawURLEncoding.EncodeToString(priv.Bytes())

	if err := ValidateVAPIDPair(pub, d); err != nil {
		t.Errorf("a matching pair must validate: %v", err)
	}

	other, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate second test keypair: %v", err)
	}
	otherPub := base64.RawURLEncoding.EncodeToString(other.PublicKey().Bytes())
	if err := ValidateVAPIDPair(otherPub, d); err == nil {
		t.Error("a public key from another keypair must be rejected")
	}

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"empty", "", true},
		{"not base64", "!!!!", true},
		{"31 decoded bytes rejected", base64.RawURLEncoding.EncodeToString(make([]byte, 31)), true},
		{"33 decoded bytes rejected", base64.RawURLEncoding.EncodeToString(make([]byte, 33)), true},
		{"zero scalar rejected", base64.RawURLEncoding.EncodeToString(make([]byte, 32)), true},
		{"matching 32-byte scalar accepted", d, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVAPIDPair(pub, tt.raw)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateVAPIDPair(pub, %q) = nil, want error", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateVAPIDPair(pub, %q) = %v, want nil", tt.raw, err)
			}
		})
	}
}

// repoRoot walks up from the test's working directory (the package dir)
// until it finds go.mod — the repository root. Tests never hardcode the
// absolute path, so the pins survive checkouts anywhere.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root (go.mod) not found")
		}
		dir = parent
	}
}
