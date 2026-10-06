package migration

import "testing"

// TestDeriveTargetID pins the derivation: deterministic, 32-hex shaped,
// distinct from the legacy ObjectId and across distinct sources.
func TestDeriveTargetID(t *testing.T) {
	const source = "65b0a1c2d3e4f5a6b7c8d9e0"
	first := deriveTargetID(source)

	if !hex32.MatchString(first) {
		t.Errorf("derived id %q is not 32-hex", first)
	}
	if again := deriveTargetID(source); again != first {
		t.Errorf("derivation is not deterministic: %q != %q", again, first)
	}
	if first == source {
		t.Errorf("derived id equals the legacy ObjectId %q", source)
	}
	if other := deriveTargetID("65b0a1c2d3e4f5a6b7c8d9e1"); other == first {
		t.Errorf("distinct sources derived the same id %q", first)
	}
}

// TestDeriveTargetID_GoldenValue pins the key constant itself: the
// derivation is a published contract (the favorites import and the identical
// derived content IDs on a fresh target depend on it), so a typo in
// `targetIDDomainKey` or the truncation must fail the suite — silently
// changing every content ID is a breaking migration change, not a refactor.
func TestDeriveTargetID_GoldenValue(t *testing.T) {
	cases := []struct {
		oid  string
		want string
	}{
		{"65b0a1c2d3e4f5a6b7c8d9e0", "4cd054a92187507a666b6dbaa3294924"},
		{"65c0a1c2d3e4f5a6b7c8d9e2", "190e8205ca4e7d0cb768402dfad1d38b"},
		{"65b0d1a2b3c4d5e6f7a8b9e0", "4f1f574c097d694cb510fde2500d55ed"},
	}
	for _, tc := range cases {
		if got := deriveTargetID(tc.oid); got != tc.want {
			t.Errorf("deriveTargetID(%q) = %q, want %q (the golden value — key or truncation changed)", tc.oid, got, tc.want)
		}
	}
}
