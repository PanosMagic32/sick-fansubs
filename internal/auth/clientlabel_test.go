package auth

import (
	"context"
	"database/sql"
	"testing"
)

// Client-label classifier tests. The classifier is the
// privacy boundary for the session list: whatever a client sends, the stored
// value is one of the vocabulary strings or the empty string.

func TestSessionClientLabel_Matrix(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, ua, want string }{
		{
			"chrome android",
			"Mozilla/5.0 (Linux; Android 13; SM-A536B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			"Chrome · Android",
		},
		{
			"chrome windows",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Chrome · Windows",
		},
		{
			"chrome linux",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
			"Chrome · Linux",
		},
		{
			"chrome ios (crios)",
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/120.0.0.0 Mobile/15E148 Safari/604.1",
			"Chrome · iOS",
		},
		{
			"safari ios",
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
			"Safari · iOS",
		},
		{
			"safari ipad",
			"Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1",
			"Safari · iOS",
		},
		{
			"safari macos",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
			"Safari · macOS",
		},
		{
			"firefox windows",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
			"Firefox · Windows",
		},
		{
			"firefox android",
			"Mozilla/5.0 (Android 13; Mobile; rv:121.0) Gecko/121.0 Firefox/121.0",
			"Firefox · Android",
		},
		{
			"firefox ios (fxios)",
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/121.0 Mobile/15E148 Safari/605.1.15",
			"Firefox · iOS",
		},
		{
			"edge desktop",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
			"Edge · Windows",
		},
		{
			"edge ios",
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) EdgiOS/120.0 Mobile/15E148 Safari/605.1.15",
			"Edge · iOS",
		},
		{
			"opera desktop",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 OPR/106.0.0.0",
			"Opera · Linux",
		},
		{
			"opera ios (opios)",
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) OPiOS/2.2.0 Mobile/15E148 Safari/9537.53",
			"Opera · iOS",
		},
		{
			"samsung internet android",
			"Mozilla/5.0 (Linux; Android 13; SM-A536B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36",
			"Samsung Internet · Android",
		},
		{
			"platform only",
			"Mozilla/5.0 (Windows NT 10.0)",
			"Windows",
		},
		{"empty", "", ""},
		{"non-browser client", "curl/8.4.0", ""},
		{"unknown token soup", "SomeCustomClient/2.1 (compatible)", ""},
		{
			// The classifier returns its OWN vocabulary, never client text:
			// the hostile payload has no path into the stored value.
			"hostile input yields a vocabulary label",
			"Chrome/1 (; DROP TABLE sessions; --) UnknownOS",
			"Chrome",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sessionClientLabel(tc.ua); got != tc.want {
				t.Errorf("sessionClientLabel(%q) = %q, want %q", tc.ua, got, tc.want)
			}
		})
	}
}

// TestSessionClientLabel_VocabularyIsBounded pins the closed-vocabulary
// properties: every browser×platform combination fits the documented bound,
// and no combination can produce a value longer than it — the guard rail in
// sessionClientLabel is unreachable with today's tables, and this test is
// what keeps that true.
func TestSessionClientLabel_VocabularyIsBounded(t *testing.T) {
	t.Parallel()

	for _, b := range browserTokens {
		for _, p := range platformTokens {
			label := b.label + clientLabelSeparator + p.label
			if len(label) > sessionClientLabelMaxLen {
				t.Errorf("label %q is %d bytes, over the %d-byte bound", label, len(label), sessionClientLabelMaxLen)
			}
		}
	}
}

// TestAuth_StoresClientLabel proves end to end that a sign-in and a
// registration write the CLASSIFIER's label — never the raw user-agent — and
// that an unrecognized agent stores SQL NULL.
func TestAuth_StoresClientLabel(t *testing.T) {
	t.Parallel()
	db := openServiceDB(t)
	ctx := context.Background()
	svc := newTestService(db)

	const rawUA = "Mozilla/5.0 (Linux; Android 13; SM-A536B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

	_, _, user, err := svc.Register(ctx, "Labeled", "labeled@example.com", "password123", rawUA)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := storedClientLabel(t, db, user.ID); got != "Chrome · Android" {
		t.Errorf("registration label: got %q, want %q", got, "Chrome · Android")
	}

	// A second sign-in with an unrecognized agent stores NULL.
	if _, _, _, _, err := svc.SignIn(ctx, "Labeled", "password123", "SomeCustomClient/2.1"); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	var nullRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ? AND client_label IS NULL`, user.ID).Scan(&nullRows); err != nil {
		t.Fatalf("count null labels: %v", err)
	}
	if nullRows != 1 {
		t.Errorf("unrecognized agent rows with NULL label = %d, want 1", nullRows)
	}

	// The raw header never lands in the column, whatever the client sends.
	var leaked int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE client_label LIKE '%Mozilla%'`).Scan(&leaked); err != nil {
		t.Fatalf("scan for leaked user-agent: %v", err)
	}
	if leaked != 0 {
		t.Errorf("%d session rows contain raw user-agent text", leaked)
	}
}

// storedClientLabel reads the client_label of a user's single session row.
func storedClientLabel(t *testing.T, db *sql.DB, userID string) string {
	t.Helper()
	var label sql.NullString
	if err := db.QueryRow(`SELECT client_label FROM sessions WHERE user_id = ?`, userID).Scan(&label); err != nil {
		t.Fatalf("read client_label: %v", err)
	}
	return label.String
}
