package codestyle

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// bannedPackageNames are the names docs/patterns/go/structure.md rule 3
// rejects: a package is named for what it owns, never for a bucket.
var bannedPackageNames = []string{
	"util", "utility", "common", "helper", "misc", "api", "types", "interfaces", "model", "testhelper",
}

// TestNoWriteStringConcatenation guards structure.md rule 20: a WriteString
// argument is a literal or a value, never a concatenation.
func TestNoWriteStringConcatenation(t *testing.T) {
	t.Parallel()

	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "WriteString" {
				return true
			}
			if bin, ok := ast.Unparen(call.Args[0]).(*ast.BinaryExpr); ok && bin.Op == token.ADD {
				out = append(out, fmt.Sprintf(
					"%s: WriteString with a concatenated argument (docs/patterns/go/structure.md rule 20)",
					fset.Position(call.Pos())))
			}
			return true
		})
		return out
	}

	selfCheck(t, check, `package p

func f(sb *strings.Builder, name string) { sb.WriteString("a" + name) }`, `package p

func f(sb *strings.Builder, name string) { fmt.Fprintf(sb, "a%s", name) }`)
	if got := checkSource(t, check, "snippet.go", `package p

func f(sb *strings.Builder, name string) { sb.WriteString(("a" + name)) }
`); len(got) == 0 {
		t.Fatal("check misses a parenthesized concatenation")
	}
	assertClean(t, check)
}

// TestNoEmptyInterfaceLiteral guards structure.md rule 10: write any, never
// interface{}.
func TestNoEmptyInterfaceLiteral(t *testing.T) {
	t.Parallel()

	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			it, ok := n.(*ast.InterfaceType)
			if ok && len(it.Methods.List) == 0 {
				out = append(out, fmt.Sprintf(
					"%s: empty interface literal — write any (docs/patterns/go/structure.md rule 10)",
					fset.Position(it.Pos())))
			}
			return true
		})
		return out
	}

	// The snippet below is the guard's own fixture; it is a string literal,
	// never a declaration in this package.
	selfCheck(t, check, `package p

var v interface{}
`, `package p

var v any
`)
	assertClean(t, check)
}

// TestNoBannedPackageNames guards structure.md rule 3: util, common, helper,
// model, testhelper and their kind are never package names.
func TestNoBannedPackageNames(t *testing.T) {
	t.Parallel()

	check := func(rel string, f *ast.File, _ *token.FileSet) []string {
		name := strings.TrimSuffix(f.Name.Name, "_test")
		if slices.Contains(bannedPackageNames, name) {
			return []string{fmt.Sprintf(
				"%s: package %s is a banned name (docs/patterns/go/structure.md rule 3)", rel, f.Name.Name)}
		}
		return nil
	}

	selfCheck(t, check, "package helper\n", "package stafflogs\n")
	assertClean(t, check)
}

// TestNoBlankOrDotImports guards structure.md rule 16: a blank import belongs
// to a main package or a test, and a dot import appears nowhere.
func TestNoBlankOrDotImports(t *testing.T) {
	t.Parallel()

	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		test := strings.HasSuffix(rel, "_test.go")
		var out []string
		for _, imp := range f.Imports {
			switch {
			case imp.Name == nil:
			case imp.Name.Name == ".":
				out = append(out, fmt.Sprintf(
					"%s: dot import %s (docs/patterns/go/structure.md rule 16)",
					fset.Position(imp.Pos()), imp.Path.Value))
			case imp.Name.Name == "_" && !test && f.Name.Name != "main":
				out = append(out, fmt.Sprintf(
					"%s: blank import %s in a library package (docs/patterns/go/structure.md rule 16)",
					fset.Position(imp.Pos()), imp.Path.Value))
			}
		}
		return out
	}

	selfCheck(t, check, `package store

import _ "example.com/driver"
`, `package main

import _ "example.com/driver"
`)
	if got := checkSource(t, check, "snippet_test.go", `package store

import _ "example.com/driver"
`); len(got) != 0 {
		t.Fatalf("a test file may carry a blank import, saw %v", got)
	}
	if got := checkSource(t, check, "snippet.go", `package store

import . "example.com/driver"
`); len(got) == 0 {
		t.Fatal("check misses a dot import in a main package")
	}
	assertClean(t, check)
}

// TestNoSortPackageImport guards docs/patterns/go/toolchain.md: the sort
// package is a replaced idiom, and slices and maps are the current form.
func TestNoSortPackageImport(t *testing.T) {
	t.Parallel()

	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		var out []string
		for _, imp := range f.Imports {
			if imp.Path.Value == `"sort"` {
				out = append(out, fmt.Sprintf(
					"%s: import of sort — use slices or maps (docs/patterns/go/toolchain.md)",
					fset.Position(imp.Pos())))
			}
		}
		return out
	}

	selfCheck(t, check, `package p

import "sort"
`, `package p

import "slices"
`)
	assertClean(t, check)
}

// TestNoPackageLevelSlogInHandler guards docs/patterns/go/logging.md rule 2:
// a handler logs through the request-scoped logger, never through the
// package-level slog functions that bypass it (including the process default).
func TestNoPackageLevelSlogInHandler(t *testing.T) {
	t.Parallel()

	levels := map[string]bool{
		"Debug": true, "Info": true, "Warn": true, "Error": true,
		"DebugContext": true, "InfoContext": true, "WarnContext": true, "ErrorContext": true,
		"Log": true, "LogAttrs": true,
		"Default": true, "SetDefault": true,
	}
	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		if !strings.HasPrefix(rel, "internal"+string(filepath.Separator)+"handler"+string(filepath.Separator)) ||
			strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		var localName string
		for name, path := range importNames(f) {
			if path == "log/slog" {
				localName = name
			}
		}
		if localName == "" {
			return nil
		}
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !levels[sel.Sel.Name] {
				return true
			}
			if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok && id.Name == localName {
				out = append(out, fmt.Sprintf(
					"%s: package-level slog.%s — log through logging.From(ctx) (docs/patterns/go/logging.md rule 2)",
					fset.Position(call.Pos()), sel.Sel.Name))
			}
			return true
		})
		return out
	}

	const name = "internal/handler/blog.go"
	if got := checkSource(t, check, name, `package handler

import "log/slog"

func f() { slog.Warn("bad request", "detail", "x") }
`); len(got) == 0 {
		t.Fatal("check misses a package-level slog call in internal/handler")
	}
	if got := checkSource(t, check, name, `package handler

import "log/slog"

func f() { slog.Default().Warn("bad request", "detail", "x") }
`); len(got) == 0 {
		t.Fatal("check misses a log written through the process default")
	}
	if got := checkSource(t, check, name, `package handler

import (
	"log/slog"

	"sick-fansubs/internal/logging"
)

func f(r *http.Request) {
	logger := logging.From(r.Context())
	logger.WarnContext(r.Context(), "bad request", "detail", "x")
}
`); len(got) != 0 {
		t.Fatalf("check flags the request-scoped logger: %v", got)
	}
	if got := checkSource(t, check, "internal/store/blog.go", `package store

import "log/slog"

func f() { slog.Warn("x") }
`); len(got) != 0 {
		t.Fatalf("check leaves packages outside internal/handler alone: %v", got)
	}
	assertClean(t, check)
}

// TestNoInlineInternalError guards docs/patterns/go/logging.md rule 5:
// writeInternalError is the only 500 path for JSON endpoints. A literal or
// named 500 handed to a response writer is flagged, while a non-writer call's
// integer argument and a plain comparison are not; the plain-text 500 body
// stays allowed only in the media asset path.
func TestNoInlineInternalError(t *testing.T) {
	t.Parallel()

	const responseFile = "internal/handler/response.go"
	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		if !strings.HasPrefix(rel, "internal"+string(filepath.Separator)+"handler"+string(filepath.Separator)) ||
			strings.HasSuffix(rel, "_test.go") || rel == responseFile {
			return nil
		}
		// An inline 500 is one written to a response: a named status constant
		// passed to a response writer or assigned for a later write, or a
		// literal 500 passed directly to a response writer. http.Error's
		// plain-text body is exempt only in media.go (logging.md rule 5), and
		// a comparison, a switch case, or a return value writes nothing.
		// isResponseWriterCall keeps unrelated integer arguments
		// (time.Sleep(500 * time.Millisecond)) out of the check.
		inlineUse := map[token.Pos]bool{}
		literalUse := map[token.Pos]bool{}
		isResponseWriterCall := func(call *ast.CallExpr) bool {
			switch fun := ast.Unparen(call.Fun).(type) {
			case *ast.SelectorExpr:
				if id, ok := fun.X.(*ast.Ident); ok && id.Name == "http" && fun.Sel.Name == "Error" {
					return true
				}
				return strings.HasPrefix(fun.Sel.Name, "write") || strings.HasPrefix(fun.Sel.Name, "Write")
			case *ast.Ident:
				return strings.HasPrefix(fun.Name, "write") || strings.HasPrefix(fun.Name, "Write")
			}
			return false
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CallExpr:
				if !isResponseWriterCall(v) {
					return true
				}
				sel, isSel := ast.Unparen(v.Fun).(*ast.SelectorExpr)
				if isSel {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "http" && sel.Sel.Name == "Error" &&
						rel == "internal"+string(filepath.Separator)+"handler"+string(filepath.Separator)+"media.go" {
						// The one plain-text 500 body (logging.md rule 5).
						return true
					}
				}
				for _, arg := range v.Args {
					a := ast.Unparen(arg)
					switch a.(type) {
					case *ast.BasicLit:
						literalUse[a.Pos()] = true
					case *ast.SelectorExpr:
						inlineUse[a.Pos()] = true
					}
				}
			case *ast.AssignStmt:
				for _, rhs := range v.Rhs {
					if _, ok := ast.Unparen(rhs).(*ast.SelectorExpr); ok {
						inlineUse[rhs.Pos()] = true
					}
				}
			}
			return true
		})
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BasicLit:
				if v.Kind == token.STRING && v.Value == `"/problems/internal-error"` {
					out = append(out, fmt.Sprintf(
						"%s: inline internal-error problem — call writeInternalError (docs/patterns/go/logging.md rule 5)",
						fset.Position(v.Pos())))
				}
				if v.Kind == token.INT && v.Value == "500" && literalUse[v.Pos()] {
					out = append(out, fmt.Sprintf(
						"%s: literal 500 passed to a response writer — call writeInternalError (docs/patterns/go/logging.md rule 5)",
						fset.Position(v.Pos())))
				}
			case *ast.SelectorExpr:
				if id, ok := v.X.(*ast.Ident); ok && id.Name == "http" && v.Sel.Name == "StatusInternalServerError" && inlineUse[v.Pos()] {
					out = append(out, fmt.Sprintf(
						"%s: inline 500 — call writeInternalError (docs/patterns/go/logging.md rule 5)",
						fset.Position(v.Pos())))
				}
			}
			return true
		})
		return out
	}

	const name = "internal/handler/blog.go"
	if got := checkSource(t, check, name, `package handler

func f(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusInternalServerError, "/problems/internal-error", "x")
}
`); len(got) == 0 {
		t.Fatal("check misses an inline internal-error problem")
	}
	if got := checkSource(t, check, name, `package handler

func f(w http.ResponseWriter, r *http.Request) {
	status := http.StatusInternalServerError
	writeProblem(w, r, status, "/problems/unsupported-media-type", "x")
}
`); len(got) == 0 {
		t.Fatal("check misses a 500 assigned for a later write")
	}
	if got := checkSource(t, check, name, `package handler

func f(w http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	writeInternalError(w, r, logger, "blog list query failed", "error", err)
}
`); len(got) != 0 {
		t.Fatalf("check flags the shared 500 helper: %v", got)
	}
	if got := checkSource(t, check, "internal/handler/media.go", `package handler

func f(w http.ResponseWriter) { http.Error(w, "internal server error", http.StatusInternalServerError) }
`); len(got) != 0 {
		t.Fatalf("the plain-text 500 body stays allowed: %v", got)
	}
	if got := checkSource(t, check, "internal/handler/media.go", `package handler

func f(w http.ResponseWriter, r *http.Request) {
	problem.Write(w, r, http.StatusInternalServerError, "req", "/problems/internal-error", "x")
}
`); len(got) == 0 {
		t.Fatal("a new inline 500 in media.go must still be flagged")
	}
	if got := checkSource(t, check, name, `package handler

func f(w http.ResponseWriter, r *http.Request, err error) {
	if writeStoreError(w, r, err, "blog update") == http.StatusInternalServerError {
		auditFailure(r)
	}
}
`); len(got) != 0 {
		t.Fatalf("a status comparison writes nothing and must stay clean: %v", got)
	}
	if got := checkSource(t, check, name, `package handler

func f(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, 500, "/problems/unsupported-media-type", "x")
}
`); len(got) == 0 {
		t.Fatal("check misses a literal 500 written inline")
	}
	if got := checkSource(t, check, name, `package handler

func f(code int, r *http.Request) {
	if code == 500 {
		auditFailure(r)
	}
}
`); len(got) != 0 {
		t.Fatalf("a literal status comparison writes nothing and must stay clean: %v", got)
	}
	if got := checkSource(t, check, name, `package handler

func f(d time.Duration) {
	time.Sleep(500 * time.Millisecond)
	truncate("x", 500)
}
`); len(got) != 0 {
		t.Fatalf("a non-writer call's integer argument must stay clean: %v", got)
	}
	if got := checkSource(t, check, name, `package handler

func f() {
	budget := 500
	_ = budget
}
`); len(got) != 0 {
		t.Fatalf("a literal assigned to a non-status variable must stay clean: %v", got)
	}
	if got := checkSource(t, check, name, `package handler

func f(w http.ResponseWriter) { http.Error(w, "oops", 500) }
`); len(got) == 0 {
		t.Fatal("an http.Error 500 outside the media asset path must be flagged")
	}
	assertClean(t, check)
}

// slogBuiltinKeys are the record's own keys (docs/patterns/go/logging.md rule
// 4): reusing one as an attribute key would write the field twice, and a JSON
// sink keeps only one of them.
var slogBuiltinKeys = []string{"time", "level", "msg", "source"}

// TestNoSlogBuiltinKeys guards docs/patterns/go/logging.md rule 4: an attribute
// key is never one of the record's own keys.
func TestNoSlogBuiltinKeys(t *testing.T) {
	t.Parallel()

	// keyStarts maps a logging call to the argument index of its first attribute
	// key: the alternating key-value pairs begin after the fixed arguments. A
	// method name is matched whatever its receiver, so an alias or a wrapped
	// logger cannot dodge the guard. LogAttrs carries Attr values, so its keys
	// are caught through the constructors instead; Group's first argument
	// qualifies its keys rather than naming one, so it is out of scope.
	keyStarts := map[string]int{
		"With": 0,
		"Info": 1, "Warn": 1, "Error": 1, "Debug": 1,
		"InfoContext": 2, "WarnContext": 2, "ErrorContext": 2, "DebugContext": 2,
		"Log": 3,
	}
	attrConstructors := map[string]bool{
		"String": true, "Int": true, "Int64": true, "Uint64": true, "Float64": true,
		"Bool": true, "Duration": true, "Time": true, "Any": true,
	}

	check := func(_ string, f *ast.File, fset *token.FileSet) []string {
		var slogName string
		for name, path := range importNames(f) {
			if path == "log/slog" {
				slogName = name
			}
		}
		var out []string
		flag := func(arg ast.Expr) {
			lit, ok := arg.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil || !slices.Contains(slogBuiltinKeys, key) {
				return
			}
			out = append(out, fmt.Sprintf(
				"%s: attribute key %s is a record built-in — pick another key (docs/patterns/go/logging.md rule 4)",
				fset.Position(arg.Pos()), lit.Value))
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if start, ok := keyStarts[sel.Sel.Name]; ok {
				for i := start; i < len(call.Args); i += 2 {
					flag(call.Args[i])
				}
				return true
			}
			id, ok := ast.Unparen(sel.X).(*ast.Ident)
			if ok && slogName != "" && id.Name == slogName && attrConstructors[sel.Sel.Name] && len(call.Args) > 0 {
				flag(call.Args[0])
			}
			return true
		})
		return out
	}

	selfCheck(t, check, `package p

func f(logger *slog.Logger) { logger.Info("started", "level", "INFO") }
`, `package p

func f(logger *slog.Logger) { logger.Info("started", "total", 3) }
`)
	if got := checkSource(t, check, "snippet.go", `package p

import "log/slog"

func f() { slog.String("msg", "x") }
`); len(got) == 0 {
		t.Fatal("check misses a built-in key in an attribute constructor")
	}
	if got := checkSource(t, check, "snippet.go", `package p

func f(logger *slog.Logger) { logger.InfoContext(nil, "started", "detail", "time") }
`); len(got) != 0 {
		t.Fatalf("check flags a built-in word in a VALUE position: %v", got)
	}
	assertClean(t, check)
}

// TestNoErrorsAs guards docs/patterns/go/toolchain.md: errors.AsType is the
// current form, whatever local name the errors package is imported under.
func TestNoErrorsAs(t *testing.T) {
	t.Parallel()

	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		var localName string
		for name, path := range importNames(f) {
			if path == "errors" {
				localName = name
			}
		}
		if localName == "" {
			return nil
		}
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "As" {
				return true
			}
			if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok && id.Name == localName {
				out = append(out, fmt.Sprintf(
					"%s: errors.As — use errors.AsType (docs/patterns/go/toolchain.md)",
					fset.Position(call.Pos())))
			}
			return true
		})
		return out
	}

	selfCheck(t, check, `package p

import "errors"

func f(err error) bool {
	var n *strconv.NumError
	return errors.As(err, &n)
}
`, `package p

import "errors"

func f(err error) bool {
	_, ok := errors.AsType[*strconv.NumError](err)
	return ok
}
`)
	if got := checkSource(t, check, "snippet.go", `package p

import stderrors "errors"

func f(err error) bool {
	var n *strconv.NumError
	return stderrors.As(err, &n)
}
`); len(got) == 0 {
		t.Fatal("check misses errors.As under an import alias")
	}
	assertClean(t, check)
}

// TestNoSentinelEquality guards docs/patterns/go/errors.md rule 3: an error
// is tested with errors.Is, never with == or !=. nil is the one valid
// comparison operand.
func TestNoSentinelEquality(t *testing.T) {
	t.Parallel()

	sentinelLike := func(e ast.Expr) bool {
		sentinelName := func(name string) bool {
			if strings.HasPrefix(name, "Err") {
				return true
			}
			return strings.HasPrefix(name, "err") && len(name) > 3 && name[3] >= 'A' && name[3] <= 'Z'
		}
		switch v := ast.Unparen(e).(type) {
		case *ast.SelectorExpr:
			return sentinelName(v.Sel.Name)
		case *ast.Ident:
			return sentinelName(v.Name)
		}
		return false
	}
	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			bin, ok := n.(*ast.BinaryExpr)
			if !ok || (bin.Op != token.EQL && bin.Op != token.NEQ) {
				return true
			}
			if sentinelLike(bin.X) || sentinelLike(bin.Y) {
				out = append(out, fmt.Sprintf(
					"%s: sentinel compared with %s — use errors.Is (docs/patterns/go/errors.md rule 3)",
					fset.Position(bin.Pos()), bin.Op))
			}
			return true
		})
		return out
	}

	selfCheck(t, check, `package p

func f(err error) bool { return err == store.ErrNotFound }`, `package p

func f(err error) bool { return errors.Is(err, store.ErrNotFound) }`)
	if got := checkSource(t, check, "snippet.go", `package p

func f(err error) bool { return err == nil }
`); len(got) != 0 {
		t.Fatalf("check flags a nil comparison: %v", got)
	}
	if got := checkSource(t, check, "snippet.go", `package p

func f(err error) bool { return errVerifierUnusable != err }
`); len(got) == 0 {
		t.Fatal("check misses an unexported sentinel")
	}
	assertClean(t, check)
}

// TestDecodeTextMatchConfined guards docs/patterns/go/errors.md rule 8: the
// encoding/json unknown-field message is the tree's one decode-error text
// match, and it lives in internal/handler/errors.go beside its pinning test.
func TestDecodeTextMatchConfined(t *testing.T) {
	t.Parallel()

	const allowed = "internal" + string(filepath.Separator) + "handler" + string(filepath.Separator) + "errors.go"
	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		if rel == allowed {
			return nil
		}
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Contains" {
				return true
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// Only an error-text match qualifies; a content-type assertion such
			// as strings.Contains(ct, "application/problem+json") does not.
			errCall, ok := ast.Unparen(call.Args[0]).(*ast.CallExpr)
			if !ok {
				return true
			}
			errSel, ok := errCall.Fun.(*ast.SelectorExpr)
			if !ok || errSel.Sel.Name != "Error" {
				return true
			}
			needle, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			// The needles that identify an encoding/json decode-error message,
			// across the field names and the wording the v2-backed text uses.
			lower := strings.ToLower(needle)
			if strings.Contains(lower, "json") || strings.Contains(needle, "unknown field") ||
				strings.Contains(lower, "unknown member") || strings.Contains(lower, "cannot unmarshal") ||
				strings.Contains(lower, "unexpected end") {
				out = append(out, fmt.Sprintf(
					"%s: decode-error text match outside the classifier — classify by type (docs/patterns/go/errors.md rule 8)",
					fset.Position(call.Pos())))
			}
			return true
		})
		return out
	}

	selfCheck(t, check, `package p

func f(err error) bool { return strings.Contains(err.Error(), "unknown field") }`, `package p

func f(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }`)
	if got := checkSource(t, check, allowed, `package handler

func f(err error) bool { return strings.Contains(err.Error(), "unknown field") }
`); len(got) != 0 {
		t.Fatalf("the classifier file must stay exempt: %v", got)
	}
	assertClean(t, check)
}

// TestMiddlewareChainConfined guards docs/patterns/go/route-chains.md rule 1:
// internal/routes/chains.go is the only file that wraps a handler with
// middleware; the pattern doc records why a test file may compose.
func TestMiddlewareChainConfined(t *testing.T) {
	t.Parallel()

	const chainsFile = "internal" + string(filepath.Separator) + "routes" + string(filepath.Separator) + "chains.go"
	wrappers := handlerWrappers(t)
	if len(wrappers) < 5 {
		t.Fatalf("derived %d handler wrappers from internal/middleware — the derivation is broken", len(wrappers))
	}

	// scan reports every use of a wrapping constructor in a buildable file,
	// however it is written: a call, a function value, a parenthesized
	// selector.
	scan := func(rel string, f *ast.File, fset *token.FileSet) []string {
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		var localName string
		for name, path := range importNames(f) {
			if path == "sick-fansubs/internal/middleware" {
				localName = name
			}
		}
		if localName == "" {
			return nil
		}
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || !wrappers[sel.Sel.Name] {
				return true
			}
			if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok && id.Name == localName {
				out = append(out, fmt.Sprintf(
					"%s: middleware.%s wraps a handler — name a chain from chains.go (docs/patterns/go/route-chains.md rule 1)",
					fset.Position(sel.Pos()), sel.Sel.Name))
			}
			return true
		})
		return out
	}

	selfCheck(t, scan, `package routes

import "sick-fansubs/internal/middleware"

func f(h http.Handler) http.Handler { return middleware.CSRF()(h) }`, `package routes

func f(h http.Handler) http.Handler { return publicChain(h) }`)
	if got := checkSource(t, scan, "snippet.go", `package routes

import mw "sick-fansubs/internal/middleware"

func f(h http.Handler) http.Handler { w := mw.CSRF; return w(h) }
`); len(got) == 0 {
		t.Fatal("check misses a wrapper taken as a function value under an aliased import")
	}

	// The control run scans the whole tree unchanged: the findings must be
	// non-empty, all of them in chains.go, and every derived wrapper must
	// appear — so the guard cannot pass by matching nothing or by falling
	// behind the code.
	seen := map[string]bool{}
	findings := scanRepo(t, scan)
	if len(findings) == 0 {
		t.Fatal("no middleware composition found in the tree — the guard would pass vacuously")
	}
	for _, finding := range findings {
		if !strings.Contains(finding, chainsFile) {
			t.Errorf("middleware composition outside %s: %s", chainsFile, finding)
		}
		for name := range wrappers {
			if strings.Contains(finding, "middleware."+name) {
				seen[name] = true
			}
		}
	}
	for name := range wrappers {
		if !seen[name] {
			t.Errorf("chains.go never wraps middleware.%s — a wrapper belongs to a chain (docs/patterns/go/route-chains.md rule 1)", name)
		}
	}
}

// handlerWrappers returns the names of the middleware package's exported
// functions that wrap a handler — a result of the func(http.Handler)
// http.Handler shape, written out or named. Deriving the set from the source
// keeps the confinement guard in step with the package it polices.
func handlerWrappers(t *testing.T) map[string]bool {
	t.Helper()

	prefix := "internal" + string(filepath.Separator) + "middleware" + string(filepath.Separator)
	inScope := func(rel string) bool {
		return strings.HasPrefix(rel, prefix) && !strings.HasSuffix(rel, "_test.go")
	}

	// Pass one: the package-local type names whose underlying type wraps a
	// handler, so a wrapper declared through a named type cannot hide.
	named := map[string]bool{}
	walkGoFiles(t, func(rel string, f *ast.File, _ *token.FileSet) {
		if !inScope(rel) {
			return
		}
		httpName := httpImportName(f)
		if httpName == "" {
			return
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && handlerWrapper(httpName, ts.Type) {
					named[ts.Name.Name] = true
				}
			}
		}
	})

	// Pass two: the exported functions returning one of those shapes.
	wrappers := map[string]bool{}
	walkGoFiles(t, func(rel string, f *ast.File, _ *token.FileSet) {
		if !inScope(rel) {
			return
		}
		httpName := httpImportName(f)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !ast.IsExported(fn.Name.Name) {
				continue
			}
			if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
				continue
			}
			result := ast.Unparen(fn.Type.Results.List[0].Type)
			if httpName != "" && handlerWrapper(httpName, result) {
				wrappers[fn.Name.Name] = true
			}
			if id, ok := result.(*ast.Ident); ok && named[id.Name] {
				wrappers[fn.Name.Name] = true
			}
		}
	})
	return wrappers
}

// TestCollectionEnvelopeConfined guards docs/patterns/go/collections.md rule 1:
// internal/handler/collection.go is the only file that declares or builds the
// keyset envelope; every collection answers through writeKeysetPage.
func TestCollectionEnvelopeConfined(t *testing.T) {
	t.Parallel()

	const collectionFile = "internal" + string(filepath.Separator) + "handler" + string(filepath.Separator) + "collection.go"

	// baseTypeName unwraps parentheses and type arguments, so collection[T]
	// and (collection[Item]) both answer "collection".
	var baseTypeName func(ast.Expr) string
	baseTypeName = func(e ast.Expr) string {
		switch v := ast.Unparen(e).(type) {
		case *ast.Ident:
			return v.Name
		case *ast.IndexExpr:
			return baseTypeName(v.X)
		case *ast.IndexListExpr:
			return baseTypeName(v.X)
		default:
			return ""
		}
	}

	// jsonName returns the name a field marshals under, or "".
	jsonName := func(f *ast.Field) string {
		if f.Tag == nil {
			return ""
		}
		tag, err := strconv.Unquote(f.Tag.Value)
		if err != nil {
			return ""
		}
		name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
		return name
	}

	// An envelope field carries one of the three wire keys — by Go name or by
	// JSON tag — or is the pageInfo type itself, embedded.
	envelopeField := func(f *ast.Field) bool {
		switch jsonName(f) {
		case "pageInfo", "hasNextPage", "endCursor":
			return true
		}
		if baseTypeName(f.Type) == "pageInfo" || baseTypeName(f.Type) == "collection" {
			return true
		}
		for _, name := range f.Names {
			switch name.Name {
			case "PageInfo", "HasNextPage", "EndCursor":
				return true
			}
		}
		return false
	}

	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		// A test file may decode the envelope; the collection file declares it.
		if strings.HasSuffix(rel, "_test.go") || rel == collectionFile {
			return nil
		}
		var out []string
		finding := func(pos token.Pos, what string) {
			out = append(out, fmt.Sprintf(
				"%s: %s outside %s — answer through writeKeysetPage (docs/patterns/go/collections.md rule 1)",
				fset.Position(pos), what, collectionFile))
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.StructType:
				for _, field := range v.Fields.List {
					if envelopeField(field) {
						finding(field.Pos(), "collection envelope field")
					}
				}
			case *ast.CompositeLit:
				if name := baseTypeName(v.Type); name == "collection" || name == "pageInfo" {
					finding(v.Pos(), "a built "+name)
				}
			case *ast.KeyValueExpr:
				// A map payload can carry the envelope without a struct.
				if lit, ok := v.Key.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if key, err := strconv.Unquote(lit.Value); err == nil && key == "pageInfo" {
						finding(v.Pos(), "a pageInfo map key")
					}
				}
			}
			return true
		})
		return out
	}

	// The bad fixtures exercise every branch: the shape under its own names, the
	// shape under other names but the wire tags, a map envelope, and a direct
	// collection literal.
	byName := "package handler\n\n" +
		"type blogList struct {\n" +
		"\tItems    []string `json:\"items\"`\n" +
		"\tPageInfo struct {\n" +
		"\t\tHasNextPage bool    `json:\"hasNextPage\"`\n" +
		"\t\tEndCursor   *string `json:\"endCursor\"`\n" +
		"\t} `json:\"pageInfo\"`\n" +
		"}"
	byTag := "package handler\n\n" +
		"type x struct {\n" +
		"\tMore bool    `json:\"hasNextPage\"`\n" +
		"\tCur  *string `json:\"endCursor,omitempty\"`\n" +
		"}"
	byMap := "package handler\n\n" +
		"func f(w http.ResponseWriter, r *http.Request, items []any) {\n" +
		"\twriteJSON(w, r, 200, map[string]any{\"items\": items, \"pageInfo\": map[string]any{}})\n" +
		"}"
	byLiteral := "package handler\n\n" +
		"var page = collection[string]{Items: []string{}}"
	good := "package handler\n\n" +
		"func f(w http.ResponseWriter, r *http.Request) {\n" +
		"\twriteKeysetPage(w, r, \"x\", keysetPage[Row, Item]{})\n" +
		"\twriteJSON(w, r, 200, staffLogs{Items: nil})\n" +
		"}"
	for _, bad := range []string{byName, byTag, byMap, byLiteral} {
		selfCheck(t, check, bad, good)
	}

	// The control runs the check against the real declaration with the
	// exemption lifted, so a check that stopped matching the envelope fails.
	src, err := os.ReadFile(filepath.Join(repoRoot(t), collectionFile))
	if err != nil {
		t.Fatalf("read %s: %v", collectionFile, err)
	}
	if got := checkSource(t, check, "snippet.go", string(src)); len(got) == 0 {
		t.Fatal("the envelope declaration in collection.go is not recognized — the guard would pass vacuously")
	}
	assertClean(t, check)
}

// TestIDMintingConfined guards docs/patterns/go/ids.md rule 1: internal/id is
// the tree's only identifier minter. The minting idiom's import pair —
// crypto/rand plus encoding/hex — is the checkable proxy.
func TestIDMintingConfined(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		filepath.Join("internal", "id", "id.go"):                   true,
		filepath.Join("internal", "database", "backup_restore.go"): true, // unpredictable names and digests (randomHex)
		filepath.Join("internal", "media", "backup.go"):            true, // temp names and SHA-256 hex (randomSuffix, digests)
	}

	check := func(rel string, f *ast.File, _ *token.FileSet) []string {
		// A test fixture may mint its own bytes; the rule polices production code.
		if allowed[rel] || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		var hasRand, hasHex bool
		for _, path := range importNames(f) {
			switch path {
			case "crypto/rand":
				hasRand = true
			case "encoding/hex":
				hasHex = true
			}
		}
		if !hasRand || !hasHex {
			return nil
		}
		return []string{fmt.Sprintf(
			"%s: mints an identifier outside internal/id — call id.New (docs/patterns/go/ids.md rule 1)", rel)}
	}

	selfCheck(t, check, `package x

import (
	"crypto/rand"
	"encoding/hex"
)

func mint() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}`, `package x

import "sick-fansubs/internal/id"

func mint() (string, error) { return id.New() }`)

	// The control runs against the real minter with its exemption lifted, so a
	// check that stopped matching the minting import pair fails.
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "id", "id.go"))
	if err != nil {
		t.Fatalf("read internal/id/id.go: %v", err)
	}
	if got := checkSource(t, check, "snippet.go", string(src)); len(got) == 0 {
		t.Fatal("internal/id/id.go is not recognized as the minter — the guard would pass vacuously")
	}
	assertClean(t, check)
}

// TestStoreDoesNotImportMedia guards docs/patterns/go/ids.md rule 8: the store
// mints through internal/id, and media is storage and processing code, not an
// identity source for the store to depend on.
func TestStoreDoesNotImportMedia(t *testing.T) {
	t.Parallel()

	storePrefix := "internal" + string(filepath.Separator) + "store" + string(filepath.Separator)
	storeFile := filepath.Join("internal", "store", "blog_store.go")

	check := func(rel string, f *ast.File, _ *token.FileSet) []string {
		if !strings.HasPrefix(rel, storePrefix) {
			return nil
		}
		for _, path := range importNames(f) {
			if path == "sick-fansubs/internal/media" {
				return []string{fmt.Sprintf(
					"%s: internal/store imports internal/media — mint through id.New (docs/patterns/go/ids.md rule 8)", rel)}
			}
		}
		return nil
	}

	bad := `package store

import "sick-fansubs/internal/media"

var _ = media.RelativePath`
	good := `package store

import "sick-fansubs/internal/id"

var _ = id.New`
	if got := checkSource(t, check, storeFile, bad); len(got) == 0 {
		t.Fatal("check misses a store file importing internal/media")
	}
	if got := checkSource(t, check, storeFile, good); len(got) != 0 {
		t.Fatalf("check flags a clean store file: %v", got)
	}
	if got := checkSource(t, check, filepath.Join("internal", "handler", "media.go"), bad); len(got) != 0 {
		t.Fatalf("check flags a non-store file: %v", got)
	}

	// The control counts the real store subtree the check polices, so a rename
	// cannot leave it scanning nothing.
	var seen int
	walkGoFiles(t, func(rel string, _ *ast.File, _ *token.FileSet) {
		if strings.HasPrefix(rel, storePrefix) {
			seen++
		}
	})
	if seen < 10 {
		t.Fatalf("check scanned %d store files, want at least 10", seen)
	}
	assertClean(t, check)
}

// TestDriverNameAndDSNConfined guards docs/patterns/go/sqlite.md rules 1 and 3:
// the registered driver name "sqlite3" appears nowhere — opening binds the
// connector — and DSN pragmas are built only inside internal/database.
func TestDriverNameAndDSNConfined(t *testing.T) {
	t.Parallel()

	dbPrefix := "internal" + string(filepath.Separator) + "database" + string(filepath.Separator)
	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		// A test file may name a banned shape on purpose (the fixtures here do);
		// the rule polices production code.
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if strings.Contains(val, "sqlite3") {
				out = append(out, fmt.Sprintf(
					"%s: the registered driver name — open through the connector (docs/patterns/go/sqlite.md rule 1)",
					fset.Position(lit.Pos())))
				return true
			}
			if strings.Contains(val, "_pragma") && !strings.HasPrefix(rel, dbPrefix) {
				out = append(out, fmt.Sprintf(
					"%s: DSN pragmas are built in internal/database (docs/patterns/go/sqlite.md rule 3)",
					fset.Position(lit.Pos())))
			}
			return true
		})
		return out
	}

	selfCheck(t, check, `package x

import "database/sql"

func open() (*sql.DB, error) { return sql.Open("sqlite3", "file:x.db") }`, `package x

import "database/sql"

func ping(db *sql.DB) error { return db.Ping() }`)
	if got := checkSource(t, check, "snippet.go", `package x

const dsn = "file:x.db?_pragma=foreign_keys(ON)"`); len(got) == 0 {
		t.Fatal("check misses a DSN pragma outside internal/database")
	}
	if got := checkSource(t, check, filepath.Join("internal", "database", "backup.go"), `package database

const dsn = "file:x.db?_pragma=foreign_keys(ON)"`); len(got) != 0 {
		t.Fatalf("check flags the database package's own DSN: %v", got)
	}

	// The control runs against the real DSN builder with its exemption lifted,
	// so a check that stopped matching the pragma text fails.
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "database", "backup_restore.go"))
	if err != nil {
		t.Fatalf("read internal/database/backup_restore.go: %v", err)
	}
	if got := checkSource(t, check, "snippet.go", string(src)); len(got) == 0 {
		t.Fatal("the real DSN builder is not recognized — the guard would pass vacuously")
	}
	assertClean(t, check)
}

// TestNullFamilyConfined guards docs/patterns/go/sql-mapping.md rule 5: the
// write/read null* helpers are declared once, in internal/store/null.go.
func TestNullFamilyConfined(t *testing.T) {
	t.Parallel()

	nullFile := "internal" + string(filepath.Separator) + "store" + string(filepath.Separator) + "null.go"
	family := map[string]bool{"nullableString": true, "nullStringPtr": true, "nullStringValue": true}
	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		if rel == nullFile {
			return nil
		}
		var out []string
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !family[fn.Name.Name] {
				continue
			}
			out = append(out, fmt.Sprintf(
				"%s: %s is declared outside %s — one null family, one file (docs/patterns/go/sql-mapping.md rule 5)",
				fset.Position(fn.Pos()), fn.Name.Name, nullFile))
		}
		return out
	}

	selfCheck(t, check, `package store

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}`, `package store

import "sick-fansubs/internal/id"

func f() (string, error) { return id.New() }`)

	// The control runs against the real declaration with its exemption lifted,
	// so a check that stopped matching the family fails.
	src, err := os.ReadFile(filepath.Join(repoRoot(t), nullFile))
	if err != nil {
		t.Fatalf("read %s: %v", nullFile, err)
	}
	if got := checkSource(t, check, "snippet.go", string(src)); len(got) == 0 {
		t.Fatal("the null family in null.go is not recognized — the guard would pass vacuously")
	}
	assertClean(t, check)
}

// httpImportName returns the local name of net/http in f, or "" when the file
// does not import it.
func httpImportName(f *ast.File) string {
	for name, path := range importNames(f) {
		if path == "net/http" {
			return name
		}
	}
	return ""
}

// handlerWrapper reports whether typ is the func(http.Handler) http.Handler
// shape that wraps one handler in another.
func handlerWrapper(httpName string, typ ast.Expr) bool {
	ft, ok := ast.Unparen(typ).(*ast.FuncType)
	if !ok || ft.Params == nil || len(ft.Params.List) != 1 ||
		ft.Results == nil || len(ft.Results.List) != 1 {
		return false
	}
	isHandler := func(e ast.Expr) bool {
		sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := ast.Unparen(sel.X).(*ast.Ident)
		return ok && id.Name == httpName && sel.Sel.Name == "Handler"
	}
	return isHandler(ft.Params.List[0].Type) && isHandler(ft.Results.List[0].Type)
}

// TestContentKindTablesConfined guards docs/patterns/go/content-kinds.md
// rule 1: the per-content-kind table names and kind strings live once, in
// internal/store/content_kinds.go, as string literals.
func TestContentKindTablesConfined(t *testing.T) {
	t.Parallel()

	storePrefix := "internal" + string(filepath.Separator) + "store" + string(filepath.Separator)
	kindsFile := storePrefix + "content_kinds.go"
	storeFile := storePrefix + "blog_store.go"

	// SQLite identifiers are case-insensitive, and a name is only meaningful
	// as a whole word, so the match is both.
	needle := regexp.MustCompile(`(?i)\b(blog_posts|projects|blog_post_comments|project_comments|blog_post_comment_hearts|project_comment_hearts|blog_post_favorites|project_favorites|blog_post_downloads|project_downloads|blog_post_search|project_search|blog-posts)\b`)

	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		// Only the store owns the descriptors, and a test file may seed
		// fixtures with raw names; the rule polices production store code.
		if !strings.HasPrefix(rel, storePrefix) || rel == kindsFile || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		var out []string
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if m := needle.FindString(val); m != "" {
				out = append(out, fmt.Sprintf(
					"%s: %q names a content kind directly — read it from the ContentKind descriptor (docs/patterns/go/content-kinds.md rule 1)",
					fset.Position(lit.Pos()), m))
			}
			return true
		})
		return out
	}

	bad := `package store

const q = "SELECT COUNT(*) FROM blog_posts"`
	good := `package store

const q = "SELECT COUNT(*) FROM " + BlogContent.table`
	if got := checkSource(t, check, storeFile, bad); len(got) == 0 {
		t.Fatal("check misses a store file naming a content table directly")
	}
	if got := checkSource(t, check, storeFile, good); len(got) != 0 {
		t.Fatalf("check flags a clean store file: %v", got)
	}
	if got := checkSource(t, check, kindsFile, bad); len(got) != 0 {
		t.Fatalf("check flags the descriptor file: %v", got)
	}
	if got := checkSource(t, check, storePrefix+"blog_store_test.go", bad); len(got) != 0 {
		t.Fatalf("check flags a store test file: %v", got)
	}
	if got := checkSource(t, check, filepath.Join("internal", "migration", "import.go"), bad); len(got) != 0 {
		t.Fatalf("check flags a non-store package: %v", got)
	}

	// The control runs against the real descriptor file with its exemption
	// lifted, so a check that stopped matching the names fails.
	src, err := os.ReadFile(filepath.Join(repoRoot(t), kindsFile))
	if err != nil {
		t.Fatalf("read %s: %v", kindsFile, err)
	}
	if got := checkSource(t, check, storeFile, string(src)); len(got) == 0 {
		t.Fatal("content_kinds.go is not recognized as the descriptor home — the guard would pass vacuously")
	}
	assertClean(t, check)
}

// TestProductionFileSizeBounded guards docs/patterns/go/structure.md rule 4: a
// production file that passes 500 lines is split by subject. The count is the
// parsed file's last line, and a _test.go file is exempt.
func TestProductionFileSizeBounded(t *testing.T) {
	t.Parallel()

	const maxProductionFileLines = 500

	check := func(rel string, f *ast.File, fset *token.FileSet) []string {
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		// Count the last token and any trailing comment lines, so a file cannot
		// hide its length behind a floating comment block.
		lines := fset.Position(f.End()).Line
		for _, cg := range f.Comments {
			if end := fset.Position(cg.End()).Line; end > lines {
				lines = end
			}
		}
		if lines > maxProductionFileLines {
			return []string{fmt.Sprintf(
				"%s: %d lines — split it by subject (docs/patterns/go/structure.md rule 4)", rel, lines)}
		}
		return nil
	}

	short := "package p\n\nvar x = 1"
	long := "package p\n\nvar x = []int{\n" + strings.Repeat("\t0,\n", maxProductionFileLines) + "}"
	selfCheck(t, check, long, short)

	// The control counts the production files the check polices, so a walk that
	// sees nothing cannot pass vacuously.
	var seen int
	walkGoFiles(t, func(rel string, _ *ast.File, _ *token.FileSet) {
		if !strings.HasSuffix(rel, "_test.go") {
			seen++
		}
	})
	if seen < 100 {
		t.Fatalf("check scanned %d production files, want at least 100", seen)
	}
	assertClean(t, check)
}
