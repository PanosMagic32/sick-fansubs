package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

//go:embed web/dist/*
var frontendFS embed.FS

// frontendHandler returns an http.Handler that serves the embedded Lit
// production build from web/dist/. Handles SPA (Single Page Application)
// fallback: unknown paths serve index.html for client-side routing, while a
// missing /assets/* file answers 404 — the fallback serves routes, not a
// broken deployment.
//
// PWA special cases: /manifest.json, /sw.js, and /icons/* are
// served no-cache before the immutable hashed-asset branch, and sw.js
// receives the same serve-time version injection as index.html.
//
// During development, the dist/ directory is empty and this handler
// returns 503. Vite serves the frontend separately and proxies API
// calls to Go — so / and /health routes hit different servers.
//
// Security headers (CSP, X-Frame-Options, etc.) and the no-cache revalidation
// directive are set on HTML responses only; hashed assets receive immutable
// cache headers.
// The version placeholder (default "dev") in index.html is replaced with the
// actual application version at serve time (ldflags, defaults to "dev").
func frontendHandler(version string) http.Handler {
	h, err := newFrontendHandler(frontendFS, version)
	if err != nil {
		slog.Error("frontend not embedded (build with 'make web-build' first)", "error", err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Frontend not built", http.StatusServiceUnavailable)
		})
	}
	return h
}

// newFrontendHandler is the testable seam: identical behavior to
// frontendHandler over any fs.FS carrying a web/dist layout (tests use
// fstest.MapFS so the embedded build state cannot affect them).
func newFrontendHandler(fsys fs.FS, version string) (http.Handler, error) {
	sub, err := fs.Sub(fsys, "web/dist")
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServer(http.FS(sub))

	// Read the embedded index.html once and cache the processed version.
	indexBytes, readErr := fs.ReadFile(sub, "index.html")
	var cachedIndex []byte
	if readErr == nil {
		cachedIndex = []byte(strings.ReplaceAll(string(indexBytes), `content="dev"`, `content="`+version+`"`))
	} else {
		slog.Error("embedded index.html read failed", "error", readErr)
	}

	// sw.js gets the same serve-time version injection:
	// the SF_VERSION placeholder becomes the deployed version, so each
	// release ships a byte-different worker and browsers install it on
	// their own conservative schedule (no build plumbing).
	swBytes, swErr := fs.ReadFile(sub, "sw.js")
	var cachedSW []byte
	if swErr == nil {
		cachedSW = []byte(strings.ReplaceAll(string(swBytes), `SF_VERSION = "dev"`, `SF_VERSION = "`+version+`"`))
	} else {
		slog.Error("embedded sw.js read failed", "error", swErr)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")

		// serveIndex writes the processed index.html with the security
		// headers and the no-cache directive: every navigation revalidates,
		// so a deploy's new asset hashes are always seen.
		serveIndex := func() {
			if cachedIndex == nil {
				http.Error(w, "Frontend index not available", http.StatusServiceUnavailable)
				return
			}
			setSecurityHeaders(w)
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(cachedIndex)
		}

		// Serve the processed index.html.
		if path == "" || path == "index.html" {
			serveIndex()
			return
		}

		// PWA special cases — these three paths are fetched with
		// regular caching semantics, so the immutable header the generic
		// branch applies to every embedded file would let them go stale
		// across deploys. sw.js itself is version-injected above.
		switch {
		case path == "sw.js":
			if cachedSW == nil {
				http.Error(w, "Service worker not available", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.Write(cachedSW)
			return
		case path == "manifest.json" || strings.HasPrefix(path, "icons/"):
			w.Header().Set("Cache-Control", "no-cache")
			fileServer.ServeHTTP(w, r)
			return
		}

		f, err := sub.Open(path)
		if err == nil {
			f.Close()
			// Hashed asset — immutable cache.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
			return
		}

		// A miss under the hashed-asset prefix is a broken deployment, not a
		// route: answer 404 instead of letting the SPA fallback mask it.
		if strings.HasPrefix(path, "assets/") {
			http.NotFound(w, r)
			return
		}

		// SPA fallback: serve the processed index.html.
		serveIndex()
	}), nil
}

// setSecurityHeaders writes production security headers for HTML responses.
//
// style-src 'unsafe-inline' is required by Lit: each component's static styles
// are injected as inline <style> elements in Shadow DOM. script-src remains
// locked to 'self' only — no 'unsafe-inline' or 'unsafe-eval'.
func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; "+
			"script-src 'self'; "+
			"style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; "+
			"font-src 'self'; "+
			"connect-src 'self'; "+
			"frame-ancestors 'none'; "+
			"base-uri 'self'; "+
			"form-action 'self'")
}
