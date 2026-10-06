package handler

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The notification-kind vocabulary lives in SEVERAL places by design: both
// store CHECK constraints (what may be stored; `user_notifications` keeps
// its own historical order), the Go kind constants and
// `pushKinds` (the preference order this package reads and writes), the wire
// contract's `{kind}` path enum, the `NotificationPreference` schema enum,
// the feed's discriminator mapping, the web client's union, and the service
// worker's `sentenceFor` branches (its `default` silently drops an unknown
// kind). A drift means a kind the server accepts becomes invisible to the
// UI, or the UI offers one the server rejects — so every copy is pinned
// here, the cheap place to learn about it (the audit event-vocabulary pin
// precedent).

// preferenceKindCheckPattern extracts the notification_preferences kind
// CHECK's value list from the newest migration that rebuilds the table.
var preferenceKindCheckPattern = regexp.MustCompile(
	`(?s)CREATE TABLE notification_preferences(?:_new)?\s*\(.*?kind\s+TEXT\s+NOT NULL\s+CHECK \(kind IN \(([^)]*)\)\)`)

// userNotificationsKindCheckPattern extracts the user_notifications kind
// CHECK's value list from the newest migration that rebuilds the table. Its
// historical order differs from notification_preferences', so that copy is
// compared as a set.
var userNotificationsKindCheckPattern = regexp.MustCompile(
	`(?s)CREATE TABLE user_notifications(?:_new)?\s*\(.*?kind\s+TEXT\s+NOT NULL\s+CHECK \(kind IN \(([^)]*)\)\)`)

// quotedKindPattern matches one single-quoted lower_snake kind.
var quotedKindPattern = regexp.MustCompile(`'([a-z_]+)'`)

// openAPIPathKindEnumPattern extracts the `{kind}` path parameter's enum.
var openAPIPathKindEnumPattern = regexp.MustCompile(
	`(?s)name: kind\b.*?enum:\s*\n((?:\s+- [a-z_]+\s*\n)+)`)

// openAPISchemaKindEnumPattern extracts the NotificationPreference schema's
// kind property enum.
var openAPISchemaKindEnumPattern = regexp.MustCompile(
	`(?s)NotificationPreference:\n.*?properties:\n\s+kind:\n\s+type: string\n\s+enum:\n((?:\s+- [a-z_]+\s*\n)+)`)

// openAPIDiscriminatorPattern extracts the feed discriminator's mapping
// entries.
var openAPIDiscriminatorPattern = regexp.MustCompile(
	`(?s)discriminator:\s*\n\s+propertyName: kind\s*\n\s+mapping:\s*\n((?:\s+[a-z_]+: "[^"]+"\s*\n)+)`)

// mappingEntryPattern matches one `kind: "#/components/schemas/…"` entry.
var mappingEntryPattern = regexp.MustCompile(`(?m)^\s+([a-z_]+): "([^"]+)"\s*$`)

// notificationItemOneOfPattern extracts the feed item union's schema refs.
var notificationItemOneOfPattern = regexp.MustCompile(
	`(?s)NotificationItem:\s*\n\s+oneOf:\s*\n((?:\s+- \$ref: "[^"]+"\s*\n)+)`)

// oneOfRefPattern matches one plain `- $ref: "…"` list entry.
var oneOfRefPattern = regexp.MustCompile(`(?m)^\s+- \$ref: "([^"]+)"\s*$`)

// listedKindPattern matches one `- lower_snake` YAML enum entry.
var listedKindPattern = regexp.MustCompile(`(?m)^\s+- ([a-z_]+)\s*$`)

// frontendKindUnionPattern extracts the body of the web client's
// `export type PushNotificationKind = …;` union.
var frontendKindUnionPattern = regexp.MustCompile(`(?s)export type PushNotificationKind =(.*?);`)

// quotedUnionMemberPattern matches one `"lower_snake"` union member.
var quotedUnionMemberPattern = regexp.MustCompile(`"([a-z_]+)"`)

// swSentenceForBodyPattern extracts the service worker's sentenceFor body
// (the closing brace sits at column 0, so a lazy match stops there).
var swSentenceForBodyPattern = regexp.MustCompile(`(?s)function sentenceFor\(p\) \{(.*?)\n\}`)

// swCaseKindPattern matches one `case "lower_snake":` branch label.
var swCaseKindPattern = regexp.MustCompile(`case "([a-z_]+)":`)

// lineCommentPattern strips a trailing `// …` so a COMMENTED-OUT entry cannot
// satisfy the pin (the audit pin precedent).
var lineCommentPattern = regexp.MustCompile(`//[^\n]*`)

// kindConstantPattern matches a declared string-valued notification kind
// constant (its optional explicit type included) in the handler package.
var kindConstantPattern = regexp.MustCompile(`(?m)^\s*(notificationKind[A-Za-z]+)(?:\s+string)?\s*=\s*"([a-z_]+)"`)

// pushKindRefPattern matches one notificationKind… identifier inside the
// pushKinds slice literal.
var pushKindRefPattern = regexp.MustCompile(`\bnotificationKind[A-Za-z]+\b`)

// accountItemSchema is the mapping target that must never carry a feed kind.
const accountItemSchema = "#/components/schemas/AccountNotificationItem"

// assertPushKindCopy fails when one copy of the vocabulary differs from
// pushKinds in length or order.
func assertPushKindCopy(t *testing.T, source string, kinds []string) {
	t.Helper()
	if len(kinds) != len(pushKinds) {
		t.Fatalf("%s lists %d kinds (%v), pushKinds %d (%v)", source, len(kinds), kinds, len(pushKinds), pushKinds)
	}
	for i := range pushKinds {
		if kinds[i] != pushKinds[i] {
			t.Errorf("%s: kind %d is %q, pushKinds has %q — the copies must stay identical and in the same order", source, i, kinds[i], pushKinds[i])
		}
	}
}

// TestPushKindsMatchMigrationChecks pins the newest rebuilds of BOTH
// kind-bearing tables: notification_preferences order-equal (the Go list is
// read as that CHECK's order) and user_notifications as a set (its historical
// order differs by design). The CHECKs are the only copies the database
// itself enforces.
func TestPushKindsMatchMigrationChecks(t *testing.T) {
	t.Parallel()

	root := handlerRepoRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, "internal", "database", "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no migration files found — the glob has drifted")
	}
	slices.Sort(paths)

	var preferences, notifications []string
	for _, path := range slices.Backward(paths) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if preferences == nil && bytes.Contains(data, []byte("CREATE TABLE notification_preferences")) {
			preferences = extractedKindCheck(t, data, preferenceKindCheckPattern, filepath.Base(path)+" notification_preferences CHECK")
		}
		if notifications == nil && bytes.Contains(data, []byte("CREATE TABLE user_notifications")) {
			notifications = extractedKindCheck(t, data, userNotificationsKindCheckPattern, filepath.Base(path)+" user_notifications CHECK")
		}
		if preferences != nil && notifications != nil {
			break
		}
	}
	if preferences == nil {
		t.Fatal("no migration rebuilds notification_preferences with an extractable kind CHECK")
	}
	if notifications == nil {
		t.Fatal("no migration rebuilds user_notifications with an extractable kind CHECK")
	}

	assertPushKindCopy(t, "the notification_preferences CHECK", preferences)

	sortedNotifications := slices.Clone(notifications)
	sortedPushKinds := slices.Clone(pushKinds)
	slices.Sort(sortedNotifications)
	slices.Sort(sortedPushKinds)
	if !slices.Equal(sortedNotifications, sortedPushKinds) {
		t.Fatalf("the user_notifications CHECK lists %v, pushKinds %v — the sets must match (the order differs by design)", notifications, pushKinds)
	}
}

// extractedKindCheck parses one quoted kind list out of a migration CHECK.
func extractedKindCheck(t *testing.T, data []byte, pattern *regexp.Regexp, source string) []string {
	t.Helper()
	block := pattern.FindSubmatch(data)
	if block == nil {
		t.Fatalf("%s: the extraction pattern has drifted", source)
	}
	matches := quotedKindPattern.FindAllSubmatch(block[1], -1)
	if len(matches) == 0 {
		t.Fatalf("%s: the CHECK carries no quoted kinds — the extraction pattern has drifted", source)
	}
	kinds := make([]string, 0, len(matches))
	for _, m := range matches {
		kinds = append(kinds, string(m[1]))
	}
	return kinds
}

// TestPushKindsMatchOpenAPI pins the wire contract: the `{kind}` path enum,
// the `NotificationPreference` schema enum, and the feed discriminator's
// mapping for every kind.
func TestPushKindsMatchOpenAPI(t *testing.T) {
	t.Parallel()

	root := handlerRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read the wire contract: %v", err)
	}

	if n := bytes.Count(data, []byte("name: kind")); n != 1 {
		t.Fatalf("docs/api/openapi.yaml must carry exactly one `name: kind` parameter, found %d — the pin's anchor moved", n)
	}
	assertPushKindCopy(t, "the OpenAPI {kind} path enum", listedKinds(t, data, openAPIPathKindEnumPattern, "the {kind} path parameter enum"))

	if n := bytes.Count(data, []byte("NotificationPreference:")); n != 1 {
		t.Fatalf("docs/api/openapi.yaml must carry exactly one `NotificationPreference:` schema, found %d — the pin's anchor moved", n)
	}
	assertPushKindCopy(t, "the OpenAPI NotificationPreference enum", listedKinds(t, data, openAPISchemaKindEnumPattern, "the NotificationPreference schema enum"))

	if n := bytes.Count(data, []byte("discriminator:")); n != 1 {
		t.Fatalf("docs/api/openapi.yaml must carry exactly one discriminator, found %d — the pin's anchor moved", n)
	}
	block := openAPIDiscriminatorPattern.FindSubmatch(data)
	if block == nil {
		t.Fatal("the NotificationItem discriminator mapping did not match — the extraction pattern has drifted")
	}
	mapping := map[string]string{}
	for _, m := range mappingEntryPattern.FindAllSubmatch(block[1], -1) {
		mapping[string(m[1])] = string(m[2])
	}
	if len(mapping) == 0 {
		t.Fatal("the discriminator mapping is empty — the extraction pattern has drifted")
	}

	oneOfBlock := notificationItemOneOfPattern.FindSubmatch(data)
	if oneOfBlock == nil {
		t.Fatal("the NotificationItem oneOf list did not match — the extraction pattern has drifted")
	}
	var oneOf []string
	for _, m := range oneOfRefPattern.FindAllSubmatch(oneOfBlock[1], -1) {
		oneOf = append(oneOf, string(m[1]))
	}

	for _, kind := range pushKinds {
		schema, ok := mapping[kind]
		if !ok {
			t.Errorf("the NotificationItem discriminator has no mapping for %q — a new kind needs an item schema and a mapping", kind)
			continue
		}
		if !slices.Contains(oneOf, schema) {
			t.Errorf("the discriminator maps %q to %s, which is not in the NotificationItem oneOf list", kind, schema)
		}
		if schema == accountItemSchema {
			t.Errorf("the discriminator maps the feed kind %q to the account item schema — the five audit events own that shape", kind)
		}
	}
	for kind, schema := range mapping {
		if slices.Contains(pushKinds, kind) || schema == accountItemSchema {
			continue
		}
		t.Errorf("the discriminator maps %q to %s, which is neither a feed kind nor an account event", kind, schema)
	}
}

// TestPushKindsMatchFrontendUnion pins the web client's union: it is the
// list the settings UI renders, so a kind missing there is invisible.
func TestPushKindsMatchFrontendUnion(t *testing.T) {
	t.Parallel()

	root := handlerRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "web", "src", "features", "notifications", "data-access", "types.ts"))
	if err != nil {
		t.Fatalf("read the frontend notification types: %v", err)
	}

	block := frontendKindUnionPattern.FindSubmatch(data)
	if block == nil {
		t.Fatal("web/src/features/notifications/data-access/types.ts must declare `export type PushNotificationKind = …;`")
	}
	body := lineCommentPattern.ReplaceAll(block[1], nil)
	matches := quotedUnionMemberPattern.FindAllSubmatch(body, -1)
	frontend := make([]string, 0, len(matches))
	for _, m := range matches {
		frontend = append(frontend, string(m[1]))
	}

	assertPushKindCopy(t, "the frontend PushNotificationKind union", frontend)
}

// TestPushKindsMatchServiceWorker pins the worker's branch set: a kind with
// no branch falls through `sentenceFor`'s default and the notification is
// dropped silently.
func TestPushKindsMatchServiceWorker(t *testing.T) {
	t.Parallel()

	root := handlerRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "web", "public", "sw.js"))
	if err != nil {
		t.Fatalf("read the service worker: %v", err)
	}

	block := swSentenceForBodyPattern.FindSubmatch(data)
	if block == nil {
		t.Fatal("web/public/sw.js must declare `function sentenceFor(p) { … }`")
	}
	body := lineCommentPattern.ReplaceAll(block[1], nil)

	// comment_removed is handled before the switch (its actorless shape needs
	// no actor guard); every other feed kind is a case label.
	removedAt := bytes.Index(body, []byte(`p.kind === "comment_removed"`))
	switchAt := bytes.Index(body, []byte("switch (p.kind)"))
	if removedAt < 0 || switchAt < 0 {
		t.Fatal(`sentenceFor must test "comment_removed" and switch on p.kind — the pin's anchors moved`)
	}
	if removedAt > switchAt {
		t.Error(`sentenceFor must handle "comment_removed" BEFORE the switch — the default branch would drop it first`)
	}
	expected := slices.DeleteFunc(slices.Clone(pushKinds), func(kind string) bool {
		return kind == notificationKindCommentRemoved
	})
	var cases []string
	for _, m := range swCaseKindPattern.FindAllSubmatch(body, -1) {
		cases = append(cases, string(m[1]))
	}
	if len(cases) != len(expected) {
		t.Fatalf("sentenceFor has %d case labels (%v), pushKinds minus %s has %d (%v)", len(cases), cases, notificationKindCommentRemoved, len(expected), expected)
	}
	for _, kind := range expected {
		if !slices.Contains(cases, kind) {
			t.Errorf("sentenceFor has no case for %q — the worker would drop that notification silently", kind)
		}
	}
	for _, kind := range cases {
		if !slices.Contains(expected, kind) {
			t.Errorf("sentenceFor has a case for %q — not a switch-handled feed kind (only the actorless %s is handled before the switch)", kind, notificationKindCommentRemoved)
		}
	}
}

// TestPushKindConstantsJoinPushKinds pins `pushKinds`' completeness: every
// string-valued notificationKind constant is listed there, and every
// identifier in the slice names a declared constant — a new kind cannot be
// half-added.
func TestPushKindConstantsJoinPushKinds(t *testing.T) {
	t.Parallel()

	root := handlerRepoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "internal", "handler", "*.go"))
	if err != nil {
		t.Fatalf("glob the handler package: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no handler source files found — the glob has drifted")
	}
	var src []byte
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(file), err)
		}
		src = append(src, data...)
	}

	declared := map[string]string{}
	for _, m := range kindConstantPattern.FindAllSubmatch(src, -1) {
		declared[string(m[1])] = string(m[2])
	}
	if len(declared) == 0 {
		t.Fatal("no notification-kind constants found — the extraction pattern has drifted")
	}

	pushSrc, err := os.ReadFile(filepath.Join(root, "internal", "handler", "push.go"))
	if err != nil {
		t.Fatalf("read push.go: %v", err)
	}
	const marker = "var pushKinds = []string{"
	start := bytes.Index(pushSrc, []byte(marker))
	if start < 0 {
		t.Fatalf("push.go must declare `%s`", marker)
	}
	end := bytes.IndexByte(pushSrc[start:], '}')
	if end < 0 {
		t.Fatal("the pushKinds slice literal has no closing brace")
	}
	body := lineCommentPattern.ReplaceAll(pushSrc[start:start+end], nil)

	listed := map[string]bool{}
	for _, m := range pushKindRefPattern.FindAll(body, -1) {
		name := string(m)
		listed[name] = true
		if _, ok := declared[name]; !ok {
			t.Errorf("pushKinds lists %s, which is not a declared string-valued kind constant", name)
		}
	}
	for name := range declared {
		if !listed[name] {
			t.Errorf("constant %s is never listed in pushKinds — add it there too", name)
		}
	}
}

// listedKinds extracts one YAML enum's `- lower_snake` entries.
func listedKinds(t *testing.T, data []byte, pattern *regexp.Regexp, source string) []string {
	t.Helper()
	block := pattern.FindSubmatch(data)
	if block == nil {
		t.Fatalf("%s did not match — the extraction pattern has drifted", source)
	}
	matches := listedKindPattern.FindAllSubmatch(block[1], -1)
	if len(matches) == 0 {
		t.Fatalf("%s matched but listed no entries — the extraction pattern has drifted", source)
	}
	kinds := make([]string, 0, len(matches))
	for _, m := range matches {
		kinds = append(kinds, string(m[1]))
	}
	return kinds
}

// handlerRepoRoot walks up to the directory holding go.mod.
func handlerRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root (go.mod) not found")
		}
		dir = parent
	}
}
