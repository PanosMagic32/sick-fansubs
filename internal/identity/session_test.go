package identity

import (
	"testing"
	"time"
)

// TestSessionTimingPolicy pins the documented 30/90/7 policy directly:
// middleware tests derive their expectations from the constants, so a silent
// change would otherwise stay green.
func TestSessionTimingPolicy(t *testing.T) {
	t.Parallel()

	if SessionLifetime != 30*24*time.Hour {
		t.Errorf("SessionLifetime = %v, want 30 days", SessionLifetime)
	}
	if SessionMaxLifetime != 90*24*time.Hour {
		t.Errorf("SessionMaxLifetime = %v, want 90 days", SessionMaxLifetime)
	}
	if SessionRenewBefore != 7*24*time.Hour {
		t.Errorf("SessionRenewBefore = %v, want 7 days", SessionRenewBefore)
	}
}

func TestSessionUser_IsExpired(t *testing.T) {
	t.Parallel()
	const expiresAtMS = 2000
	su := &SessionUser{ExpiresAtMS: expiresAtMS}

	tests := []struct {
		name  string
		nowMS int64
		want  bool
	}{
		{"before expiry", expiresAtMS - 1, false},
		{"at the expiry instant", expiresAtMS, true},
		{"after expiry", expiresAtMS + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := su.IsExpired(tt.nowMS); got != tt.want {
				t.Errorf("IsExpired(%d) = %t, want %t", tt.nowMS, got, tt.want)
			}
		})
	}
}

func TestSessionUser_ToPublic(t *testing.T) {
	t.Parallel()
	su := &SessionUser{UserID: "user_01", Username: "Katakuri", Role: RoleModerator}

	got := su.ToPublic()
	want := PublicUser{ID: "user_01", Username: "Katakuri", Role: RoleModerator}
	if got != want {
		t.Errorf("ToPublic() = %+v, want %+v", got, want)
	}
}
