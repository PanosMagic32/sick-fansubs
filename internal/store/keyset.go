package store

// PageKey is the keyset continuation tuple shared by every list endpoint:
// (published_at_ms, id). The accepted ordering is
// published_at_ms DESC, id ASC, so "strictly after" means an earlier
// publish time, or the same publish time with a larger id (the opaque
// public ID is the final tie-breaker).
//
// One shape serves every collection: the endpoints share the ordering and the
// tuple rather than keeping per-endpoint copies that could diverge. Search's
// tuple is SearchKeyset — the title sort needs a third, text-keyed slot.
type PageKey struct {
	PublishedAtMS int64
	ID            string
}
