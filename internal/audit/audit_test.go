package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"sick-fansubs/internal/logging"
)

// fakeWriter records every record it is asked to write and can be made to
// fail.
type fakeWriter struct {
	records []Record
	err     error
}

func (f *fakeWriter) Write(_ context.Context, rec Record) error {
	f.records = append(f.records, rec)
	return f.err
}

// logLines decodes every JSON log line in buf into a map.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		lines = append(lines, rec)
	}
	return lines
}

// lineWithMsg returns the log line whose msg field matches, or fails the test.
func lineWithMsg(t *testing.T, lines []map[string]any, msg string) map[string]any {
	t.Helper()
	for _, l := range lines {
		if l["msg"] == msg {
			return l
		}
	}
	t.Fatalf("no log line with msg %q in %v", msg, lines)
	return nil
}

// NOTE: these tests are deliberately NOT parallel — they register the
// package-global writer (set once at startup), and concurrent tests would
// observe each other's registrations.

func TestEvent_DualWritesToSlogAndWriter(t *testing.T) {
	fw := &fakeWriter{}
	SetWriter(fw)
	defer SetWriter(nil)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("requestId", "req-1")
	Event(logging.With(t.Context(), logger), EventSignInSuccess, ResultSuccess, "user-1", "req-1", "192.0.2.1:1234")

	if len(fw.records) != 1 {
		t.Fatalf("writer records: got %d, want 1", len(fw.records))
	}
	rec := fw.records[0]
	if rec.Event != EventSignInSuccess || rec.Result != ResultSuccess {
		t.Errorf("record event/result: got %q/%q", rec.Event, rec.Result)
	}
	if rec.ActorID != "user-1" {
		t.Errorf("record actorID: got %q, want user-1", rec.ActorID)
	}
	if rec.TargetID != "" {
		t.Errorf("record targetID: got %q, want empty for actor-only events", rec.TargetID)
	}
	if rec.RequestID != "req-1" || rec.RemoteAddr != "192.0.2.1:1234" {
		t.Errorf("record request/remote: got %q/%q", rec.RequestID, rec.RemoteAddr)
	}

	logLine := lineWithMsg(t, logLines(t, &buf), "audit")
	if logLine["event"] != EventSignInSuccess || logLine["result"] != ResultSuccess {
		t.Errorf("slog event/result: got %v/%v", logLine["event"], logLine["result"])
	}
	if logLine["actorId"] != "user-1" {
		t.Errorf("slog actorID: got %v, want user-1", logLine["actorId"])
	}
	if _, ok := logLine["targetId"]; ok {
		t.Errorf("slog must omit targetID for actor-only events, got %v", logLine["targetId"])
	}
	if logLine["remoteAddr"] != "192.0.2.1:1234" {
		t.Errorf("slog remoteAddr: got %v", logLine["remoteAddr"])
	}
	if logLine["requestId"] != "req-1" {
		t.Errorf("slog requestId: got %v, want the request-scoped logger's req-1", logLine["requestId"])
	}
}

func TestEventWithTarget_CarriesBothIDs(t *testing.T) {
	fw := &fakeWriter{}
	SetWriter(fw)
	defer SetWriter(nil)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	EventWithTarget(logging.With(t.Context(), logger), EventRoleChanged, ResultSuccess, "admin-1", "user-9", "moderator", "req-2", "203.0.113.5:80")

	if len(fw.records) != 1 {
		t.Fatalf("writer records: got %d, want 1", len(fw.records))
	}
	rec := fw.records[0]
	if rec.ActorID != "admin-1" || rec.TargetID != "user-9" {
		t.Errorf("record actor/target: got %q/%q, want admin-1/user-9", rec.ActorID, rec.TargetID)
	}
	if rec.TargetRole != "moderator" {
		t.Errorf("record targetRole: got %q, want moderator", rec.TargetRole)
	}

	logLine := lineWithMsg(t, logLines(t, &buf), "audit")
	if logLine["actorId"] != "admin-1" || logLine["targetId"] != "user-9" {
		t.Errorf("slog actor/target: got %v/%v, want admin-1/user-9", logLine["actorId"], logLine["targetId"])
	}
	if logLine["targetRole"] != "moderator" {
		t.Errorf("slog targetRole: got %v, want moderator", logLine["targetRole"])
	}
}

func TestEvent_UnknownActorOmitted(t *testing.T) {
	fw := &fakeWriter{}
	SetWriter(fw)
	defer SetWriter(nil)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	Event(logging.With(t.Context(), logger), EventSignInFailure, ResultFailure, "", "req-3", "198.51.100.7:9")

	if len(fw.records) != 1 {
		t.Fatalf("writer records: got %d, want 1", len(fw.records))
	}
	if rec := fw.records[0]; rec.ActorID != "" {
		t.Errorf("record actorID: got %q, want empty", rec.ActorID)
	}
	logLine := lineWithMsg(t, logLines(t, &buf), "audit")
	if _, ok := logLine["actorId"]; ok {
		t.Errorf("slog must omit actorID when unknown (enumeration resistance), got %v", logLine["actorId"])
	}
}

func TestEvent_NilWriterLogsOnly(t *testing.T) {
	SetWriter(nil) // the default; explicit for clarity

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	Event(logging.With(t.Context(), logger), EventSignOut, ResultSuccess, "user-2", "req-4", "192.0.2.9:1")

	lines := logLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("log lines: got %d, want exactly the audit record", len(lines))
	}
	if lines[0]["msg"] != "audit" || lines[0]["event"] != EventSignOut {
		t.Errorf("slog record: got msg=%v event=%v", lines[0]["msg"], lines[0]["event"])
	}
}

func TestEvent_WriteFailureLoggedNotPropagated(t *testing.T) {
	fw := &fakeWriter{err: errors.New("db closed")}
	SetWriter(fw)
	defer SetWriter(nil)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil)).With("requestId", "req-5")
	// Event returns nothing — a writer failure must never propagate to the
	// audited operation.
	Event(logging.With(t.Context(), logger), EventContentCreated, ResultSuccess, "admin-2", "req-5", "192.0.2.2:2")

	if len(fw.records) != 1 {
		t.Fatalf("writer records: got %d, want 1", len(fw.records))
	}
	lines := logLines(t, &buf)

	audit := lineWithMsg(t, lines, "audit")
	if audit["event"] != EventContentCreated {
		t.Errorf("slog half must survive a writer failure, got %v", audit["event"])
	}

	failed := lineWithMsg(t, lines, "audit write failed")
	if failed["event"] != EventContentCreated {
		t.Errorf("failure record event: got %v, want content_created", failed["event"])
	}
	// The failure record is not an audit event, but it is request-scoped:
	// it inherits the logger's requestId like every other record.
	if failed["requestId"] != "req-5" {
		t.Errorf("failure record requestId: got %v, want the inherited req-5", failed["requestId"])
	}
	if failed["error"] == nil || failed["error"] == "" {
		t.Errorf("failure record must carry the writer error, got %v", failed["error"])
	}
}

func TestSetWriter_TypedNilTreatedAsUnregistered(t *testing.T) {
	// A typed-nil writer (interface non-nil, concrete pointer nil) must not
	// panic the emitter — SetWriter guards it to nil.
	var fw *fakeWriter
	SetWriter(fw)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	Event(logging.With(t.Context(), logger), EventSignOut, ResultSuccess, "user-3", "req-6", "192.0.2.3:3")

	lines := logLines(t, &buf)
	if len(lines) != 1 || lines[0]["msg"] != "audit" {
		t.Errorf("typed-nil writer must act as unregistered, got lines %v", lines)
	}
	SetWriter(nil)
}

// TestEvent_EmptyRemoteAddrIsEmitted pins the break-glass call shape's slog
// half: remoteAddr is always an attribute (emit's contract), so an empty
// address must still be emitted as an empty string.
func TestEvent_EmptyRemoteAddrIsEmitted(t *testing.T) {
	fw := &fakeWriter{}
	SetWriter(fw)
	defer SetWriter(nil)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	EventWithTarget(logging.With(t.Context(), logger), EventPasswordReset, ResultSuccess,
		"maintenance", "target-1", "moderator", "", "")

	if len(fw.records) != 1 {
		t.Fatalf("writer records: got %d, want 1", len(fw.records))
	}
	logLine := lineWithMsg(t, logLines(t, &buf), "audit")
	if v, ok := logLine["remoteAddr"]; !ok || v != "" {
		t.Errorf("slog must carry remoteAddr even when empty, got %v (present=%t)", v, ok)
	}
}

// TestAccountSecurityEvents pins the reconciliation list exactly: the seven
// events that change durable account security state. The staged-restore
// refusal filters on this list, so an omission silently weakens the guard and
// an addition silently broadens it. Deliberately excluded: the ledger-only
// events that change no credential, role, status, or account existence
// (sign-in/out, email verification and change, session revoke), and
// password_reset_requested (no durable state change).
func TestAccountSecurityEvents(t *testing.T) {
	want := []string{
		EventPasswordChanged,
		EventPasswordReset,
		EventRoleChanged,
		EventUserSuspended,
		EventUserReactivated,
		EventUserDeleted,
		EventPasswordResetCompleted,
	}
	got := AccountSecurityEvents()
	if !slices.Equal(got, want) {
		t.Errorf("AccountSecurityEvents() = %v, want %v", got, want)
	}
	// The returned slice is a copy — mutating it must not corrupt the list.
	got[0] = "corrupted"
	if AccountSecurityEvents()[0] == "corrupted" {
		t.Error("AccountSecurityEvents returned a shared slice")
	}
}
