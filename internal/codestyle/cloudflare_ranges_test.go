package codestyle

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The Caddyfile's trusted-proxy ranges and the ufw DOCKER-USER sources in the
// bring-up runbook are two hand-maintained copies of the Cloudflare edge
// ranges; docslint cannot see the pair, so the parity guard lives here.
var (
	cfCIDRPattern      = regexp.MustCompile(`[0-9a-fA-F:.]+/[0-9]{1,3}`)
	cfUFWSourcePattern = regexp.MustCompile(`-s\s+([0-9a-fA-F:.]+/[0-9]{1,3})`)
)

// TestCloudflareRangesMatch ties the edge config's trusted-proxy ranges to the
// ufw blocks (docs/ops/deploy.md rule 3). A drift either untrusts a Cloudflare
// edge at Caddy — client identity collapses to one edge address — or blocks it
// at the firewall (522s for part of the users).
func TestCloudflareRangesMatch(t *testing.T) {
	// Parser probes: the two shapes the guard reads must match before the
	// repository files do, so a broken pattern cannot pass vacuously.
	if got := collectCF(cfCIDRPattern, trustedProxyBlock("trusted_proxies static \\\n\t103.21.244.0/22 2c0f:f248::/32\n")); len(got) != 2 {
		t.Fatalf("Caddyfile CIDR parser matched %d ranges in the probe, want 2", len(got))
	}
	if got := collectCF(cfUFWSourcePattern, "-A DOCKER-USER -s 103.21.244.0/22 -p tcp -j ACCEPT\n"); len(got) != 1 {
		t.Fatalf("ufw source parser matched %d ranges in the probe, want 1", len(got))
	}

	root := repoRoot(t)
	caddy := readCF(t, filepath.Join(root, "Caddyfile"))
	runbook := readCF(t, filepath.Join(root, "deploy", "bring-up.md"))

	caddySet := collectCF(cfCIDRPattern, trustedProxyBlock(caddy))
	ufwSet := collectCF(cfUFWSourcePattern, runbook)

	// Floors keep the guard meaningful if a file stops carrying the lists.
	if v4, v6 := splitCF(caddySet); len(caddySet) < 20 || v4 < 10 || v6 < 5 {
		t.Fatalf("Caddyfile trusted_proxies parsed %d ranges (%d v4, %d v6), want the Cloudflare set", len(caddySet), v4, v6)
	}
	if v4, v6 := splitCF(ufwSet); len(ufwSet) < 20 || v4 < 10 || v6 < 5 {
		t.Fatalf("the ufw blocks parsed %d sources (%d v4, %d v6), want the Cloudflare set", len(ufwSet), v4, v6)
	}

	if diff := cfDifference(caddySet, ufwSet); len(diff) > 0 {
		t.Fatalf("ranges in the Caddyfile but not in the ufw blocks: %s", strings.Join(diff, ", "))
	}
	if diff := cfDifference(ufwSet, caddySet); len(diff) > 0 {
		t.Fatalf("ranges in the ufw blocks but not in the Caddyfile: %s", strings.Join(diff, ", "))
	}
}

func readCF(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func collectCF(re *regexp.Regexp, text string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out[strings.ToLower(m[len(m)-1])] = struct{}{}
	}
	return out
}

// trustedProxyBlock returns the continuation lines of the Caddyfile's
// trusted_proxies directive, so the range parser cannot see port-like tokens
// elsewhere in the file (the header comment names 80/443).
func trustedProxyBlock(text string) string {
	var out []string
	inBlock := false
	for line := range strings.SplitSeq(text, "\n") {
		if !inBlock {
			if !strings.Contains(line, "trusted_proxies static") {
				continue
			}
			inBlock = true
		}
		out = append(out, line)
		if !strings.HasSuffix(strings.TrimSpace(line), "\\") {
			break
		}
	}
	return strings.Join(out, "\n")
}

func splitCF(set map[string]struct{}) (v4, v6 int) {
	for cidr := range set {
		if strings.Contains(cidr, ":") {
			v6++
		} else {
			v4++
		}
	}
	return v4, v6
}

func cfDifference(from, to map[string]struct{}) []string {
	var out []string
	for cidr := range from {
		if _, ok := to[cidr]; !ok {
			out = append(out, cidr)
		}
	}
	slices.Sort(out)
	return out
}
