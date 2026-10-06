package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultSecretsDir is where Docker Compose mounts file secrets inside the
// container. Everything else is non-secret environment config.
const defaultSecretsDir = "/run/secrets"

// maxSecretFileSize bounds a secret file: real values are a few hundred
// bytes, so a mount pointed at something else must fail with a named refusal
// instead of an allocation.
const maxSecretFileSize = 64 << 10

// LoadSecret reads the file secret named name from /run/secrets.
//
// Production (c.AppEnv == EnvProduction): a missing or empty file is an
// error. Development: a missing or empty file returns ("", nil) — the
// caller decides whether the feature needs the secret to run. The flag comes
// from the validated Config.AppEnv, never a bare environment re-read, so an
// APP_ENV typo fails in Load.
//
// Compose mounts the value as a file secret (uid/gid 1000, mode 0400 —
// compose.yaml); it never travels through environment variables, `docker
// compose config`, or the image. A secret that grants any group or other
// permission is refused in every environment (the Compose mount is the
// accidental-exposure boundary, not a vault — a 0640 slip must fail loudly,
// not silently read), and a file larger than 64 KiB is refused with a named
// error. Surrounding whitespace is trimmed (a .env quoting artifact;
// whitespace inside a value is kept), and a name must be a plain Compose
// secret name (letters, digits, '_', '-', '.') so it can never escape the
// directory.
func (c *Config) LoadSecret(name string) (string, error) {
	return loadSecret(defaultSecretsDir, name, c.AppEnv == EnvProduction)
}

// loadSecret reads the file secret named name from dir. required selects
// the fail-closed production semantics; tests pass their own directory and
// flag so they never touch /run/secrets or the process environment.
func loadSecret(dir, name string, required bool) (string, error) {
	if !validSecretName(name) {
		return "", fmt.Errorf("config: secret name %q is not a valid Compose secret name", name)
	}

	path := filepath.Join(dir, name)
	// Lstat, not Stat: a symlink in place of the secret is refused too —
	// Compose mounts regular files, so a symlink is never legitimate, and
	// failing closed beats following it to wherever it points.
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if !required {
				return "", nil
			}
			return "", fmt.Errorf("config: secret %q is required in %s but %s does not exist", name, EnvProduction, path)
		}
		return "", fmt.Errorf("config: stat secret %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("config: secret %q must be a regular file", name)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return "", fmt.Errorf("config: secret %q must not grant any group or other access (mode %04o); use 0600 or 0400", name, perm)
	}
	if info.Size() > maxSecretFileSize {
		return "", fmt.Errorf("config: secret %q is too large (%d bytes, max %d); is the mount pointed at the right file?", name, info.Size(), maxSecretFileSize)
	}

	// The stat/read pair is not race-proof. That is deliberate: /run/secrets
	// is a root-owned bind mount inside the container, so the permission
	// check guards against accidental misconfiguration (a wrong Compose
	// mode), not against a local attacker who can rewrite root-owned files.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("config: read secret %q: %w", name, err)
	}
	// Compose writes the raw .env value; surrounding whitespace is formatting,
	// not secret material — and a stray space must not reach a signer that
	// decodes the value (the VAPID private key) or an auth routine. Whitespace
	// inside a value is kept.
	value := strings.TrimSpace(string(data))
	if value == "" {
		if !required {
			return "", nil
		}
		return "", fmt.Errorf("config: secret %q is required in %s but %s is empty", name, EnvProduction, path)
	}
	return value, nil
}

// validSecretName accepts the character set Docker Compose allows for
// secret names. It also blocks "", ".", and ".." — with the allowed set,
// only those could traverse the directory.
func validSecretName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}
