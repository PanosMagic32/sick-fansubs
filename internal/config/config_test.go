package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Unset env vars to test defaults.
	os.Unsetenv("PORT")
	os.Unsetenv("DATA_DIR")
	os.Unsetenv("PUBLIC_BASE_URL")
	os.Unsetenv("APP_ENV")
	os.Unsetenv("TRUSTED_ORIGIN")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with defaults: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Load() Port = %d, want 8080", cfg.Port)
	}
	if cfg.DataDir == "" {
		t.Error("expected non-empty DataDir")
	}
	if cfg.PublicBaseURL != "http://localhost:3000" {
		t.Errorf("got %q, want default PublicBaseURL http://localhost:3000", cfg.PublicBaseURL)
	}
	if cfg.AppEnv != EnvDevelopment {
		t.Errorf("got %q, want default AppEnv %q", cfg.AppEnv, EnvDevelopment)
	}
	if cfg.TrustedOrigin != "http://localhost:3000" {
		t.Errorf("got %q, want default TrustedOrigin http://localhost:3000", cfg.TrustedOrigin)
	}
}

func TestLoadCustomPort(t *testing.T) {
	os.Setenv("PORT", "3000")
	defer os.Unsetenv("PORT")
	os.Unsetenv("DATA_DIR")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with PORT=3000: %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("got %d, want port 3000", cfg.Port)
	}
}

func TestLoadInvalidPort(t *testing.T) {
	os.Setenv("PORT", "abc")
	defer os.Unsetenv("PORT")
	os.Unsetenv("DATA_DIR")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for PORT=abc")
	}
}

func TestLoadPortOutOfRange(t *testing.T) {
	os.Setenv("PORT", "99999")
	defer os.Unsetenv("PORT")
	os.Unsetenv("DATA_DIR")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for PORT=99999")
	}
}

func TestLoadCustomDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	os.Unsetenv("PORT")
	os.Setenv("DATA_DIR", "relative/data")
	defer os.Unsetenv("DATA_DIR")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with DATA_DIR=relative/data: %v", err)
	}
	want := filepath.Join(dir, "relative", "data")
	if cfg.DataDir != want {
		t.Errorf("DataDir = %q, want %q (resolved against the working directory)", cfg.DataDir, want)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		t.Errorf("DataDir = %q, want an absolute path", cfg.DataDir)
	}
}

// TestLoadDataDirWhitespace: a DATA_DIR with surrounding whitespace is
// refused rather than silently resolved to a different directory — which
// would create and open a fresh empty database.
func TestLoadDataDirWhitespace(t *testing.T) {
	os.Setenv("DATA_DIR", " ./data ")
	defer os.Unsetenv("DATA_DIR")

	_, err := Load()
	if err == nil {
		t.Fatal("DATA_DIR with surrounding whitespace accepted, want an error")
	}
	if !strings.Contains(err.Error(), "surrounding whitespace") {
		t.Errorf("error = %q, want it to name the surrounding-whitespace rule", err)
	}
}

func TestLoadPublicBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    string
		wantErr bool
	}{
		{"default when unset", "", "http://localhost:3000", false},
		{"custom https origin", "https://sickfansubs.com", "https://sickfansubs.com", false},
		{"trailing slash normalized", "https://sickfansubs.com/", "https://sickfansubs.com", false},
		{"double trailing slash normalized", "https://sickfansubs.com//", "https://sickfansubs.com", false},
		{"path prefix allowed", "https://cdn.example.com/static", "https://cdn.example.com/static", false},
		{"missing host", "https://", "", true},
		{"wrong scheme", "file:///tmp", "", true},
		{"relative", "just-a-path", "", true},
		{"query rejected", "https://sickfansubs.com?x=1", "", true},
		{"fragment rejected", "https://sickfansubs.com#frag", "", true},
		{"userinfo rejected", "https://user:pass@sickfansubs.com", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("PORT")
			os.Unsetenv("DATA_DIR")
			if tt.env == "" {
				os.Unsetenv("PUBLIC_BASE_URL")
			} else {
				os.Setenv("PUBLIC_BASE_URL", tt.env)
				defer os.Unsetenv("PUBLIC_BASE_URL")
			}
			// SMTP: these cases exercise URL validation, not the mailer — a
			// relay host keeps the custom-base-URL cases legal (the dev
			// log-link fallback is loopback-only).
			os.Setenv("SMTP_HOST", "smtp.example.com")
			defer os.Unsetenv("SMTP_HOST")

			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for PUBLIC_BASE_URL=%q", tt.env)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load with PUBLIC_BASE_URL=%q: %v", tt.env, err)
			}
			if cfg.PublicBaseURL != tt.want {
				t.Errorf("PublicBaseURL = %q, want %q", cfg.PublicBaseURL, tt.want)
			}
		})
	}
}

func TestLoadAppEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    string
		wantErr bool
	}{
		{"default when unset", "", EnvDevelopment, false},
		{"explicit development", EnvDevelopment, EnvDevelopment, false},
		{"explicit production with https config", EnvProduction, EnvProduction, false},
		{"invalid value rejected", "staging", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("PORT")
			os.Unsetenv("DATA_DIR")
			if tt.env == "" {
				os.Unsetenv("APP_ENV")
			} else {
				os.Setenv("APP_ENV", tt.env)
				defer os.Unsetenv("APP_ENV")
			}
			if tt.env == EnvProduction {
				os.Setenv("PUBLIC_BASE_URL", "https://v2.sickfansubs.com")
				defer os.Unsetenv("PUBLIC_BASE_URL")
				os.Setenv("TRUSTED_ORIGIN", "https://v2.sickfansubs.com")
				defer os.Unsetenv("TRUSTED_ORIGIN")
				// Production requires the relay — these
				// cases exercise APP_ENV validation, not the mailer.
				os.Setenv("SMTP_HOST", "smtp.example.com")
				defer os.Unsetenv("SMTP_HOST")
				os.Setenv("SMTP_FROM", "beta@example.com")
				defer os.Unsetenv("SMTP_FROM")
			} else {
				os.Unsetenv("PUBLIC_BASE_URL")
				os.Unsetenv("TRUSTED_ORIGIN")
				os.Unsetenv("SMTP_HOST")
				os.Unsetenv("SMTP_FROM")
			}

			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for APP_ENV=%q", tt.env)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load with APP_ENV=%q: %v", tt.env, err)
			}
			if cfg.AppEnv != tt.want {
				t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, tt.want)
			}
		})
	}
}

func TestLoadTrustedOrigin(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    string
		wantErr bool
	}{
		{"dev default when unset", "", "http://localhost:3000", false},
		{"explicit origin", "https://v2.sickfansubs.com", "https://v2.sickfansubs.com", false},
		{"trailing slash normalized", "https://v2.sickfansubs.com/", "https://v2.sickfansubs.com", false},
		{"path rejected", "https://sickfansubs.com/app", "", true},
		{"wrong scheme", "file:///tmp", "", true},
		{"missing host", "https://", "", true},
		{"query rejected", "https://sickfansubs.com?x=1", "", true},
		{"fragment rejected", "https://sickfansubs.com#frag", "", true},
		{"userinfo rejected", "https://user:pass@example.com", "", true},
		{"default port rejected", "https://sickfansubs.com:443", "", true},
		{"trailing-dot host rejected", "https://sickfansubs.com.", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("PORT")
			os.Unsetenv("DATA_DIR")
			os.Unsetenv("APP_ENV")
			os.Unsetenv("PUBLIC_BASE_URL")
			if tt.env == "" {
				os.Unsetenv("TRUSTED_ORIGIN")
			} else {
				os.Setenv("TRUSTED_ORIGIN", tt.env)
				defer os.Unsetenv("TRUSTED_ORIGIN")
			}

			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for TRUSTED_ORIGIN=%q", tt.env)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load with TRUSTED_ORIGIN=%q: %v", tt.env, err)
			}
			if cfg.TrustedOrigin != tt.want {
				t.Errorf("TrustedOrigin = %q, want %q", cfg.TrustedOrigin, tt.want)
			}
		})
	}
}

func TestSecureCookies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		appEnv string
		want   bool
	}{
		{"development cookies are not Secure", EnvDevelopment, false},
		{"production cookies are Secure", EnvProduction, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{AppEnv: tt.appEnv}
			if got := cfg.SecureCookies(); got != tt.want {
				t.Errorf("SecureCookies(%q) = %t, want %t", tt.appEnv, got, tt.want)
			}
		})
	}
}

func TestLoadProductionFailClosed(t *testing.T) {
	tests := []struct {
		name          string
		baseURL       string
		trustedOrigin string
		wantErr       bool
	}{
		{
			name:          "unset TRUSTED_ORIGIN rejected",
			baseURL:       "https://v2.sickfansubs.com",
			trustedOrigin: "",
			wantErr:       true,
		},
		{
			name:          "http TRUSTED_ORIGIN rejected",
			baseURL:       "https://v2.sickfansubs.com",
			trustedOrigin: "http://v2.sickfansubs.com",
			wantErr:       true,
		},
		{
			// Unset PUBLIC_BASE_URL falls back to the http dev default,
			// which production must reject rather than silently accept.
			name:          "unset PUBLIC_BASE_URL rejected",
			baseURL:       "",
			trustedOrigin: "https://v2.sickfansubs.com",
			wantErr:       true,
		},
		{
			name:          "http PUBLIC_BASE_URL rejected",
			baseURL:       "http://v2.sickfansubs.com",
			trustedOrigin: "https://v2.sickfansubs.com",
			wantErr:       true,
		},
		{
			name:          "https both accepted",
			baseURL:       "https://v2.sickfansubs.com",
			trustedOrigin: "https://v2.sickfansubs.com",
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("PORT")
			os.Unsetenv("DATA_DIR")
			os.Setenv("APP_ENV", EnvProduction)
			defer os.Unsetenv("APP_ENV")
			if tt.baseURL == "" {
				os.Unsetenv("PUBLIC_BASE_URL")
			} else {
				os.Setenv("PUBLIC_BASE_URL", tt.baseURL)
				defer os.Unsetenv("PUBLIC_BASE_URL")
			}
			if tt.trustedOrigin == "" {
				os.Unsetenv("TRUSTED_ORIGIN")
			} else {
				os.Setenv("TRUSTED_ORIGIN", tt.trustedOrigin)
				defer os.Unsetenv("TRUSTED_ORIGIN")
			}
			// Relay config for the accepted case (production requires SMTP_HOST +
			// SMTP_FROM). Set for all cases — the wantErr
			// cases fail on their own earlier checks.
			os.Setenv("SMTP_HOST", "smtp.example.com")
			defer os.Unsetenv("SMTP_HOST")
			os.Setenv("SMTP_FROM", "beta@example.com")
			defer os.Unsetenv("SMTP_FROM")

			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatal("got nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.AppEnv != EnvProduction {
				t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, EnvProduction)
			}
			if cfg.TrustedOrigin != tt.trustedOrigin {
				t.Errorf("TrustedOrigin = %q, want %q", cfg.TrustedOrigin, tt.trustedOrigin)
			}
		})
	}
}

// TestLoadSMTP covers the relay hygiene rules that Load
// enforces for EVERY process: the port whitelist, the CR/LF rejection, and
// the defaults. The environment requirements (production host/from, the
// loopback-only dev fallback) live in ValidateMailRelay — the next test.
func TestLoadSMTP(t *testing.T) {
	devBase := func(t *testing.T) {
		t.Helper()
		os.Unsetenv("PORT")
		os.Unsetenv("DATA_DIR")
		os.Unsetenv("APP_ENV")
		os.Setenv("PUBLIC_BASE_URL", defaultPublicBaseURL)
		t.Cleanup(func() { os.Unsetenv("PUBLIC_BASE_URL") })
		os.Unsetenv("TRUSTED_ORIGIN")
		os.Unsetenv("SMTP_HOST")
		os.Unsetenv("SMTP_PORT")
		os.Unsetenv("SMTP_USERNAME")
		os.Unsetenv("SMTP_FROM")
	}

	t.Run("dev defaults", func(t *testing.T) {
		devBase(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.SMTPHost != "" || cfg.SMTPPort != 587 || cfg.SMTPUsername != "resend" ||
			cfg.SMTPFrom != "noreply@sickfansubs.com" {
			t.Errorf("unexpected dev SMTP defaults: %+v", cfg)
		}
	})

	t.Run("username surrounding whitespace trimmed", func(t *testing.T) {
		devBase(t)
		os.Setenv("SMTP_USERNAME", "  mailer-x  ")
		t.Cleanup(func() { os.Unsetenv("SMTP_USERNAME") })
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.SMTPUsername != "mailer-x" {
			t.Errorf("SMTPUsername = %q, want %q (surrounding whitespace trimmed)", cfg.SMTPUsername, "mailer-x")
		}
	})

	t.Run("whitespace-only username is the default", func(t *testing.T) {
		devBase(t)
		os.Setenv("SMTP_USERNAME", "   ")
		t.Cleanup(func() { os.Unsetenv("SMTP_USERNAME") })
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.SMTPUsername != "resend" {
			t.Errorf("SMTPUsername = %q, want the resend default", cfg.SMTPUsername)
		}
	})

	t.Run("port whitelist", func(t *testing.T) {
		for _, port := range []string{"25", "465", "587", "2465", "2587"} {
			t.Run(port+" accepted", func(t *testing.T) {
				devBase(t)
				os.Setenv("SMTP_PORT", port)
				if _, err := Load(); err != nil {
					t.Errorf("SMTP_PORT=%s rejected: %v", port, err)
				}
				os.Unsetenv("SMTP_PORT")
			})
		}
		for _, port := range []string{"0", "8080", "not-a-port"} {
			t.Run(port+" rejected", func(t *testing.T) {
				devBase(t)
				os.Setenv("SMTP_PORT", port)
				if _, err := Load(); err == nil {
					t.Errorf("SMTP_PORT=%s accepted, want error", port)
				}
				os.Unsetenv("SMTP_PORT")
			})
		}
	})

	t.Run("CRLF rejected in from", func(t *testing.T) {
		devBase(t)
		os.Setenv("SMTP_FROM", "a@b.c\r\nBcc: evil@x.y")
		if _, err := Load(); err == nil {
			t.Error("SMTP_FROM with CRLF accepted")
		}
	})
}

// TestValidateMailRelay covers the API-surface-only environment
// requirements: production host/from and the loopback-only dev fallback.
func TestValidateMailRelay(t *testing.T) {
	devBase := func(t *testing.T, baseURL string) *Config {
		t.Helper()
		os.Unsetenv("PORT")
		os.Unsetenv("DATA_DIR")
		os.Unsetenv("APP_ENV")
		if baseURL == "" {
			os.Unsetenv("PUBLIC_BASE_URL")
		} else {
			os.Setenv("PUBLIC_BASE_URL", baseURL)
			t.Cleanup(func() { os.Unsetenv("PUBLIC_BASE_URL") })
		}
		os.Unsetenv("TRUSTED_ORIGIN")
		os.Unsetenv("SMTP_HOST")
		os.Unsetenv("SMTP_PORT")
		os.Unsetenv("SMTP_USERNAME")
		os.Unsetenv("SMTP_FROM")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		return cfg
	}

	t.Run("dev fallback is loopback-only", func(t *testing.T) {
		cfg := devBase(t, "https://dev.example.com")
		if err := cfg.ValidateMailRelay(); err == nil {
			t.Fatal("got nil, want loopback-only error")
		}
		cfg = devBase(t, "")
		if err := cfg.ValidateMailRelay(); err != nil {
			t.Errorf("loopback dev fallback rejected: %v", err)
		}
	})

	t.Run("dev with host passes any base URL", func(t *testing.T) {
		cfg := devBase(t, "https://dev.example.com")
		cfg.SMTPHost = "smtp.example.com"
		if err := cfg.ValidateMailRelay(); err != nil {
			t.Fatalf("ValidateMailRelay: %v", err)
		}
	})

	t.Run("production requires host and from", func(t *testing.T) {
		os.Setenv("APP_ENV", EnvProduction)
		t.Cleanup(func() { os.Unsetenv("APP_ENV") })
		os.Setenv("PUBLIC_BASE_URL", "https://v2.sickfansubs.com")
		t.Cleanup(func() { os.Unsetenv("PUBLIC_BASE_URL") })
		os.Setenv("TRUSTED_ORIGIN", "https://v2.sickfansubs.com")
		t.Cleanup(func() { os.Unsetenv("TRUSTED_ORIGIN") })

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if err := cfg.ValidateMailRelay(); err == nil {
			t.Error("production without SMTP_HOST accepted")
		}
		cfg.SMTPHost = "smtp.resend.com"
		if err := cfg.ValidateMailRelay(); err == nil {
			t.Error("production without SMTP_FROM accepted")
		}
		cfg.SMTPFrom = "beta@sickfansubs.com"
		if err := cfg.ValidateMailRelay(); err != nil {
			t.Errorf("complete production SMTP config rejected: %v", err)
		}
	})
}

func TestPortFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{"default", "", 8080},
		{"valid", "3000", 3000},
		{"invalid", "abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "" {
				os.Unsetenv("PORT")
			} else {
				os.Setenv("PORT", tt.env)
				defer os.Unsetenv("PORT")
			}
			got := portFromEnv()
			if got != tt.want {
				t.Errorf("portFromEnv() = %d, want %d", got, tt.want)
			}
		})
	}
}
