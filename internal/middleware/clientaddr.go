package middleware

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientAddr resolves the client address a request came from: the left-most
// X-Forwarded-For entry when the header is present, else the connection's peer
// address with its port stripped. Every consumer shares it — the IP rate
// limiter and the audit writers — so a record can never disagree with the
// bucket that throttled it.
//
// Trust: the header is accepted
// without a Go-side allowlist, and that is deliberate. The app publishes NO
// host port (`compose.yaml`: `expose` only) and Caddy is its only ingress, so
// the only writers of the header are Caddy and, behind it, Cloudflare — and
// Caddy parses X-Forwarded-For right-to-left against the static Cloudflare
// ranges (`Caddyfile`, `trusted_proxies static … + trusted_proxies_strict`),
// dropping any client-appended prefix before it reaches Go. The left-most
// entry is therefore the original client. A second allowlist inside the app
// would duplicate that trust boundary and add a way to get it wrong.
//
// A forwarded entry that does not parse as a bare IP literal (a malformed
// value, or one carrying an IPv6 zone identifier — attacker-chosen text) is
// not a client identity: the resolver falls back to the peer instead of
// feeding that text into rate-limit keys and audit rows. The check refines
// the input, it is not a second trust list.
//
// An empty peer address (a synthetic request in a unit test) resolves to "".
// RemoteAddr always carries a port — or brackets an IPv6 literal — so the
// fallback strips it in both shapes.
func ClientAddr(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// The left-most entry is the original client, before any proxy.
		if before, _, ok := strings.Cut(fwd, ","); ok {
			fwd = before
		}
		if addr, err := netip.ParseAddr(strings.TrimSpace(fwd)); err == nil && addr.Zone() == "" {
			return addr.String()
		}
	}

	// Fall back to RemoteAddr, which carries ip:port — or [v6]:port.
	host := r.RemoteAddr
	if strings.HasPrefix(host, "[") {
		if end := strings.IndexByte(host, ']'); end != -1 {
			return host[1:end]
		}
	}
	if idx := strings.LastIndexByte(host, ':'); idx != -1 {
		return host[:idx]
	}
	return host
}
