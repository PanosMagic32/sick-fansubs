package database

// Schema verification: the Go mirror of the embedded migrations.
//
// docs/patterns/go/sqlite.md requires the migration operation to verify the
// resulting schema with PRAGMA introspection: `table_list` (STRICT status),
// `table_xinfo` (columns, declared types, nullability, primary keys), and
// `index_list`/`index_xinfo` (unique keys and declared indexes), rejecting
// unexpected columns, tables, and indexes. SQLite does not expose CHECK
// clauses as structured metadata, so the runner does not attempt to prove
// textual DDL equivalence — the fixed migrations supply those checks.
//
// The expected schema is declared here in Go, mirroring the embedded
// migration files. A drift between the two is a FAILURE by design: any
// hand-applied or foreign modification that the migration files do not
// produce makes verification (and therefore readiness) fail closed.

// columnSpec declares one expected table column.
type columnSpec struct {
	typ     string // declared type as PRAGMA table_xinfo reports it
	notNull bool   // NOT NULL constraint (implicit for PK columns — skipped)
	pk      bool   // part of the PRIMARY KEY
}

// indexSpec declares one expected index.
//
// Declared CREATE INDEX entries match by name. PRIMARY KEY and UNIQUE
// constraint auto-indexes (sqlite_autoindex_*) are matched by origin +
// unique flag + column set because their generated names are not part of
// the contract.
//
// A column entry may carry a " DESC" suffix for descending index columns
// (e.g. "created_at_ms DESC") — the verifier compares direction too, so a
// hand-flipped index direction is a fail-closed drift.
//
// partial marks a declared PARTIAL index (a WHERE clause, migration 0010's
// idx_user_notifications_unread). A partial index only
// satisfies an expectation that also declares partial, so an undeclared
// partial index fails verification.
type indexSpec struct {
	name    string // declared index name; empty for auto-indexes
	origin  string // "c" = CREATE INDEX, "u" = UNIQUE constraint, "pk" = PRIMARY KEY
	unique  bool
	partial bool
	columns []string
}

// tableSpec declares one expected table.
type tableSpec struct {
	// rowidAlias marks tables whose PRIMARY KEY is an INTEGER PRIMARY KEY
	// (a rowid alias — [SQLite rowid tables]). SQLite creates no separate
	// primary-key index for those — index_list has no entry to match, so
	// pk-origin expectations are satisfied implicitly by the table_xinfo pk
	// flag.
	// [SQLite rowid tables]: https://www.sqlite.org/lang_createtable.html#rowid
	rowidAlias bool
	// fts marks an FTS5 virtual table: it is not STRICT and
	// exposes hidden bookkeeping columns, so structural verification stops
	// at "virtual and present" — SQLite owns the shadow-table shapes.
	fts     bool
	columns map[string]columnSpec
	indexes []indexSpec
}

// schemaSpec mirrors the embedded migrations (0001–0021). Adding a table,
// column, or index to a migration file WITHOUT updating this spec makes every
// migrated database fail verification — exactly the fail-closed behavior
// docs/patterns/go/sqlite.md requires.
var schemaSpec = map[string]tableSpec{
	"schema_migrations": {
		// version is INTEGER PRIMARY KEY — a rowid alias with no separate
		// primary-key index entry in index_list.
		rowidAlias: true,
		columns: map[string]columnSpec{
			"version":       {"INTEGER", true, true},
			"name":          {"TEXT", true, false},
			"checksum":      {"BLOB", true, false},
			"applied_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "u", unique: true, columns: []string{"name"}},
		},
	},
	"users": {
		columns: map[string]columnSpec{
			"id":                   {"TEXT", true, true},
			"username":             {"TEXT", true, false},
			"username_canon":       {"TEXT", true, false},
			"email":                {"TEXT", true, false},
			"password":             {"TEXT", true, false},
			"role":                 {"TEXT", true, false},
			"status":               {"TEXT", true, false},
			"auth_version":         {"INTEGER", true, false},
			"must_change_password": {"INTEGER", true, false},
			"avatar_url":           {"TEXT", false, false},
			"email_verified_at_ms": {"INTEGER", false, false},
			"created_at_ms":        {"INTEGER", true, false},
			"updated_at_ms":        {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{origin: "u", unique: true, columns: []string{"username_canon"}},
			{origin: "u", unique: true, columns: []string{"email"}},
		},
	},
	"sessions": {
		columns: map[string]columnSpec{
			"id":            {"TEXT", true, true},
			"user_id":       {"TEXT", true, false},
			"token_digest":  {"BLOB", true, false},
			"csrf_token":    {"BLOB", true, false},
			"auth_version":  {"INTEGER", true, false},
			"created_at_ms": {"INTEGER", true, false},
			"expires_at_ms": {"INTEGER", true, false},
			// Nullable client label (migration 0018) — a
			// closed-vocabulary device label, never raw client text.
			"client_label": {"TEXT", false, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{origin: "u", unique: true, columns: []string{"token_digest"}},
			{name: "idx_sessions_expires_at_ms", origin: "c", columns: []string{"expires_at_ms"}},
			{name: "idx_sessions_user_id", origin: "c", columns: []string{"user_id"}},
		},
	},
	// Self-service reset tokens (migration 0011): SHA-256
	// digest PK, one active token per user via the UNIQUE user_id.
	"password_reset_tokens": {
		columns: map[string]columnSpec{
			"token_digest":  {"BLOB", true, true},
			"user_id":       {"TEXT", true, false},
			"created_at_ms": {"INTEGER", true, false},
			"expires_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"token_digest"}},
			{origin: "u", unique: true, columns: []string{"user_id"}},
		},
	},
	// Email-verification tokens (migration 0012): same
	// shape as password_reset_tokens.
	"email_verification_tokens": {
		columns: map[string]columnSpec{
			"token_digest":  {"BLOB", true, true},
			"user_id":       {"TEXT", true, false},
			"created_at_ms": {"INTEGER", true, false},
			"expires_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"token_digest"}},
			{origin: "u", unique: true, columns: []string{"user_id"}},
		},
	},
	"blog_posts": {
		columns: map[string]columnSpec{
			"id":              {"TEXT", true, true},
			"title":           {"TEXT", true, false},
			"subtitle":        {"TEXT", true, false},
			"description":     {"TEXT", true, false},
			"thumbnail_url":   {"TEXT", true, false},
			"status":          {"TEXT", true, false},
			"creator_id":      {"TEXT", false, false},
			"updater_id":      {"TEXT", false, false},
			"published_at_ms": {"INTEGER", false, false},
			"created_at_ms":   {"INTEGER", true, false},
			"updated_at_ms":   {"INTEGER", true, false},
			"revision":        {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{name: "idx_blog_posts_published", origin: "c", columns: []string{"published_at_ms", "id"}},
			{name: "idx_blog_posts_creator", origin: "c", columns: []string{"creator_id"}},
		},
	},
	"blog_post_downloads": {
		columns: map[string]columnSpec{
			"id":            {"TEXT", true, true},
			"blog_post_id":  {"TEXT", true, false},
			"resolution":    {"TEXT", true, false},
			"magnet_link":   {"TEXT", false, false},
			"torrent_link":  {"TEXT", false, false},
			"position":      {"INTEGER", true, false},
			"created_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{name: "idx_blog_post_downloads_post", origin: "c", columns: []string{"blog_post_id", "position"}},
		},
	},
	"projects": {
		columns: map[string]columnSpec{
			"id":              {"TEXT", true, true},
			"title":           {"TEXT", true, false},
			"description":     {"TEXT", true, false},
			"slug":            {"TEXT", true, false},
			"thumbnail_url":   {"TEXT", true, false},
			"status":          {"TEXT", true, false},
			"creator_id":      {"TEXT", false, false},
			"updater_id":      {"TEXT", false, false},
			"published_at_ms": {"INTEGER", false, false},
			"created_at_ms":   {"INTEGER", true, false},
			"updated_at_ms":   {"INTEGER", true, false},
			"revision":        {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			// The UNIQUE constraint on slug owns the auto-index that serves
			// slug lookups; no named duplicate exists.
			{origin: "u", unique: true, columns: []string{"slug"}},
			{name: "idx_projects_published", origin: "c", columns: []string{"published_at_ms", "id"}},
		},
	},
	"project_downloads": {
		columns: map[string]columnSpec{
			"id":            {"TEXT", true, true},
			"project_id":    {"TEXT", true, false},
			"name":          {"TEXT", true, false},
			"magnet_link":   {"TEXT", false, false},
			"torrent_link":  {"TEXT", false, false},
			"position":      {"INTEGER", true, false},
			"created_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{name: "idx_project_downloads_project", origin: "c", columns: []string{"project_id", "position"}},
		},
	},
	// Favorites join tables (migration 0006): composite
	// primary keys (user_id, content_id) — the PK auto-index covers the
	// exact column set in declared order.
	"blog_post_favorites": {
		columns: map[string]columnSpec{
			"user_id":       {"TEXT", true, true},
			"blog_post_id":  {"TEXT", true, true},
			"created_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"user_id", "blog_post_id"}},
			{name: "idx_blog_post_favorites_post", origin: "c", columns: []string{"blog_post_id"}},
		},
	},
	"project_favorites": {
		columns: map[string]columnSpec{
			"user_id":       {"TEXT", true, true},
			"project_id":    {"TEXT", true, true},
			"created_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"user_id", "project_id"}},
			{name: "idx_project_favorites_project", origin: "c", columns: []string{"project_id"}},
		},
	},
	// Durable audit events (migration 0007): no foreign
	// keys — events survive the deletion of the accounts they reference.
	"audit_events": {
		columns: map[string]columnSpec{
			"id":            {"TEXT", true, true},
			"event":         {"TEXT", true, false},
			"result":        {"TEXT", true, false},
			"actor_id":      {"TEXT", false, false},
			"target_id":     {"TEXT", false, false},
			"target_role":   {"TEXT", false, false},
			"request_id":    {"TEXT", true, false},
			"remote_addr":   {"TEXT", true, false},
			"created_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{name: "idx_audit_events_created", origin: "c", columns: []string{"created_at_ms"}},
		},
	},
	// Per-event notification read state (migration 0009):
	// the two cascade FKs pair read-row cleanup with account deletion and
	// the 365-day audit event cleanup.
	"notification_reads": {
		columns: map[string]columnSpec{
			"user_id":         {"TEXT", true, true},
			"event_id":        {"TEXT", true, true},
			"read_at_ms":      {"INTEGER", true, false},
			"dismissed_at_ms": {"INTEGER", false, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"user_id", "event_id"}},
			{name: "idx_notification_reads_event", origin: "c", columns: []string{"event_id"}},
		},
	},
	// Comments thread (migration 0010): the mirror pair
	// against blog_posts / projects with the materialized hearts_count, the
	// one-level parent_id self-FK, and the heart join tables. The _post_top
	// indexes pin the DESC directions (the schemaSpec direction convention).
	"blog_post_comments": {
		columns: map[string]columnSpec{
			"id":            {"TEXT", true, true},
			"user_id":       {"TEXT", true, false},
			"blog_post_id":  {"TEXT", true, false},
			"parent_id":     {"TEXT", false, false},
			"body":          {"TEXT", true, false},
			"hearts_count":  {"INTEGER", true, false},
			"created_at_ms": {"INTEGER", true, false},
			"updated_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{name: "idx_blog_post_comments_post", origin: "c", columns: []string{"blog_post_id", "created_at_ms", "id"}},
			{name: "idx_blog_post_comments_post_top", origin: "c", columns: []string{"blog_post_id", "hearts_count DESC", "created_at_ms DESC", "id"}},
			{name: "idx_blog_post_comments_parent", origin: "c", columns: []string{"parent_id"}},
		},
	},
	"blog_post_comment_hearts": {
		columns: map[string]columnSpec{
			"user_id":       {"TEXT", true, true},
			"comment_id":    {"TEXT", true, true},
			"created_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"user_id", "comment_id"}},
			{name: "idx_blog_post_comment_hearts_comment", origin: "c", columns: []string{"comment_id"}},
		},
	},
	"project_comments": {
		columns: map[string]columnSpec{
			"id":            {"TEXT", true, true},
			"user_id":       {"TEXT", true, false},
			"project_id":    {"TEXT", true, false},
			"parent_id":     {"TEXT", false, false},
			"body":          {"TEXT", true, false},
			"hearts_count":  {"INTEGER", true, false},
			"created_at_ms": {"INTEGER", true, false},
			"updated_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{name: "idx_project_comments_project", origin: "c", columns: []string{"project_id", "created_at_ms", "id"}},
			{name: "idx_project_comments_project_top", origin: "c", columns: []string{"project_id", "hearts_count DESC", "created_at_ms DESC", "id"}},
			{name: "idx_project_comments_parent", origin: "c", columns: []string{"parent_id"}},
		},
	},
	"project_comment_hearts": {
		columns: map[string]columnSpec{
			"user_id":       {"TEXT", true, true},
			"comment_id":    {"TEXT", true, true},
			"created_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"user_id", "comment_id"}},
			{name: "idx_project_comment_hearts_comment", origin: "c", columns: []string{"comment_id"}},
		},
	},
	// Per-recipient product notifications (migration 0010):
	// the unified feed's user_notifications source. The unread
	// index is the one DECLARED partial index — see the partial flag on
	// indexSpec. Migration 0015 made comment_id NULLABLE — content_updated
	// rows carry no comment; the per-kind rule lives in the table CHECK.
	// Migration 0019 added draft_action, bound to the draft_activity kind
	// by the same per-kind
	// CHECK shape.
	"user_notifications": {
		columns: map[string]columnSpec{
			"id":            {"TEXT", true, true},
			"recipient_id":  {"TEXT", true, false},
			"kind":          {"TEXT", true, false},
			"actor_id":      {"TEXT", false, false},
			"content_kind":  {"TEXT", true, false},
			"content_id":    {"TEXT", true, false},
			"comment_id":    {"TEXT", false, false},
			"draft_action":  {"TEXT", false, false},
			"created_at_ms": {"INTEGER", true, false},
			"read_at_ms":    {"INTEGER", false, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{name: "idx_user_notifications_recipient_created", origin: "c", columns: []string{"recipient_id", "created_at_ms DESC", "id"}},
			{name: "idx_user_notifications_unread", origin: "c", partial: true, columns: []string{"recipient_id", "created_at_ms DESC"}},
		},
	},
	// Web push channel (migration 0014):
	// per-device subscription rows (endpoint UNIQUE — resubscribe = new
	// endpoint, one row per device per user) and per-user per-kind preference
	// toggles (absence = the role default, a row = an explicit override).
	"push_subscriptions": {
		columns: map[string]columnSpec{
			"id":            {"TEXT", true, true},
			"user_id":       {"TEXT", true, false},
			"endpoint":      {"TEXT", true, false},
			"p256dh":        {"TEXT", true, false},
			"auth":          {"TEXT", true, false},
			"created_at_ms": {"INTEGER", true, false},
			"updated_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"id"}},
			{origin: "u", unique: true, columns: []string{"endpoint"}},
			{name: "idx_push_subscriptions_user", origin: "c", columns: []string{"user_id"}},
		},
	},
	"notification_preferences": {
		columns: map[string]columnSpec{
			"user_id":       {"TEXT", true, true},
			"kind":          {"TEXT", true, true},
			"push_enabled":  {"INTEGER", true, false},
			"updated_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"user_id", "kind"}},
		},
	},
	// Per-content follows (migration 0015):
	// one table with a polymorphic (content_kind, content_id) ref — drives
	// the 'comment' and 'content_updated' kinds. No content FK (the
	// user_notifications precedent: rows on hard-deleted content dangle
	// inertly).
	"content_follows": {
		columns: map[string]columnSpec{
			"user_id":       {"TEXT", true, true},
			"content_kind":  {"TEXT", true, true},
			"content_id":    {"TEXT", true, true},
			"created_at_ms": {"INTEGER", true, false},
		},
		indexes: []indexSpec{
			{origin: "pk", unique: true, columns: []string{"user_id", "content_kind", "content_id"}},
			{name: "idx_content_follows_content", origin: "c", columns: []string{"content_kind", "content_id"}},
		},
	},
	// FTS5 search indexes (migration 0005): virtual tables —
	// see the fts flag on tableSpec.
	"blog_post_search": {fts: true},
	"project_search":   {fts: true},
}
