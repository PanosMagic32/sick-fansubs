package routes

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The route table and docs/api/openapi.yaml are two copies of one contract:
// a path added, renamed, or removed on one side only would ship a wire
// surface the other side denies. This pin compares them (the audit
// event-enum pin precedent) — the OpenAPI side by line scan, the registration
// side by parsing this package with go/parser, because ServeMux exposes no
// registration enumeration. A registration shape the scanner does not
// understand fails the test instead of disappearing.
var (
	// openAPIPathPattern matches a path key at the two-space indent under
	// `paths:`.
	openAPIPathPattern = regexp.MustCompile(`^  (/[^:]+):$`)
	// openAPIMethodPattern matches an operation key at the four-space indent
	// under a path.
	openAPIMethodPattern = regexp.MustCompile(`^    (get|post|put|patch|delete|head|options|trace):$`)

	// methodSpacePattern matches a `"METHOD "` literal — the comment
	// namespace's concatenation head.
	methodSpacePattern = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE) $`)
)

// nonOpenAPIRoutes are registered paths deliberately absent from the wire
// contract: asset delivery serves bytes, not problems (internal/handler/
// AGENTS.md owns its contract).
var nonOpenAPIRoutes = map[string]bool{
	"GET /media/images/{file}": true,
}

// TestOpenAPIRoutesMatchRegistrations pins both directions: every documented
// operation dispatches to a registered pattern, and every registered
// operation is documented (minus the recorded asset path).
func TestOpenAPIRoutesMatchRegistrations(t *testing.T) {
	t.Parallel()

	documented := parseOpenAPIRoutes(t)
	registered := scanRegisteredRoutes(t)

	if len(documented) < 60 || len(registered) < 60 {
		t.Fatalf("scan collapsed: %d documented, %d registered — the parsing patterns drifted", len(documented), len(registered))
	}
	for _, key := range []string{
		"GET /health/live",
		"GET /media/images/{file}",
		"GET /api/v1/blog-posts/{id}/comments",
		"DELETE /api/v1/projects/{id}/comments/{commentId}/heart",
		"PUT /api/v1/notification-preferences/{kind}",
	} {
		if !registered[key] {
			t.Errorf("registration scan missed %s — the extraction patterns drifted", key)
		}
	}

	for key := range documented {
		if !registered[key] {
			t.Errorf("docs/api/openapi.yaml documents %s, but no route registers it", key)
		}
	}
	for key := range registered {
		if nonOpenAPIRoutes[key] {
			continue
		}
		if !documented[key] {
			t.Errorf("route %s is registered, but docs/api/openapi.yaml does not document it", key)
		}
	}
}

// TestOpenAPIConditionalAndCreateRefs pins the If-Match parameter and its
// four conditional operations, and the split create/update schemas referenced
// by exactly one body each.
func TestOpenAPIConditionalAndCreateRefs(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(routesRepoRoot(t), "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read the wire contract: %v", err)
	}
	body := string(data)

	for _, check := range []struct {
		name   string
		needle string
		want   int
	}{
		{"If-Match component", "\n    IfMatch:\n", 1},
		{"If-Match references", `$ref: "#/components/parameters/IfMatch"`, 4},
		{"If-Match pattern", `pattern: '^"[1-9][0-9]*"$'`, 1},
		{"BlogPostCreate declaration", "\n    BlogPostCreate:\n", 1},
		{"BlogPostCreate reference", `$ref: "#/components/schemas/BlogPostCreate"`, 1},
		{"BlogPostWrite reference", `$ref: "#/components/schemas/BlogPostWrite"`, 1},
		{"ProjectCreate reference", `$ref: "#/components/schemas/ProjectCreate"`, 1},
		{"ProjectUpdate reference", `$ref: "#/components/schemas/ProjectUpdate"`, 1},
	} {
		if got := strings.Count(body, check.needle); got != check.want {
			t.Errorf("%s: found %d occurrences, want %d", check.name, got, check.want)
		}
	}
}

// parseOpenAPIRoutes returns the documented operations as "METHOD path"
// keys.
func parseOpenAPIRoutes(t *testing.T) map[string]bool {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(routesRepoRoot(t), "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read the wire contract: %v", err)
	}

	out := map[string]bool{}
	var current string
	inPaths := false
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "paths:" {
			inPaths = true
			continue
		}
		if !inPaths {
			continue
		}
		if line == "components:" {
			break
		}
		if m := openAPIPathPattern.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := openAPIMethodPattern.FindStringSubmatch(line); m != nil && current != "" {
			out[strings.ToUpper(m[1])+" "+current] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("no OpenAPI operations parsed — the path/method patterns drifted")
	}
	return out
}

// scanRegisteredRoutes returns this package's method-prefixed registrations
// as "METHOD path" keys, resolving the auth/users subtree prefixes and the
// comment namespace's concatenated patterns.
func scanRegisteredRoutes(t *testing.T) map[string]bool {
	t.Helper()

	if len(nonOpenAPIRoutes) != 1 {
		t.Fatalf("nonOpenAPIRoutes has %d entries — each exemption needs a recorded reason", len(nonOpenAPIRoutes))
	}

	root := filepath.Join(routesRepoRoot(t), "internal", "routes")
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root {
				return fmt.Errorf("unexpected subdirectory %s — extend the parity pin", path)
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		collectRegistrations(t, fset, file, name, out)
		return nil
	})
	if err != nil {
		t.Fatalf("scan route registrations: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no route registrations parsed — the extraction pattern drifted")
	}
	return out
}

// collectRegistrations adds one file's Handle/HandleFunc patterns to out.
func collectRegistrations(t *testing.T, fset *token.FileSet, file *ast.File, name string, out map[string]bool) {
	t.Helper()

	prefix := subtreePrefix(name)
	segments := commentSegments(file)

	// bad records a shape the scanner cannot interpret, with its position.
	bad := func(pos token.Pos) {
		t.Errorf("%s: unrecognized registration shape — extend the parity pin", fset.Position(pos))
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isMuxHandleCall(call) || len(call.Args) == 0 {
			return true
		}
		switch first := ast.Unparen(call.Args[0]).(type) {
		case *ast.BasicLit:
			if first.Kind != token.STRING {
				bad(call.Pos())
				return true
			}
			s, err := strconv.Unquote(first.Value)
			if err != nil {
				bad(call.Pos())
				return true
			}
			// A method-less literal is a JSON-404 fallback or a subtree mount;
			// anything else method-less is a registration the pin cannot read.
			if method, path, ok := splitMethodPattern(s); ok {
				out[method+" "+resolvePath(prefix, path)] = true
			} else if len(call.Args) < 2 || !isFallbackHandler(call.Args[1]) {
				bad(call.Pos())
			}
		case *ast.Ident:
			// The comment namespace's `mux.Handle(base, notFound())` fallback.
			if first.Name != "base" || len(call.Args) < 2 || !isFallbackHandler(call.Args[1]) {
				bad(call.Pos())
			}
		case *ast.BinaryExpr:
			method, suffix, usesBase := methodAndSuffix(first)
			if !usesBase {
				bad(call.Pos())
				return true
			}
			if method == "" {
				// `base + "/"` fallbacks carry no method.
				if len(call.Args) < 2 || !isFallbackHandler(call.Args[1]) {
					bad(call.Pos())
				}
				return true
			}
			// The two comment kinds share one registration closure, so each
			// method-suffix template is expanded for every segment; a phantom
			// key on either side fails the comparison above.
			for _, seg := range segments {
				out[method+" /api/v1/"+seg+"/{id}/comments"+suffix] = true
			}
		default:
			bad(call.Pos())
		}
		return true
	})
}

// methodAndSuffix walks a `"METHOD " + base [+ "…"]` concatenation: it
// returns the method (empty when the chain carries none) and the literal
// suffix after base.
func methodAndSuffix(expr ast.Expr) (method, suffix string, usesBase bool) {
	switch v := ast.Unparen(expr).(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", "", false
		}
		if m := methodSpacePattern.FindStringSubmatch(s); m != nil {
			return m[1], "", false
		}
		return "", s, false
	case *ast.Ident:
		return "", "", v.Name == "base"
	case *ast.BinaryExpr:
		lm, ls, lb := methodAndSuffix(v.X)
		rm, rs, rb := methodAndSuffix(v.Y)
		return lm + rm, ls + rs, lb || rb
	}
	return "", "", false
}

// commentSegments returns the segment literals passed to the comment
// namespace's `register(...)` closure.
func commentSegments(file *ast.File) []string {
	var segs []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := ast.Unparen(call.Fun).(*ast.Ident)
		if !ok || id.Name != "register" || len(call.Args) == 0 {
			return true
		}
		if lit, ok := ast.Unparen(call.Args[0]).(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil {
				segs = append(segs, s)
			}
		}
		return true
	})
	return segs
}

// isMuxHandleCall reports whether the call is a mux Handle/HandleFunc
// registration.
func isMuxHandleCall(call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Handle" || sel.Sel.Name == "HandleFunc"
}

// isFallbackHandler reports whether a method-less registration's handler is a
// JSON-404 fallback or a subtree mount — the two shapes allowed to carry no
// method.
func isFallbackHandler(arg ast.Expr) bool {
	call, ok := ast.Unparen(arg).(*ast.CallExpr)
	if !ok {
		return false
	}
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		return fun.Name == "notFound"
	case *ast.SelectorExpr:
		id, ok := fun.X.(*ast.Ident)
		return ok && id.Name == "http" && fun.Sel.Name == "StripPrefix"
	}
	return false
}

// splitMethodPattern splits a `"METHOD /path"` literal.
func splitMethodPattern(s string) (method, path string, ok bool) {
	i := strings.IndexByte(s, ' ')
	if i <= 0 || !strings.HasPrefix(s[i:], " /") {
		return "", "", false
	}
	method = s[:i]
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE":
		return method, s[i+1:], true
	}
	return "", "", false
}

// subtreePrefix maps a registration file to the namespace its stripped,
// relative patterns execute under.
func subtreePrefix(name string) string {
	switch name {
	case "auth.go":
		return "/api/v1/auth"
	case "users.go":
		return "/api/v1/users"
	}
	return ""
}

// resolvePath prefixes a stripped subtree registration; absolute paths pass
// through.
func resolvePath(prefix, path string) string {
	if prefix == "" || strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/health") || strings.HasPrefix(path, "/media") {
		return path
	}
	return prefix + path
}

// routesRepoRoot walks up to the directory holding go.mod.
func routesRepoRoot(t *testing.T) string {
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
