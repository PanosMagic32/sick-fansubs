package id_test

import (
	"strings"
	"testing"

	"sick-fansubs/internal/id"
)

// fallbackPrefix is the degraded-value marker; the wire shape it preserves is
// a contract (X-Request-ID is 32 characters whether minted or degraded).
const fallbackPrefix = "ffffffffffff"

// isLowerHex32 reports whether s is 32 lowercase hexadecimal characters.
func isLowerHex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func TestNew_Shape(t *testing.T) {
	t.Parallel()

	got, err := id.New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !isLowerHex32(got) {
		t.Errorf("New() = %q, want 32 lowercase hex characters", got)
	}
}

func TestNew_Unique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 1000)
	for range 1000 {
		got, err := id.New()
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if seen[got] {
			t.Fatalf("New() repeated %q", got)
		}
		seen[got] = true
	}
}

func TestFallback_Shape(t *testing.T) {
	t.Parallel()

	for range 100 {
		got := id.Fallback()
		if !isLowerHex32(got) {
			t.Errorf("Fallback() = %q, want 32 lowercase hex characters", got)
			continue
		}
		if !strings.HasPrefix(got, fallbackPrefix) {
			t.Errorf("Fallback() = %q, want the %s marker prefix", got, fallbackPrefix)
		}
	}
}

func TestFallback_UniqueAcrossCalls(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, 1000)
	for range 1000 {
		got := id.Fallback()
		if seen[got] {
			t.Fatalf("Fallback() repeated %q", got)
		}
		seen[got] = true
	}
}
