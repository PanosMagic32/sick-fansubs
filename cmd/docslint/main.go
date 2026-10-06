// Command docslint checks the documentation structure of the repository.
//
// It enforces four rules from docs/patterns/docs-conventions.md: every code
// scope has an AGENTS.md feature doc, every pattern, operations, and
// architecture doc is linked from the docs index, relative links resolve, and
// no living file cites a retired decision record. It runs as `make docs-lint` inside `make check`.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	docsIndex = "docs/README.md"
	// checkerDir is this command; only its test files are skipped, because a
	// fixture must contain the citation forms the tool matches.
	checkerDir = "cmd/docslint"
)

// nestedSkipNames are never walked, at any depth.
var nestedSkipNames = map[string]bool{".git": true, "node_modules": true}

// rootSkips are root-relative paths that are never walked: local runtime data,
// build output (all gitignored), and the pass's scratch material, which is
// deleted rather than migrated.
var rootSkips = map[string]bool{
	"data": true, "dist": true, "web/dist": true, "cmd/api/web/dist": true, ".scratch": true,
}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "docslint: unexpected argument:", flag.Arg(0))
		os.Exit(2)
	}

	problems, err := run(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docslint:", err)
		os.Exit(2)
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "docslint:", p)
	}
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "docslint: %d problem(s)\n", len(problems))
		os.Exit(1)
	}
	fmt.Println("docslint: ok")
}

func run(root string) ([]string, error) {
	var problems []string
	for _, check := range []func(string) ([]string, error){
		checkScopeDocs,
		checkIndexReachability,
		checkLinks,
		checkCitations,
	} {
		found, err := check(root)
		if err != nil {
			return nil, err
		}
		problems = append(problems, found...)
	}
	slices.Sort(problems)
	return problems, nil
}

// checkScopeDocs requires an AGENTS.md in every code scope: each Go package
// directory holding production source, and each web scope directory.
func checkScopeDocs(root string) ([]string, error) {
	var problems []string
	for _, base := range []string{"cmd", "internal"} {
		entries, err := os.ReadDir(filepath.Join(root, base))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", base, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.ToSlash(filepath.Join(base, entry.Name()))
			hasGo, err := hasProductionGo(root, dir)
			if err != nil {
				return nil, err
			}
			if hasGo && !exists(filepath.Join(root, dir, "AGENTS.md")) {
				problems = append(problems, dir+": holds Go source but has no AGENTS.md")
			}
		}
	}

	scopes := []string{"web/src/core", "web/src/shared"}
	features, err := os.ReadDir(filepath.Join(root, "web/src/features"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read web/src/features: %w", err)
	}
	for _, entry := range features {
		if entry.IsDir() {
			scopes = append(scopes, "web/src/features/"+entry.Name())
		}
	}
	for _, scope := range scopes {
		if !exists(filepath.Join(root, scope)) {
			continue
		}
		if !exists(filepath.Join(root, scope, "AGENTS.md")) {
			problems = append(problems, scope+": web scope has no AGENTS.md")
		}
	}
	return problems, nil
}

// checkIndexReachability requires every pattern and operations doc — and the
// architecture doc, when present — to be linked from the docs index. A prose
// mention does not count: the index must carry a link whose target resolves to
// the doc.
func checkIndexReachability(root string) ([]string, error) {
	index := filepath.Join(root, docsIndex)
	linked, err := linkTargets(index)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, dir := range []string{"docs/patterns", "docs/ops"} {
		if !exists(filepath.Join(root, dir)) {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			if !linked[filepath.Clean(path)] {
				problems = append(problems, filepath.ToSlash(relTo(root, path))+": not linked from "+docsIndex)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	architecture := filepath.Join(root, "docs", "architecture.md")
	if exists(architecture) && !linked[filepath.Clean(architecture)] {
		problems = append(problems, "docs/architecture.md: not linked from "+docsIndex)
	}
	return problems, nil
}

// plural renders a count with the right noun form.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func hasProductionGo(root, dir string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		return false, fmt.Errorf("read %s: %w", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			return true, nil
		}
	}
	return false, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
