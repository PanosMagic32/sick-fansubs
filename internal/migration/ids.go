package migration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// targetIDDomainKey domain-separates the deterministic content-ID
// derivation from any other hash use. It is a fixed public constant, NOT a
// secret: target content IDs are opaque public identifiers, never credential
// material.
const targetIDDomainKey = "sick-fansubs:legacy-content-id:v1"

// deriveTargetID maps a legacy content ObjectId to the target 32-hex content
// ID: hex(HMAC-SHA256(targetIDDomainKey, legacyObjectID)) truncated to 128
// bits.
//
// Determinism is the point: the same source record derives the same target
// ID on every run, so the favorites import resolves legacy content
// references without saved mapping state and a fresh-target re-import
// derives identical content IDs ("repeatable on a fresh target"). The child
// tables' fresh ids (blog download rows, project entries without a legacy
// sub-ObjectId) are not part of that claim.
func deriveTargetID(legacyObjectID string) string {
	mac := hmac.New(sha256.New, []byte(targetIDDomainKey))
	mac.Write([]byte(legacyObjectID))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}
