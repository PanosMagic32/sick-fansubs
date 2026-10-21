package auth

import (
	"errors"
	"fmt"
	"testing"

	"sick-fansubs/internal/store"
)

// TestRetryDuplicate_SucceedsAfterCollisions pins the bounded retry: the
// first two collisions are retried and the third attempt's value is returned.
func TestRetryDuplicate_SucceedsAfterCollisions(t *testing.T) {
	t.Parallel()

	calls := 0
	got, err := retryDuplicate(func() (string, error) {
		calls++
		if calls < 3 {
			return "", fmt.Errorf("%w: digest", store.ErrDuplicate)
		}
		return "value", nil
	})
	if err != nil {
		t.Fatalf("retryDuplicate: %v", err)
	}
	if got != "value" {
		t.Errorf("value = %q, want %q", got, "value")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

// TestRetryDuplicate_ReturnsLastCollision pins the exhausted budget: the
// caller receives the final collision, never a zero value with nil error.
func TestRetryDuplicate_ReturnsLastCollision(t *testing.T) {
	t.Parallel()

	calls := 0
	_, err := retryDuplicate(func() (string, error) {
		calls++
		return "", fmt.Errorf("%w: attempt %d", store.ErrDuplicate, calls)
	})
	if !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("err = %v, want store.ErrDuplicate", err)
	}
	if calls != duplicateAttempts {
		t.Errorf("calls = %d, want %d", calls, duplicateAttempts)
	}
}

// TestRetryDuplicate_StopsOnOtherError pins the non-collision arm: an
// unrelated failure returns immediately without spending the budget.
func TestRetryDuplicate_StopsOnOtherError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	calls := 0
	_, err := retryDuplicate(func() (string, error) {
		calls++
		return "", sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}
