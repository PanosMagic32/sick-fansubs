package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSecretFile creates a file at dir/name with the given content and
// mode. Tests use it so permission semantics are explicit at every call
// site.
func writeSecretFile(t *testing.T, dir, name, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestLoadSecret covers the loader core through the unexported loadSecret
// with an explicit required flag and a test directory — the exported
// (*Config).LoadSecret is a thin delegation proven by TestConfigLoadSecret.
func TestLoadSecret(t *testing.T) {
	tests := []struct {
		name     string
		required bool
		fileName string
		content  string
		mode     os.FileMode
		want     string
		wantErr  string // substring of the expected error
	}{
		{
			name:     "optional missing file is empty",
			fileName: "absent",
			want:     "",
		},
		{
			name:     "required missing file fails closed",
			required: true,
			fileName: "absent",
			wantErr:  "required in production",
		},
		{
			name:     "optional reads value",
			fileName: "smtp_api_key",
			content:  "re_abc123",
			mode:     0o400,
			want:     "re_abc123",
		},
		{
			name:     "required reads value",
			required: true,
			fileName: "smtp_api_key",
			content:  "re_abc123",
			mode:     0o400,
			want:     "re_abc123",
		},
		{
			name:     "0600 mode accepted",
			fileName: "smtp_api_key",
			content:  "re_abc123",
			mode:     0o600,
			want:     "re_abc123",
		},
		{
			name:     "trailing newline trimmed",
			fileName: "smtp_api_key",
			content:  "re_abc123\n",
			mode:     0o400,
			want:     "re_abc123",
		},
		{
			name:     "trailing CRLF trimmed",
			fileName: "smtp_api_key",
			content:  "re_abc123\r\n",
			mode:     0o400,
			want:     "re_abc123",
		},
		{
			name:     "inner spaces kept",
			fileName: "smtp_api_key",
			content:  "re abc 123\n",
			mode:     0o400,
			want:     "re abc 123",
		},
		{
			name:     "surrounding whitespace trimmed",
			fileName: "smtp_api_key",
			content:  "  re_abc123\r\n",
			mode:     0o400,
			want:     "re_abc123",
		},
		{
			name:     "oversized file refused",
			required: true,
			fileName: "smtp_api_key",
			content:  strings.Repeat("x", maxSecretFileSize+1),
			mode:     0o400,
			wantErr:  "too large",
		},
		{
			name:     "size cap accepts the boundary",
			fileName: "smtp_api_key",
			content:  strings.Repeat("x", maxSecretFileSize),
			mode:     0o400,
			want:     strings.Repeat("x", maxSecretFileSize),
		},
		{
			name:     "empty file is missing when required",
			required: true,
			fileName: "smtp_api_key",
			content:  "",
			mode:     0o400,
			wantErr:  "is empty",
		},
		{
			name:     "newline-only file is missing when required",
			required: true,
			fileName: "smtp_api_key",
			content:  "\n",
			mode:     0o400,
			wantErr:  "is empty",
		},
		{
			name:     "empty file is optional otherwise",
			fileName: "smtp_api_key",
			content:  "",
			mode:     0o400,
			want:     "",
		},
		{
			name:     "group-readable file refused",
			required: true,
			fileName: "smtp_api_key",
			content:  "re_abc123",
			mode:     0o640,
			wantErr:  "must not grant any group or other access",
		},
		{
			name:     "world-readable file refused even when optional",
			fileName: "smtp_api_key",
			content:  "re_abc123",
			mode:     0o644,
			wantErr:  "must not grant any group or other access",
		},
		{
			name:     "traversal name refused",
			required: true,
			fileName: "../smtp_api_key",
			wantErr:  "not a valid Compose secret name",
		},
		{
			name:     "absolute path name refused",
			required: true,
			fileName: "/etc/passwd",
			wantErr:  "not a valid Compose secret name",
		},
		{
			name:     "empty name refused",
			required: true,
			fileName: "",
			wantErr:  "not a valid Compose secret name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.content != "" || tt.mode != 0 {
				writeSecretFile(t, dir, tt.fileName, tt.content, tt.mode)
			}

			got, err := loadSecret(dir, tt.fileName, tt.required)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("loadSecret(%q) = %q, want error containing %q", tt.fileName, got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("loadSecret(%q) error = %q, want it to contain %q", tt.fileName, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadSecret(%q): %v", tt.fileName, err)
			}
			if got != tt.want {
				t.Errorf("loadSecret(%q) = %q, want %q", tt.fileName, got, tt.want)
			}
		})
	}
}

// TestLoadSecretNotRegularFile: a directory in the secret's place must be
// refused — the loader reads files, not whatever Stat accepts.
func TestLoadSecretNotRegularFile(t *testing.T) {
	dir := t.TempDir()
	name := "smtp_api_key"
	if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := loadSecret(dir, name, true)
	if err == nil {
		t.Fatal("loadSecret accepted a directory as a secret")
	}
	if !strings.Contains(err.Error(), "must be a regular file") {
		t.Fatalf("error = %q, want it to mention a regular file", err)
	}
}

// TestLoadSecretSymlinkRefused: Lstat refuses a symlink in the secret's
// place even when the target has safe permissions — Compose mounts regular
// files, so a symlink is never legitimate.
func TestLoadSecretSymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real"), []byte("re_abc123"), 0o400); err != nil {
		t.Fatalf("write real: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "smtp_api_key")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := loadSecret(dir, "smtp_api_key", true)
	if err == nil {
		t.Fatal("loadSecret accepted a symlink as a secret")
	}
	if !strings.Contains(err.Error(), "must be a regular file") {
		t.Fatalf("error = %q, want it to mention a regular file", err)
	}
}

// TestConfigLoadSecret proves the exported method wires Config.AppEnv to
// the required flag. It uses a secret name that cannot plausibly exist
// under /run/secrets — the interesting assertions are which environment
// fails closed, not the fixed directory.
func TestConfigLoadSecret(t *testing.T) {
	tests := []struct {
		name    string
		appEnv  string
		wantErr string
	}{
		{name: "development missing secret optional", appEnv: EnvDevelopment, wantErr: ""},
		{name: "production missing secret fails closed", appEnv: EnvProduction, wantErr: "required in production"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{AppEnv: tt.appEnv}
			got, err := cfg.LoadSecret("sick_fansubs_absent_9f3d2a")
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("LoadSecret = %q, want error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadSecret: %v", err)
			}
			if got != "" {
				t.Errorf("LoadSecret = %q, want empty", got)
			}
		})
	}
}
