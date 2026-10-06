package migration

import (
	"encoding/json"
	"strings"
)

// LegacyUser mirrors the legacy MongoDB export shape for user records. It
// exists ONLY as the migration source contract — never a target runtime
// type. Field names follow the Mongoose
// defaults; the rehearsal inventory confirms them against the controlled
// source.
//
// The legacy refresh-token fields (refreshTokenHash, refreshTokenJti,
// refreshTokenExpiresAt) are intentionally absent from this struct: they
// are excluded from the target and the JSON decoder
// drops unknown fields. createdBlogPostIds is likewise excluded — creator
// identity lives on content rows. The FAVORITES arrays (favoriteBlogPostIds/
// favoriteProjectIds) ARE decoded: the users import ignores them, and the
// favorites import reads them from this same shape.
type LegacyUser struct {
	ID        MongoObjectID `json:"_id"`
	Username  string        `json:"username"`
	Email     string        `json:"email"`
	Password  string        `json:"password"`
	Role      string        `json:"role"`
	Status    string        `json:"status"`
	Avatar    string        `json:"avatar"`
	CreatedAt MongoDate     `json:"createdAt"`
	UpdatedAt MongoDate     `json:"updatedAt"`

	// Favorite arrays: ObjectId references to legacy content
	// records, insertion-ordered (oldest first — legacy appended on
	// favoriting). Decoded for the favorites import only; the users import
	// writes no join rows.
	FavoriteBlogPostIds []MongoObjectID `json:"favoriteBlogPostIds"`
	FavoriteProjectIds  []MongoObjectID `json:"favoriteProjectIds"`

	// Refresh-token fields are excluded from the target.
	// They are decoded ONLY to detect presence for the aggregate exclusion
	// count — the values are never retained, printed, or copied
	// anywhere (json.RawMessage is discarded after the presence check).
	RefreshTokenHash      json.RawMessage `json:"refreshTokenHash"`
	RefreshTokenJti       json.RawMessage `json:"refreshTokenJti"`
	RefreshTokenExpiresAt json.RawMessage `json:"refreshTokenExpiresAt"`
}

// ImportUser is one target users row, pre-INSERT.
type ImportUser struct {
	ID            string // preserved legacy ObjectId
	Username      string // stored as provided for display
	UsernameCanon string // LOWER(username) applied in Go
	Email         string // lowercased
	Password      string // verifier preserved byte-for-byte
	Role          string
	Status        string
	AuthVersion   int64
	CreatedAtMS   int64
	UpdatedAtMS   int64
}

// verifierCategory classifies a legacy bcrypt verifier.
type verifierCategory int

const (
	categoryUnsupported verifierCategory = iota
	categoryAboveMax
	categorySupported
)

// Verifier classification constants. These mirror
// internal/auth's maxBcryptCost (13), bcryptCost (12), and
// supportedVerifierPrefix — the runtime gate and this classifier must stay
// aligned; change both sides together.
const (
	verifierOperationalMaxCost = 13
	verifierTargetCost         = 12
	verifierMinCost            = 4
	verifierMaxCost            = 31
)

// maxClockSkewMS is the largest created/updated inversion the transform
// equalizes as clock skew. Larger inversions reject the record for manual
// review. The value is an implementation choice (recorded in
// internal/migration/AGENTS.md).
const maxClockSkewMS int64 = 5 * 60 * 1000

// minVerifierLength mirrors x/crypto/bcrypt's minHashSize (59): a stored
// hash shorter than this fails the runtime's parse before any comparison,
// so it must classify as unsupported rather than by its prefix/cost alone.
const minVerifierLength = 59

// classifyVerifier inspects a legacy password verifier WITHOUT copying or
// printing it and returns its category, its prefix, and its cost.
//
// $2a$/$2b$/$2y$ with cost 4..13 are supported (cost < 12 rehashes on
// sign-in); cost 14..31 is above the operational maximum; everything else —
// other minor characters (Go's parser accepts any), a malformed prefix, or
// an out-of-range/unparseable cost — is unsupported. The prefix is reported
// even for unsupported-but-shaped values so the inventory can
// count them.
func classifyVerifier(verifier string) (verifierCategory, string, int) {
	if len(verifier) < minVerifierLength {
		return categoryUnsupported, "", 0
	}
	if verifier[0] != '$' || verifier[1] != '2' || verifier[3] != '$' {
		return categoryUnsupported, "", 0
	}
	prefix := verifier[:4]
	if minor := verifier[2]; minor != 'a' && minor != 'b' && minor != 'y' {
		return categoryUnsupported, prefix, 0
	}
	// Exactly two ASCII cost digits, matching the runtime's parse
	// (x/crypto decodeCost runs strconv.Atoi over sbytes[0:2] — a partial
	// or unpadded cost like "$2a$4$" fails Atoi, and a leading zero like
	// "$2a$012$" parses as cost 1, below the minimum — both unusable).
	hi, lo := verifier[4], verifier[5]
	if hi < '0' || hi > '9' || lo < '0' || lo > '9' {
		return categoryUnsupported, prefix, 0
	}
	cost := int(hi-'0')*10 + int(lo-'0')
	if cost < verifierMinCost || cost > verifierMaxCost {
		return categoryUnsupported, prefix, 0
	}
	if cost > verifierOperationalMaxCost {
		return categoryAboveMax, prefix, cost
	}
	return categorySupported, prefix, cost
}

// TransformUserResult is the outcome of mapping one legacy user record.
type TransformUserResult struct {
	User             ImportUser
	Warnings         []MigrationWarning
	RejectReason     string
	VerifierCategory verifierCategory
	VerifierPrefix   string // "" when the verifier has no parseable $2?$ shape
	VerifierCost     int    // 0 when unparseable
	AvatarDeferred   bool

	// Inventory flags (import as-is, count for manual resolution — never
	// reject on charset).
	UsernameNonConforming bool
	EmailNonConforming    bool
	// HasRefreshTokenMaterial marks records that carried excluded
	// refresh-token fields.
	HasRefreshTokenMaterial bool
}

// TransformUser maps one legacy record to the target user row: ID, username,
// and verifier are preserved (username_canon and email lowercased), the
// verifier is classified for the report, and the avatar defers to the media
// import (NULL, counted). The timestamp fallback chain, the clock-skew
// allowance, and the target CHECK pre-validation are the contract in
// internal/migration/AGENTS.md.
func TransformUser(rec LegacyUser) TransformUserResult {
	res := TransformUserResult{}
	res.User.ID = rec.ID.OID
	res.User.Username = rec.Username
	res.User.UsernameCanon = strings.ToLower(rec.Username)
	res.User.Email = strings.ToLower(rec.Email)
	res.User.Password = rec.Password
	res.User.Role = rec.Role
	res.User.Status = rec.Status
	res.User.AuthVersion = 1

	res.VerifierCategory, res.VerifierPrefix, res.VerifierCost = classifyVerifier(rec.Password)
	res.AvatarDeferred = rec.Avatar != ""
	res.UsernameNonConforming = !conformingUsername(rec.Username)
	res.EmailNonConforming = !conformingEmailCharset(rec.Email)
	res.HasRefreshTokenMaterial = len(rec.RefreshTokenHash) > 0 ||
		len(rec.RefreshTokenJti) > 0 || len(rec.RefreshTokenExpiresAt) > 0

	if rec.ID.OID == "" {
		res.RejectReason = "missing _id"
		return res
	}
	if rec.Username == "" {
		res.RejectReason = "empty username"
		return res
	}
	if rec.Email == "" {
		res.RejectReason = "empty email"
		return res
	}
	// Role/status CHECK pre-validation: the rejection reason carries the
	// observed value so the rehearsal can inventory every production value
	// — roles/statuses are constrained source fields,
	// not personal data, and are never in the never-log list.
	switch rec.Role {
	case "super-admin", "admin", "moderator", "user":
	default:
		res.RejectReason = "unknown role \"" + rec.Role + "\""
		return res
	}
	switch rec.Status {
	case "active", "suspended":
	default:
		res.RejectReason = "unknown status \"" + rec.Status + "\""
		return res
	}

	createdMS, createdOK := rec.CreatedAt.Millis()
	createdOK = createdOK && createdMS > 0
	updatedMS, updatedOK := rec.UpdatedAt.Millis()

	if !createdOK {
		// A record without a usable createdAt reuses its only real timestamp —
		// updatedAt — instead of rejecting or inventing one.
		// The per-record warning inventories every fallback for the
		// rehearsal. A record with NEITHER timestamp usable still rejects.
		if !updatedOK || updatedMS <= 0 {
			res.RejectReason = "missing or invalid createdAt"
			return res
		}
		createdMS = updatedMS
		res.Warnings = append(res.Warnings, MigrationWarning{
			SourceID: rec.ID.OID,
			Field:    "createdAt",
			Detail:   "missing createdAt fell back to updatedAt",
		})
	} else if !updatedOK {
		updatedMS = createdMS
		res.Warnings = append(res.Warnings, MigrationWarning{
			SourceID: rec.ID.OID,
			Field:    "updatedAt",
			Detail:   "missing updatedAt fell back to createdAt",
		})
	} else if updatedMS <= 0 {
		// Parseable but non-positive — a pre-epoch instant is malformed for
		// this data, not a clock-skew inversion.
		res.RejectReason = "invalid updatedAt"
		return res
	}
	if updatedMS < createdMS {
		if createdMS-updatedMS <= maxClockSkewMS {
			updatedMS = createdMS
			res.Warnings = append(res.Warnings, MigrationWarning{
				SourceID: rec.ID.OID,
				Field:    "updatedAt",
				Detail:   "clock-skew inversion equalized to created_at_ms",
			})
		} else {
			res.RejectReason = "updatedAt before createdAt beyond the clock-skew allowance"
			return res
		}
	}

	res.User.CreatedAtMS = createdMS
	res.User.UpdatedAtMS = updatedMS
	return res
}

// conformingUsername mirrors internal/auth's Register rule: the normalized
// name (trimmed, runs collapsed) is 1–32 bytes of ASCII letters/digits with
// single inner spaces; other legacy names import as-is. Keep both in sync.
func conformingUsername(s string) bool {
	normalized := strings.Join(strings.Fields(s), " ")
	if len(normalized) < 1 || len(normalized) > 32 {
		return false
	}
	for i := 0; i < len(normalized); i++ {
		c := normalized[i]
		if c == ' ' {
			continue
		}
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// conformingEmailCharset reports whether every byte of s is in the
// ASCII email charset [A-Za-z0-9@._+-]. Non-conforming
// legacy emails import as-is and are counted for manual resolution.
func conformingEmailCharset(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '@' || c == '.' || c == '_' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}
