package storetest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNotImportedByProductionCode pins the package's contract. A non-test file
// that imports it would link the testing package into the server binary, so the
// rule is checked rather than trusted.
func TestNotImportedByProductionCode(t *testing.T) {
	t.Parallel()
	const importPath = "sick-fansubs/internal/store/storetest"

	root := moduleRoot(t)
	var offenders []string
	scanned := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "data", "dist":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), importPath) {
			offenders = append(offenders, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	if scanned == 0 {
		t.Fatalf("scan of %s found no Go files; the guard would pass vacuously", root)
	}
	if len(offenders) > 0 {
		t.Errorf("%s is imported by production code at %v; it is test support only", importPath, offenders)
	}
}

// moduleRoot walks up from the working directory until it finds the module's
// go.mod, so the guard checks the real tree whatever directory the test binary
// runs from.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s; cannot anchor the guard", dir)
		}
		dir = parent
	}
}
