package store

import (
	"database/sql"
	"testing"
)

func TestNullableString_EmptyBecomesNull(t *testing.T) {
	t.Parallel()

	if got := nullableString(""); got != nil {
		t.Errorf("nullableString(%q) = %v, want nil", "", got)
	}
	if got := nullableString("device"); got != "device" {
		t.Errorf("nullableString(%q) = %v, want %q", "device", got, "device")
	}
}

func TestNullStringPtr_ValidBecomesPointer(t *testing.T) {
	t.Parallel()

	if got := nullStringPtr(sql.NullString{}); got != nil {
		t.Errorf("nullStringPtr(invalid) = pointer to %q, want a nil pointer", *got)
	}
	got := nullStringPtr(sql.NullString{String: "avatar.jpg", Valid: true})
	if got == nil || *got != "avatar.jpg" {
		t.Errorf("nullStringPtr(valid) = %v, want pointer to %q", got, "avatar.jpg")
	}
}

func TestNullStringValue_InvalidBecomesEmpty(t *testing.T) {
	t.Parallel()

	if got := nullStringValue(sql.NullString{}); got != "" {
		t.Errorf("nullStringValue(invalid) = %q, want %q", got, "")
	}
	if got := nullStringValue(sql.NullString{String: "u1", Valid: true}); got != "u1" {
		t.Errorf("nullStringValue(valid) = %q, want %q", got, "u1")
	}
}
