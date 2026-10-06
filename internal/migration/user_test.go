// User transform, verifier classification, and user import tests.

package migration

import (
	"encoding/json"
	"strings"
	"testing"
)

// verifierBody is a 53-char synthetic bcrypt body (22 salt + 31 hash chars),
// making every testVerifier* string a full 60-char hash like the real thing.
// Synthetic only — never a real hash.
const verifierBody = "abcdefghijklmnopqrstuvABCDEFGHIJKLMNOPQRSTUVWXYZ01234"

// v builds a full-length verifier for a prefix and cost.
func v(prefix, cost string) string {
	return prefix + cost + "$" + verifierBody
}

func TestClassifyVerifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		verifier string
		category verifierCategory
		prefix   string
		cost     int
	}{
		{"2a cost 12", v("$2a$", "12"), categorySupported, "$2a$", 12},
		{"2b cost 12", v("$2b$", "12"), categorySupported, "$2b$", 12},
		{"2y cost 12", v("$2y$", "12"), categorySupported, "$2y$", 12},
		{"2a cost 4 (minimum)", v("$2a$", "04"), categorySupported, "$2a$", 4},
		{"2a cost 10 (rehash on sign-in)", v("$2a$", "10"), categorySupported, "$2a$", 10},
		{"2b cost 13 (operational maximum)", v("$2b$", "13"), categorySupported, "$2b$", 13},
		{"2b cost 14 (above maximum)", v("$2b$", "14"), categoryAboveMax, "$2b$", 14},
		{"2a cost 31 (parse ceiling)", v("$2a$", "31"), categoryAboveMax, "$2a$", 31},
		{"2x (PHP variant — Go would accept it)", v("$2x$", "12"), categoryUnsupported, "$2x$", 0},
		{"2z (unknown minor)", v("$2z$", "12"), categoryUnsupported, "$2z$", 0},
		{"major version 1", v("$1a$", "12"), categoryUnsupported, "", 0},
		{"cost below minimum", v("$2a$", "03"), categoryUnsupported, "$2a$", 0},
		{"cost above two-digit ceiling", v("$2a$", "99"), categoryUnsupported, "$2a$", 0},
		{"non-digit cost", v("$2a$", "1x"), categoryUnsupported, "$2a$", 0},
		{"no cost digits", "$2a$", categoryUnsupported, "", 0},
		{"empty", "", categoryUnsupported, "", 0},
		{"not bcrypt", "not-a-bcrypt-hash", categoryUnsupported, "", 0},
		{"short with valid prefix/cost (runtime minHashSize)", "$2a$12$short", categoryUnsupported, "", 0},
		{"unpadded cost (runtime Atoi rejects)", "$2a$4$" + strings.Repeat("x", 54), categoryUnsupported, "$2a$", 0},
		{"leading-zero cost (runtime parses as 1, below minimum)", "$2a$012$" + strings.Repeat("x", 53), categoryUnsupported, "$2a$", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			category, prefix, cost := classifyVerifier(tt.verifier)
			if category != tt.category {
				t.Errorf("category: got %d, want %d", category, tt.category)
			}
			if prefix != tt.prefix {
				t.Errorf("prefix: got %q, want %q", prefix, tt.prefix)
			}
			if cost != tt.cost {
				t.Errorf("cost: got %d, want %d", cost, tt.cost)
			}
		})
	}
}

func TestTransformUser_HappyPath(t *testing.T) {
	t.Parallel()
	rec := LegacyUser{
		ID:        MongoObjectID{OID: "65a0b1c2d3e4f5a6b7c8d9e1"},
		Username:  "MixedCase",
		Email:     "MixedCase@Example.com",
		Password:  v("$2b$", "12"),
		Role:      "user",
		Status:    "active",
		Avatar:    "https://minio.example.com/media/avatar.jpg",
		CreatedAt: MongoDate{raw: jsonRaw(t, `"2024-01-10T10:00:00.000Z"`)},
		UpdatedAt: MongoDate{raw: jsonRaw(t, `"2024-02-01T10:00:00.000Z"`)},
	}

	res := TransformUser(rec)
	if res.RejectReason != "" {
		t.Fatalf("unexpected rejection: %q", res.RejectReason)
	}
	u := res.User
	if u.ID != "65a0b1c2d3e4f5a6b7c8d9e1" {
		t.Errorf("ID must be the preserved ObjectId, got %q", u.ID)
	}
	if u.Username != "MixedCase" {
		t.Errorf("username must be preserved as provided, got %q", u.Username)
	}
	if u.UsernameCanon != "mixedcase" {
		t.Errorf("username_canon: got %q, want %q", u.UsernameCanon, "mixedcase")
	}
	if u.Email != "mixedcase@example.com" {
		t.Errorf("email must be lowercased, got %q", u.Email)
	}
	if u.Password != rec.Password {
		t.Error("verifier must be preserved byte-for-byte")
	}
	if u.AuthVersion != 1 {
		t.Errorf("auth_version: got %d, want 1", u.AuthVersion)
	}
	if res.VerifierCategory != categorySupported || res.VerifierPrefix != "$2b$" || res.VerifierCost != 12 {
		t.Errorf("verifier classification: %d %q %d", res.VerifierCategory, res.VerifierPrefix, res.VerifierCost)
	}
	if !res.AvatarDeferred {
		t.Error("avatar must be counted as deferred")
	}
	if u.CreatedAtMS != 1704880800000 {
		t.Errorf("created_at_ms: got %d, want 1704880800000", u.CreatedAtMS)
	}
	if u.UpdatedAtMS != 1706781600000 {
		t.Errorf("updated_at_ms: got %d, want 1706781600000", u.UpdatedAtMS)
	}
}

func TestTransformUser_SkewEqualization(t *testing.T) {
	t.Parallel()
	rec := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e2",
		`"2024-01-10T10:00:00.000Z"`, `"2024-01-10T09:59:00.000Z"`) // one minute before

	res := TransformUser(rec)
	if res.RejectReason != "" {
		t.Fatalf("unexpected rejection: %q", res.RejectReason)
	}
	if res.User.UpdatedAtMS != res.User.CreatedAtMS {
		t.Errorf("small inversion must equalize: updated %d, created %d", res.User.UpdatedAtMS, res.User.CreatedAtMS)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Field != "updatedAt" {
		t.Errorf("got %+v, want one updatedAt warning", res.Warnings)
	}
}

func TestTransformUser_LargeInversionRejects(t *testing.T) {
	t.Parallel()
	rec := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e3",
		`"2024-01-10T10:00:00.000Z"`, `"2024-01-01T10:00:00.000Z"`) // nine days before

	res := TransformUser(rec)
	if res.RejectReason == "" {
		t.Fatal("expected rejection for a large created/updated inversion")
	}
}

func TestTransformUser_MissingUpdatedAtFallsBack(t *testing.T) {
	t.Parallel()
	rec := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e4",
		`"2024-01-10T10:00:00.000Z"`, "")

	res := TransformUser(rec)
	if res.RejectReason != "" {
		t.Fatalf("unexpected rejection: %q", res.RejectReason)
	}
	if res.User.UpdatedAtMS != res.User.CreatedAtMS {
		t.Errorf("missing updatedAt must fall back to createdAt")
	}
	if len(res.Warnings) != 1 {
		t.Errorf("got %d, want one fallback warning", len(res.Warnings))
	}
}

// TestTransformUser_MissingCreatedAtFallsBack pins the createdAt
// fallback: every unusable-createdAt shape (missing, unparseable,
// non-positive) reuses the record's updatedAt with a per-record warning —
// the mirror of the missing-updatedAt fallback.
func TestTransformUser_MissingCreatedAtFallsBack(t *testing.T) {
	t.Parallel()

	base := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e9",
		`"2024-01-10T10:00:00.000Z"`, `"2024-02-01T10:00:00.000Z"`)

	tests := []struct {
		name   string
		mutate func(*LegacyUser)
	}{
		{"missing", func(u *LegacyUser) { u.CreatedAt = MongoDate{} }},
		{"unparseable", func(u *LegacyUser) { u.CreatedAt = MongoDate{raw: jsonRaw(t, `"not-a-date"`)} }},
		{"non-positive", func(u *LegacyUser) { u.CreatedAt = MongoDate{raw: jsonRaw(t, `"1970-01-01T00:00:00.000Z"`)} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := base
			tt.mutate(&rec)

			res := TransformUser(rec)
			if res.RejectReason != "" {
				t.Fatalf("unexpected rejection: %q", res.RejectReason)
			}
			if res.User.CreatedAtMS != res.User.UpdatedAtMS {
				t.Errorf("missing createdAt must fall back to updatedAt: created %d, updated %d",
					res.User.CreatedAtMS, res.User.UpdatedAtMS)
			}
			if len(res.Warnings) != 1 || res.Warnings[0].Field != "createdAt" {
				t.Errorf("got %+v, want one createdAt fallback warning", res.Warnings)
			}
		})
	}
}

func TestTransformUser_InventoryFlags(t *testing.T) {
	t.Parallel()
	rec := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e6",
		`"2024-01-10T10:00:00.000Z"`, `"2024-01-10T10:00:00.000Z"`)
	rec.Username = "Δοκιμή" // Greek — outside [a-zA-Z0-9]
	rec.Email = "δοκιμή@example.com"
	rec.RefreshTokenHash = jsonRaw(t, `"some-hash-value"`)

	res := TransformUser(rec)
	if res.RejectReason != "" {
		t.Fatalf("non-conforming charset must import as-is, got rejection: %q", res.RejectReason)
	}
	if !res.UsernameNonConforming {
		t.Error("Greek username must be flagged non-conforming")
	}
	if !res.EmailNonConforming {
		t.Error("Greek email must be flagged non-conforming")
	}
	if !res.HasRefreshTokenMaterial {
		t.Error("refresh-token material must be flagged for the aggregate count")
	}
}

func TestTransformUser_SpacedUsernameConforming(t *testing.T) {
	t.Parallel()
	rec := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e8",
		`"2024-01-10T10:00:00.000Z"`, `"2024-01-10T10:00:00.000Z"`)
	rec.Username = "  John   Doe "

	res := TransformUser(rec)
	if res.RejectReason != "" {
		t.Fatalf("unexpected rejection: %q", res.RejectReason)
	}
	if res.UsernameNonConforming {
		t.Error("a name that normalizes to letters/digits with single spaces must be conforming")
	}
}

func TestTransformUser_UsernameLengthBound(t *testing.T) {
	t.Parallel()

	// 32 bytes is the registration ceiling; 33 must flag for manual resolution
	// exactly like any other unregisterable shape.
	atLimit := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e9",
		`"2024-01-10T10:00:00.000Z"`, `"2024-01-10T10:00:00.000Z"`)
	atLimit.Username = strings.Repeat("a", 32)
	if res := TransformUser(atLimit); res.UsernameNonConforming {
		t.Error("a 32-byte name must be conforming")
	}

	overLimit := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9ea",
		`"2024-01-10T10:00:00.000Z"`, `"2024-01-10T10:00:00.000Z"`)
	overLimit.Username = strings.Repeat("a", 33)
	if res := TransformUser(overLimit); !res.UsernameNonConforming {
		t.Error("a 33-byte name is outside the registration rule and must be flagged")
	}
}

func TestTransformUser_RejectionReasonsCarryObservedValues(t *testing.T) {
	t.Parallel()
	rec := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e7",
		`"2024-01-10T10:00:00.000Z"`, `"2024-01-10T10:00:00.000Z"`)
	rec.Role = "root"
	if res := TransformUser(rec); res.RejectReason != `unknown role "root"` {
		t.Errorf("reason must carry the observed value, got %q", res.RejectReason)
	}
	rec.Role = "user"
	rec.Status = "banned"
	if res := TransformUser(rec); res.RejectReason != `unknown status "banned"` {
		t.Errorf("reason must carry the observed value, got %q", res.RejectReason)
	}
}

func TestTransformUser_NonPositiveUpdatedAtRejects(t *testing.T) {
	t.Parallel()
	rec := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e8",
		`"2024-01-10T10:00:00.000Z"`, `"1970-01-01T00:00:00.000Z"`) // zero millis

	res := TransformUser(rec)
	if res.RejectReason != "invalid updatedAt" {
		t.Errorf("non-positive updatedAt must reject as invalid, got %q", res.RejectReason)
	}
}

func TestTransformUser_Rejections(t *testing.T) {
	t.Parallel()
	base := func() LegacyUser {
		u := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d9e5",
			`"2024-01-10T10:00:00.000Z"`, `"2024-01-10T10:00:00.000Z"`)
		u.ID = MongoObjectID{OID: "65a0b1c2d3e4f5a6b7c8d9e5"}
		return u
	}

	tests := []struct {
		name   string
		mutate func(*LegacyUser)
	}{
		{"missing id", func(u *LegacyUser) { u.ID.OID = "" }},
		{"empty username", func(u *LegacyUser) { u.Username = "" }},
		{"empty email", func(u *LegacyUser) { u.Email = "" }},
		{"unknown role", func(u *LegacyUser) { u.Role = "root" }},
		{"unknown status", func(u *LegacyUser) { u.Status = "banned" }},
		{"missing both timestamps", func(u *LegacyUser) {
			u.CreatedAt = MongoDate{}
			u.UpdatedAt = MongoDate{}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := base()
			tt.mutate(&rec)
			res := TransformUser(rec)
			if res.RejectReason == "" {
				t.Error("expected rejection")
			}
		})
	}
}

func TestImportUsers(t *testing.T) {
	t.Parallel()
	db := openImportDB(t)
	ctx := t.Context()

	verifier := v("$2b$", "12")
	recs := []LegacyUser{
		validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d900", `"2024-01-10T10:00:00.000Z"`, `"2024-02-01T10:00:00.000Z"`),
		validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d901", `"2024-01-11T10:00:00.000Z"`, `"2024-01-11T10:00:00.000Z"`),
	}
	recs[0].Username, recs[0].Email = "FirstUser", "first@example.com"
	recs[1].Username, recs[1].Email = "Δοκιμή", "δοκιμή@example.com" // Greek — imported as-is, counted
	recs[0].Password = verifier
	recs[1].Password = verifier
	recs[0].RefreshTokenHash = jsonRaw(t, `"legacy-hash-value"`)
	recs[0].RefreshTokenJti = jsonRaw(t, `"legacy-jti-value"`)
	// Duplicate canonical username (different case) — must reject.
	dup := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d902", `"2024-01-12T10:00:00.000Z"`, `"2024-01-12T10:00:00.000Z"`)
	dup.Username, dup.Email = "firstuser", "dup@example.com" // same canon as recs[0]
	recs = append(recs, dup)
	// Above-max verifier — imported unchanged, classified in the report.
	aboveMax := validUserWithTimestamps(t, "65a0b1c2d3e4f5a6b7c8d903", `"2024-01-13T10:00:00.000Z"`, `"2024-01-13T10:00:00.000Z"`)
	aboveMax.Username, aboveMax.Email = "ThirdUser", "third@example.com"
	aboveMax.Password = v("$2b$", "14")
	recs = append(recs, aboveMax)

	report, err := ImportUsers(ctx, db, recs)
	if err != nil {
		t.Fatalf("ImportUsers: %v", err)
	}

	if report.Source.Users != 4 {
		t.Errorf("source users: got %d, want 4", report.Source.Users)
	}
	if report.Source.Imported != 3 {
		t.Errorf("imported: got %d, want 3", report.Source.Imported)
	}
	if report.Source.Rejected != 1 {
		t.Errorf("rejected: got %d, want 1", report.Source.Rejected)
	}
	if report.Credentials.Supported != 3 || report.Credentials.AboveMaxCost != 1 || report.Credentials.Unsupported != 0 {
		t.Errorf("credentials: supported=%d aboveMax=%d unsupported=%d",
			report.Credentials.Supported, report.Credentials.AboveMaxCost, report.Credentials.Unsupported)
	}
	if report.Credentials.ByPrefix["$2b$"] != 4 {
		t.Errorf("byPrefix[$2b$]: got %d, want 4", report.Credentials.ByPrefix["$2b$"])
	}
	if report.AvatarsDeferred != 4 {
		t.Errorf("avatarsDeferred: got %d, want 4", report.AvatarsDeferred)
	}
	if report.SpecialCharacters.Usernames != 1 || report.SpecialCharacters.Emails != 1 {
		t.Errorf("specialCharacters: usernames=%d emails=%d, want 1/1",
			report.SpecialCharacters.Usernames, report.SpecialCharacters.Emails)
	}
	if report.ExcludedRefreshTokenFields != 1 {
		t.Errorf("excludedRefreshTokenFields: got %d, want 1 (the record with refresh material)", report.ExcludedRefreshTokenFields)
	}
	if report.TargetSessions != 0 {
		t.Errorf("targetSessions: got %d, want 0 — the import must create no session rows", report.TargetSessions)
	}
	if len(report.RejectedRecords) != 1 || !strings.Contains(report.RejectedRecords[0].Reason, "duplicate canonical username") {
		t.Errorf("rejected records: %+v", report.RejectedRecords)
	}

	// The verifier was preserved byte-for-byte for the above-max record too
	// (preserve in every category) — compare equality WITHOUT printing the
	// value.
	var stored string
	if err := db.QueryRowContext(ctx, `SELECT password FROM users WHERE id = ?`, "65a0b1c2d3e4f5a6b7c8d903").Scan(&stored); err != nil {
		t.Fatalf("read stored verifier: %v", err)
	}
	if stored != aboveMax.Password {
		t.Error("above-max verifier must be preserved byte-for-byte")
	}

	// User IDs are preserved ObjectIds — the mapping entries are identity
	// pairs.
	for _, m := range report.Identifiers.SourceToTarget {
		if m.SourceID != m.TargetID {
			t.Errorf("user mapping must be identity: %q != %q", m.SourceID, m.TargetID)
		}
	}
}

func TestReportUserSectionsMarshalStably(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(newReport())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	src, ok := body["source"].(map[string]any)
	if !ok {
		t.Fatal("source section missing")
	}
	if _, has := src["users"]; !has {
		t.Error("source.users missing from the report shape")
	}
	creds, ok := body["credentials"].(map[string]any)
	if !ok {
		t.Fatal("credentials section missing")
	}
	byPrefix, ok := creds["byPrefix"].(map[string]any)
	if !ok {
		t.Error("credentials.byPrefix must marshal as an object, not null")
	}
	if len(byPrefix) != 0 {
		t.Errorf("empty byPrefix must marshal empty, got %v", byPrefix)
	}
	if _, has := body["avatarsDeferred"]; !has {
		t.Error("avatarsDeferred missing from the report shape")
	}
	if _, has := body["specialCharacters"]; !has {
		t.Error("specialCharacters missing from the report shape")
	}
	if _, has := body["excludedRefreshTokenFields"]; !has {
		t.Error("excludedRefreshTokenFields missing from the report shape")
	}
	if _, has := body["targetSessions"]; !has {
		t.Error("targetSessions missing from the report shape")
	}
}

// TestVerifierConstantsMirrorAuth pins this side of the mirror against the
// literal cost boundaries the auth runtime gate uses. Each side pins its own
// pair (auth pins the same literals in internal/auth/auth_service_test.go) —
// change both sides together.
func TestVerifierConstantsMirrorAuth(t *testing.T) {
	t.Parallel()
	if verifierOperationalMaxCost != 13 {
		t.Errorf("verifierOperationalMaxCost = %d, want 13 (auth maxBcryptCost)", verifierOperationalMaxCost)
	}
	if verifierTargetCost != 12 {
		t.Errorf("verifierTargetCost = %d, want 12 (auth bcryptCost)", verifierTargetCost)
	}
}

// TestConformingUsername_MatchesRegistrationRule pins this side of the
// fold/shape mirror (auth's Register rule is the other side).
func TestConformingUsername_MatchesRegistrationRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		username string
		want     bool
	}{
		{"simple", "Katakuri", true},
		{"single inner space", "John Doe", true},
		{"whitespace folds before the check", "  John   Doe  ", true},
		{"digits and letters", "user123", true},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"32 bytes", strings.Repeat("a", 32), true},
		{"33 bytes", strings.Repeat("a", 33), false},
		{"underscore", "test_user", false},
		{"hyphen", "a-b", false},
		{"Greek", "Τεστ", false},
		{"at sign", "m@ria", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := conformingUsername(tt.username); got != tt.want {
				t.Errorf("conformingUsername(%q) = %t, want %t", tt.username, got, tt.want)
			}
		})
	}
}
