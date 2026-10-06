// Package codestyle holds the repository's code-shape guards. Every file here
// is a test file, so the package has no buildable code and nothing in the
// module can import it; each guard names the pattern doc that owns its rule.
package codestyle

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// checkFunc reports one finding, formatted for a test failure message, per
// offending position in f.
type checkFunc func(rel string, f *ast.File, fset *token.FileSet) []string

// repoRoot returns the directory holding go.mod by walking up from the test's
// working directory. It fails the test instead of scanning a wrong tree.
func repoRoot(t *testing.T) string {
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
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// walkGoFiles parses every buildable Go file under cmd/ and internal/ and hands
// each to visit with its repository-relative path. Directory and file names
// starting with "." or "_", and directories named testdata, are skipped as the
// go tool skips them.
func walkGoFiles(t *testing.T, visit func(rel string, f *ast.File, fset *token.FileSet)) {
	t.Helper()
	root := repoRoot(t)
	for _, top := range []string{"cmd", "internal"} {
		base := filepath.Join(root, top)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if path != base && (strings.HasPrefix(name, ".") ||
					strings.HasPrefix(name, "_") || name == "testdata") {
					return fs.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || !strings.HasSuffix(name, ".go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
			if err != nil {
				return fmt.Errorf("parse %s: %w", rel, err)
			}
			visit(rel, f, fset)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", top, err)
		}
	}
}

// importNames maps each imported package's local name to its import path, so a
// check can key on the path instead of on an identifier that may be aliased.
func importNames(f *ast.File) map[string]string {
	names := make(map[string]string, len(f.Imports))
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := path
		if slash := strings.LastIndex(path, "/"); slash >= 0 {
			name = path[slash+1:]
		}
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names[name] = path
	}
	return names
}

// scanRepo collects the findings check reports across the whole tree.
func scanRepo(t *testing.T, check checkFunc) []string {
	t.Helper()
	var findings []string
	walkGoFiles(t, func(rel string, f *ast.File, fset *token.FileSet) {
		findings = append(findings, check(rel, f, fset)...)
	})
	return findings
}

// assertClean fails the test when the repository-wide scan reports a finding.
func assertClean(t *testing.T, check checkFunc) {
	t.Helper()
	if findings := scanRepo(t, check); len(findings) > 0 {
		t.Fatalf("found %d:\n%s", len(findings), strings.Join(findings, "\n"))
	}
}

// checkSource runs check over one snippet parsed under name.
func checkSource(t *testing.T, check checkFunc, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse snippet: %v\n%s", err, src)
	}
	return check(name, f, fset)
}

// selfCheck proves a check flags the shape it guards and leaves a clean snippet
// alone, so a check that stopped matching cannot pass vacuously.
func selfCheck(t *testing.T, check checkFunc, bad, good string) {
	t.Helper()
	if got := checkSource(t, check, "snippet.go", bad); len(got) == 0 {
		t.Fatalf("check misses the shape it guards:\n%s", bad)
	}
	if got := checkSource(t, check, "snippet.go", good); len(got) != 0 {
		t.Fatalf("check flags a clean snippet %v:\n%s", got, good)
	}
}

// TestWalkerFindsTheTree fails when the walker sees an implausibly small tree,
// misses a tree, or misses a known file, so no guard can pass by scanning
// nothing. The floors are checked per top-level tree because either tree alone
// clears the total.
func TestWalkerFindsTheTree(t *testing.T) {
	const minGoFiles, minCmdFiles = 200, 20

	var total, cmdCount int
	var sawHandlerFile, sawMainFile bool
	walkGoFiles(t, func(rel string, _ *ast.File, _ *token.FileSet) {
		total++
		switch {
		case strings.HasPrefix(rel, "cmd"+string(filepath.Separator)):
			cmdCount++
		case rel == filepath.Join("internal", "handler", "auth.go"):
			sawHandlerFile = true
		}
		if rel == filepath.Join("cmd", "api", "main.go") {
			sawMainFile = true
		}
	})
	if total < minGoFiles || cmdCount < minCmdFiles || !sawHandlerFile || !sawMainFile {
		t.Fatalf("walker saw %d Go files (%d under cmd), internal/handler/auth.go: %v, cmd/api/main.go: %v; want at least %d files, at least %d under cmd, and both named files",
			total, cmdCount, sawHandlerFile, sawMainFile, minGoFiles, minCmdFiles)
	}
}
