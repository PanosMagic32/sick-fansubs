package handler

import (
	"errors"
	"testing"

	"sick-fansubs/internal/store"
)

func TestMintRetry_RetriesDuplicateThenSucceeds(t *testing.T) {
	t.Parallel()

	calls := 0
	got, err := mintRetry(func() (int, error) {
		calls++
		if calls < maxMintAttempts {
			return 0, errors.Join(store.ErrDuplicate, errors.New("collision"))
		}
		return 7, nil
	})
	if err != nil {
		t.Fatalf("mintRetry: %v", err)
	}
	if got != 7 || calls != maxMintAttempts {
		t.Errorf("result = %d after %d calls, want 7 after %d", got, calls, maxMintAttempts)
	}
}

func TestMintRetry_StopsOnOtherErrors(t *testing.T) {
	t.Parallel()

	calls := 0
	want := errors.New("mint failed")
	_, err := mintRetry(func() (int, error) {
		calls++
		return 0, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the original failure", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (a non-collision error is not retried)", calls)
	}
}

func TestMintRetry_ReturnsLastCollisionOnExhaustion(t *testing.T) {
	t.Parallel()

	calls := 0
	_, err := mintRetry(func() (int, error) {
		calls++
		return 0, store.ErrDuplicate
	})
	if !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
	if calls != maxMintAttempts {
		t.Errorf("calls = %d, want the bounded budget %d", calls, maxMintAttempts)
	}
}
