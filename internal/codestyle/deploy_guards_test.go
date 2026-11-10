package codestyle

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The deploy script's loopback TLS probe names the site the Caddyfile serves,
// and the local and public probes share one curl policy. Neither pair is
// visible to docslint; a drift fails a healthy release at the health gate, or
// lets the deploy host's curl configuration redirect a probe.
var (
	caddySiteAddressPattern = regexp.MustCompile(`(?m)^([a-z0-9][a-z0-9.-]*\.[a-z]{2,})[ \t]*\{`)
	scriptSiteDomainPattern = regexp.MustCompile(`(?m)^SITE_DOMAIN="([^"]+)"$`)
)

const deployProbeCurlPolicy = `-q --noproxy '*' --proto '=https'`

const deployRepairCommand = `docker compose run --rm --no-deps --user root --cap-add CHOWN --cap-add DAC_READ_SEARCH --entrypoint chown caddy -R 1000:1000 /data /config`

// TestDeployProbeMatchesCaddySite ties SITE_DOMAIN to the Caddyfile site
// address (docs/ops/deploy.md rule 6). A mismatch makes every deploy probe a
// name Caddy does not serve: five TLS failures, then a rollback.
func TestDeployProbeMatchesCaddySite(t *testing.T) {
	// Parser probes before the real files, so a broken pattern fails loudly
	// instead of passing vacuously.
	if m := caddySiteAddressPattern.FindStringSubmatch("example.com {\n\ttls internal\n}\n"); m == nil || m[1] != "example.com" {
		t.Fatalf("Caddyfile site parser did not match a site block: %v", m)
	}
	if m := caddySiteAddressPattern.FindStringSubmatch("{\n\tservers {\n\t}\n}\n"); m != nil {
		t.Fatalf("Caddyfile site parser matched a global-options block: %v", m)
	}
	if m := scriptSiteDomainPattern.FindStringSubmatch("SITE_DOMAIN=\"example.com\"\n"); m == nil || m[1] != "example.com" {
		t.Fatalf("deploy script SITE_DOMAIN parser did not match: %v", m)
	}

	root := repoRoot(t)
	caddy := readCF(t, filepath.Join(root, "Caddyfile"))
	script := readCF(t, filepath.Join(root, "deploy", "deploy.sh"))

	matches := scriptSiteDomainPattern.FindAllStringSubmatch(script, -1)
	if len(matches) != 1 {
		t.Fatalf("deploy/deploy.sh declares SITE_DOMAIN %d times, want exactly 1", len(matches))
	}
	domain := matches[0][1]

	sites := make(map[string]struct{})
	for _, m := range caddySiteAddressPattern.FindAllStringSubmatch(caddy, -1) {
		sites[m[1]] = struct{}{}
	}
	if len(sites) < 2 {
		t.Fatalf("Caddyfile site parser found %d site addresses, want the apex and www blocks", len(sites))
	}
	if _, ok := sites[domain]; !ok {
		t.Fatalf("SITE_DOMAIN %q is not a Caddyfile site address (docs/ops/deploy.md rule 6)", domain)
	}
}

// TestDeployRepairCommandIsShared keeps the script's repair hint and
// docs/ops/deploy.md rule 4 identical, and pins the narrow capability set:
// CHOWN changes ownership, DAC_READ_SEARCH traverses the state tree, and the
// write bypass of DAC_OVERRIDE is not carried.
func TestDeployRepairCommandIsShared(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{
		filepath.Join("deploy", "deploy.sh"),
		filepath.Join("docs", "ops", "deploy.md"),
	} {
		body := readCF(t, filepath.Join(root, name))
		if !strings.Contains(body, deployRepairCommand) {
			t.Fatalf("%s: repair command drifted from %q (docs/ops/deploy.md rule 4)", name, deployRepairCommand)
		}
		if strings.Contains(body, "--cap-add DAC_OVERRIDE") {
			t.Fatalf("%s: repair must not request DAC_OVERRIDE (docs/ops/deploy.md rule 4)", name)
		}
	}
}

// TestDeployProbesShareCurlPolicy keeps the loopback and public probes on the
// same hardened curl invocation (docs/ops/deploy.md rule 6): the deploy host's
// curlrc and proxy environment must not decide which client address, scheme,
// or route a probe uses.
func TestDeployProbesShareCurlPolicy(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{
		filepath.Join("deploy", "deploy.sh"),
		filepath.Join(".github", "workflows", "deploy.yml"),
	} {
		body := readCF(t, filepath.Join(root, name))
		if !strings.Contains(body, deployProbeCurlPolicy) {
			t.Fatalf("%s: probe must carry %q (docs/ops/deploy.md rule 6)", name, deployProbeCurlPolicy)
		}
	}
}
