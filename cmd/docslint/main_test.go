package main

import (
	"strings"
	"testing"
)

func TestCheckScopeDocs_MissingFeatureDoc(t *testing.T) {
	root := writeTree(t, map[string]string{
		"internal/foo/foo.go":      "package foo\n",
		"internal/foo/foo_test.go": "package foo\n",
		"web/src/core/core.ts":     "export {};\n",
	})

	problems, err := checkScopeDocs(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"internal/foo: holds Go source but has no AGENTS.md",
		"web/src/core: web scope has no AGENTS.md",
	}
	if len(problems) != len(want) {
		t.Fatalf("checkScopeDocs() = %v, want %v", problems, want)
	}
	for i, w := range want {
		if problems[i] != w {
			t.Errorf("checkScopeDocs()[%d] = %q, want %q", i, problems[i], w)
		}
	}
}

func TestCheckScopeDocs_TestOnlyPackageIsNotAScope(t *testing.T) {
	root := writeTree(t, map[string]string{
		"internal/foo/foo_test.go": "package foo\n",
	})

	problems, err := checkScopeDocs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("checkScopeDocs() = %v, want none", problems)
	}
}

func TestCheckIndexReachability_ProseMentionIsNotALink(t *testing.T) {
	root := writeTree(t, map[string]string{
		docsIndex:                   "# Index\n\n[ok](patterns/ok.md)\nA prose mention of patterns/go/prose.md.\n",
		"docs/patterns/ok.md":       "# ok\n",
		"docs/patterns/go/prose.md": "# prose\n",
	})

	problems, err := checkIndexReachability(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "docs/patterns/go/prose.md") {
		t.Fatalf("checkIndexReachability() = %v, want the prose-mentioned doc flagged", problems)
	}
}

func TestCheckIndexReachability_ArchitectureDocMustBeLinked(t *testing.T) {
	root := writeTree(t, map[string]string{
		docsIndex:              "# Index\n\n[patterns](patterns/ok.md)\n",
		"docs/patterns/ok.md":  "# ok\n",
		"docs/architecture.md": "# arch\n",
	})
	problems, err := checkIndexReachability(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "docs/architecture.md") {
		t.Fatalf("checkIndexReachability() = %v, want the unlinked architecture doc flagged", problems)
	}

	linkedRoot := writeTree(t, map[string]string{
		docsIndex:              "# Index\n\n[patterns](patterns/ok.md)\n[arch](architecture.md)\n",
		"docs/patterns/ok.md":  "# ok\n",
		"docs/architecture.md": "# arch\n",
	})
	problems, err = checkIndexReachability(linkedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("checkIndexReachability() = %v, want none for a linked architecture doc", problems)
	}
}

func TestCheckLinks_BrokenLinkOutsideTheIndex(t *testing.T) {
	root := writeTree(t, map[string]string{
		docsIndex:                           "# Index\n\n[ok](patterns/ok.md)\n",
		"docs/patterns/ok.md":               "# ok\n\n[gone](missing.md)\n[good](docs-conventions.md)\n",
		"docs/patterns/docs-conventions.md": "# conventions\n",
	})

	problems, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("checkLinks() = %v, want exactly the sibling link in docs/patterns/ok.md", problems)
	}
	if !strings.Contains(problems[0], "docs/patterns/ok.md: link does not resolve: missing.md") {
		t.Errorf("problems[0] = %q, want the sibling link named with its file", problems[0])
	}
}

func TestCheckLinks_IgnoresFencesAndExternalTargets(t *testing.T) {
	root := writeTree(t, map[string]string{
		docsIndex: "# Index\n\n[head](#a-heading)\n[web](https://go.dev)\n[abs](/nope)\n```\n[ignored](gone.md)\n```\n",
	})

	problems, err := checkLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("checkLinks() = %v, want none", problems)
	}
}

func TestCheckCitations_FlagsEveryReferenceForm(t *testing.T) {
	root := writeTree(t, map[string]string{
		"internal/foo/foo.go": "// decision 0007, decision-0008, and decisions/0009-x.md\npackage foo\n",
	})

	problems, err := checkCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "3 references") {
		t.Fatalf("checkCitations() = %v, want one problem reporting three references", problems)
	}
}

func TestCheckCitations_CountsExtensionlessBuildFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"Makefile":   "# decision 0007: see the shape\nall:\n\t@true\n",
		"Dockerfile": "# decision 0008\nFROM scratch\n",
		"Caddyfile":  "# decision 0009\n",
	})

	problems, err := checkCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 3 {
		t.Fatalf("checkCitations() = %v, want Makefile, Dockerfile, and Caddyfile each flagged", problems)
	}
}

func TestCheckCitations_SkipsOnlyCheckerTestFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		checkerDir + "/main_test.go": "// decision 0007 fixture\n",
		checkerDir + "/notes.md":     "# decision 0008 in a non-test file\n",
	})

	problems, err := checkCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], checkerDir+"/notes.md") {
		t.Fatalf("checkCitations() = %v, want only the checker's non-test file flagged", problems)
	}
}

func TestCheckCitations_ExemptsChecksummedMigrations(t *testing.T) {
	root := writeTree(t, map[string]string{
		"internal/database/migrations/0001_users.sql": "-- decision 0007: checksummed history\n",
		"internal/database/schema.go":                 "// decision 0008\n",
		"internal/database/schema.sql":                "-- decision 0009\n",
	})

	problems, err := checkCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(problems, "\n")
	if len(problems) != 2 || !strings.Contains(got, "internal/database/schema.go") || !strings.Contains(got, "internal/database/schema.sql") {
		t.Fatalf("checkCitations() = %v, want the two living files flagged and the migration exempt", problems)
	}
	if strings.Contains(got, "migrations/") {
		t.Errorf("checkCitations() = %v, want no problem for the exempt migration directory", problems)
	}
}

func TestCheckCitations_MigrationExemptionIsPathScoped(t *testing.T) {
	root := writeTree(t, map[string]string{
		"internal/database/migrations.md": "// decision 0007\n",
	})

	problems, err := checkCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "internal/database/migrations.md") {
		t.Fatalf("checkCitations() = %v, want a file beside the exempt directory flagged", problems)
	}
}

func TestRun_CleanTree(t *testing.T) {
	root := writeTree(t, map[string]string{
		"AGENTS.md":                         "# root\n",
		docsIndex:                           "# Index\n\n[conventions](patterns/docs-conventions.md)\n",
		"docs/patterns/docs-conventions.md": "# conventions\n",
		"internal/foo/foo.go":               "package foo\n",
		"internal/foo/AGENTS.md":            "# foo\n",
		"internal/database/migrations/0001_users.sql": "-- decision 0007: checksummed history\n",
		"cmd/tool/main.go":                "package main\n",
		"cmd/tool/AGENTS.md":              "# tool\n",
		"web/src/core/core.ts":            "export {};\n",
		"web/src/core/AGENTS.md":          "# core\n",
		"web/src/shared/shared.ts":        "export {};\n",
		"web/src/shared/AGENTS.md":        "# shared\n",
		"web/src/features/blog/blog.ts":   "export {};\n",
		"web/src/features/blog/AGENTS.md": "# blog\n",
	})

	problems, err := run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("run() on a clean fixture tree = %v, want none", problems)
	}
}

func TestCheckCitations_CountsJSAndToml(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/public/sw.js": "// decision 0007\n",
		".air.toml":        "# decision 0008\n",
	})

	problems, err := checkCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 {
		t.Fatalf("checkCitations() = %v, want sw.js and .air.toml flagged", problems)
	}
}

func TestCheckCitations_CountsTheLogrotateDropIn(t *testing.T) {
	root := writeTree(t, map[string]string{
		"deploy/logrotate.sick-fansubs": "# decision 0007\n",
	})

	problems, err := checkCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "deploy/logrotate.sick-fansubs") {
		t.Fatalf("checkCitations() = %v, want the logrotate drop-in flagged", problems)
	}
}

func TestCheckCitations_SkipsRootDist(t *testing.T) {
	root := writeTree(t, map[string]string{
		"dist/notes.md": "# decision 0007 (GoReleaser output)\n",
	})

	problems, err := checkCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("checkCitations() = %v, want the root dist output skipped", problems)
	}
}

func TestCheckCitations_IgnoresVendoredAssetKinds(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/public/fonts/OFL-license.txt": "decision 0007 in vendored text\n",
		"web/public/images/brand.svg":      "<!-- decision 0008 -->\n",
	})

	problems, err := checkCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("checkCitations() = %v, want vendored .txt/.svg assets out of the counted universe", problems)
	}
}
