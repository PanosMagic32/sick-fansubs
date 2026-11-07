package handler

import (
	"context"
	"testing"
	"time"
)

// TestUploadSlotCapsConcurrentWork pins the four-slot gate: a fifth upload
// waits for a release, and a canceled waiter never takes a slot. The slot is
// taken before the multipart parse, so it bounds the spooled temp file as
// well as the decode.
func TestUploadSlotCapsConcurrentWork(t *testing.T) {
	for range 4 {
		if !acquireUploadSlot(t.Context()) {
			t.Fatal("acquireUploadSlot returned false with a free slot")
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	acquired := make(chan bool, 1)
	go func() { acquired <- acquireUploadSlot(ctx) }()

	select {
	case <-acquired:
		t.Fatal("a fifth upload acquired a slot before a release")
	case <-time.After(20 * time.Millisecond):
	}

	releaseUploadSlot()
	if !<-acquired {
		t.Fatal("waiter did not acquire after release")
	}
	for range 4 {
		releaseUploadSlot()
	}

	if len(uploadWorkSlots) != 0 {
		t.Fatalf("slots held after cleanup = %d, want 0", len(uploadWorkSlots))
	}

	for range 4 {
		if !acquireUploadSlot(t.Context()) {
			t.Fatal("acquireUploadSlot returned false with a free slot")
		}
	}
	canceled, cancelNow := context.WithCancel(t.Context())
	cancelNow()
	if acquireUploadSlot(canceled) {
		t.Fatal("canceled waiter acquired a slot")
	}
	for range 4 {
		releaseUploadSlot()
	}
}
