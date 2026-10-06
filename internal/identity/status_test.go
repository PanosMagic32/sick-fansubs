package identity

import "testing"

func TestIsValidStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"active", StatusActive, true},
		{"suspended", StatusSuspended, true},
		{"unknown value", "banned", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsValidStatus(tt.status); got != tt.want {
				t.Errorf("IsValidStatus(%q) = %t, want %t", tt.status, got, tt.want)
			}
		})
	}
}
