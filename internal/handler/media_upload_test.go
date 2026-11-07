package handler

import (
	"context"
	"testing"
	"testing/synctest"
)

// TestUploadSlotCapsConcurrentWork pins the four-slot gate: a fifth upload
// waits for a release, and a canceled waiter never takes a slot. The slot is
// taken before the multipart parse, so it bounds the spooled temp file as
// well as the decode. The blocking half runs in a synctest bubble, so the
// wait is deterministic instead of a wall-clock sleep.
func TestUploadSlotCapsConcurrentWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// The package channel was created before the bubble, so a block on it
		// is not durably blocked. Swap in a bubble-local channel for this test
		// and restore it before the bubble exits.
		oldSlots := uploadWorkSlots
		uploadWorkSlots = make(chan struct{}, 4)
		defer func() { uploadWorkSlots = oldSlots }()

		for range 4 {
			if !acquireUploadSlot(context.Background()) {
				t.Fatal("acquireUploadSlot returned false with a free slot")
			}
		}

		// The context's Done channel must be created inside the bubble; a
		// t.Context() channel is external and the select would not count as
		// durably blocked.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		acquired := make(chan bool, 1)
		go func() { acquired <- acquireUploadSlot(ctx) }()
		synctest.Wait()

		select {
		case <-acquired:
			t.Fatal("a fifth upload acquired a slot before a release")
		default:
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
			if !acquireUploadSlot(context.Background()) {
				t.Fatal("acquireUploadSlot returned false with a free slot")
			}
		}
		canceled, cancelNow := context.WithCancel(context.Background())
		cancelNow()
		if acquireUploadSlot(canceled) {
			t.Fatal("canceled waiter acquired a slot")
		}
		for range 4 {
			releaseUploadSlot()
		}
	})
}
