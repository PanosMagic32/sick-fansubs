package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// TestFromReturnsTheDefaultWithoutALogger pins the answer every caller relies
// on: a context that carries no logger still yields a usable one.
func TestFromReturnsTheDefaultWithoutALogger(t *testing.T) {
	t.Parallel()
	if got := From(context.Background()); got != slog.Default() {
		t.Errorf("From(empty) = %p, want the process default %p", got, slog.Default())
	}
}

// TestFromReturnsANilFreeLogger pins the second half of the contract: a
// context explicitly carrying nil must not hand callers a nil logger.
func TestFromReturnsANilFreeLogger(t *testing.T) {
	t.Parallel()
	if got := From(With(context.Background(), nil)); got == nil {
		t.Fatal("From(ctx with nil logger) = nil, want the default logger")
	}
}

// TestWithAndFromRoundTrip pins that the logger attached by With is the one
// From returns, and that a record written through it reaches the sink with the
// fields the logger carries.
func TestWithAndFromRoundTrip(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("requestId", "abc123")

	From(With(context.Background(), logger)).InfoContext(context.Background(), "probe")

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if line["msg"] != "probe" || line["requestId"] != "abc123" {
		t.Errorf("record = %v, want msg=probe requestId=abc123", line)
	}
}
