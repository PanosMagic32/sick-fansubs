# VPS Bring-Up and Recovery Runbook (owner-executed)

> Provisioning and recovery reference: the first bring-up (acceptance checks
> in §7), the firewall, and the from-scratch rebuild path.
>
> Version updates and rollback are owned by the tag workflow — see
> [docs/ops/deploy.md](../docs/ops/deploy.md). The manual image build/load
> lines survive only as the offline fallback for a first bring-up without
> published images or a registry outage.
>
> **SSH alias:** the owner reaches the VPS through an SSH alias. Replace
> `<user>@<vps-ip>` with the alias name and drop `-P 32122` throughout
> (the alias carries port/user/key).

## Prerequisites

- **Local machine:** docker (+ buildx for `--platform` below) + this repo
  checkout, SSH access to the VPS.
- **VPS:** Docker + Compose v2.24 or newer (`env_file.required`, `ps --format '{{.Health}}'`),
  `curl`, `flock`, ufw active, host port 443 free. ✓
- **VPS clock:** `sudo timedatectl set-timezone Europe/Berlin` — the cron and backup docs assume
  this host timezone (03:00 Berlin = 04:00 Athens). Verify with `timedatectl`. ✓
- **Cloudflare:** apex `sickfansubs.com` proxied with Full (strict); **"Always
  Use HTTPS" enabled** (Edge Certificates — plaintext HTTP is redirected at the
  edge and never reaches origin port 443); Zone:DNS:Edit API token created. No
  Origin Rule — Cloudflare proxies straight to Caddy on 443. `www.sickfansubs.com`
  is redirected to the apex by a Cloudflare Redirect Rule; its proxied
  placeholder record (`AAAA 100::`) never reaches the origin, and the Caddyfile's
  `www` site block stands as the origin-side fallback if that rule is removed.
  Bot Fight Mode
  stays on: it challenges the workflow runner's shared egress IPs, so the deploy
  probe runs from the VPS ([deploy.md](../docs/ops/deploy.md)). The app's strict
  CSP (`script-src 'self'`) blocks Cloudflare's JavaScript Detections injection,
  so browsers are unaffected. ✓
- The token value lives **only** in the VPS `.env` — never in chat, never in
  the repo.

## 1. Prepare the VPS directory

```sh
mkdir -p ~/sick-fansubs
```

From the local repo checkout (with an SSH alias, drop `-P 32122` and
`<user>@<vps-ip>`):

```sh
scp compose.yaml Caddyfile <alias>:~/sick-fansubs/
```

On the VPS, create the owner-only env file:

```sh
cd ~/sick-fansubs
nano .env        # contents below; token is the real one
chmod 600 .env
```

`.env`:

```
TAG=<version>
CLOUDFLARE_API_TOKEN=<zone-dns-edit-token>
SMTP_API_KEY=<resend-api-key>
SMTP_FROM=noreply@sickfansubs.com
VAPID_PRIVATE_KEY=<vapid-private-key>
VAPID_PUBLIC_KEY=<vapid-public-key>
```

`SMTP_API_KEY` feeds the Compose file secret `smtp_api_key`: the value is
read from this `.env` at `docker compose up` time and mounted at
`/run/secrets/smtp_api_key` (uid 1000, mode 0400) inside the app
container. It never appears in `docker compose config` output.
If the line is missing, Compose either refuses `up` or mounts an empty secret,
which the app's loader rejects as soon as a feature consumes it.

`SMTP_FROM` is the verified-domain sender:
`noreply@sickfansubs.com` once `sickfansubs.com` is verified in Resend
(DKIM + SPF + bounce MX records at Cloudflare — the live DNS host). There
is NO Reply-To: replies to the no-reply sender are meant to bounce.

### VAPID keypair

The production pair is live: the private half is in the VPS `.env`. The public
half is committed in four pinned copies — `web/src/shared/config/vapid.ts`,
the Go config default, `.env.example`, and the compose
`VAPID_PUBLIC_KEY:-…` fallback — all four pinned equal by
`internal/config/vapid_pin_test.go`.

Rotation is one runbook pass:

```sh
bunx web-push generate-vapid-keys
```

1. Put the PRIVATE half in this `.env`:

```
VAPID_PRIVATE_KEY=<vapid-private-key>
VAPID_PUBLIC_KEY=<vapid-public-key>
```

`VAPID_PRIVATE_KEY` feeds the Compose file secret `vapid_private_key`:
read from `.env` at `docker compose up` time and mounted at
`/run/secrets/vapid_private_key` (uid 1000, mode 0400).
The app REFUSES to start in production without it (fail-closed).
`VAPID_PUBLIC_KEY` must be the MATCHING public half.

2. Give the PUBLIC half to the build — swap the committed constant
   (`web/src/shared/config/vapid.ts`), the Go config default
   (`internal/config/config.go`), the `.env.example` line, and the compose
   fallback together; the pin tests fail until all four agree.

3. Rebuild and tag. A mismatched PAIR is the failure this flow must not
   ship: the pin tests keep the four public copies equal and config.Load
   validates the 65-byte wire format, but neither can see a private half that
   belongs to a different keypair — `config.ValidateVAPIDPair` catches that
   at startup (the API refuses to boot), and the first send after the deploy
   is the live confirmation. **A rotation strands existing subscriptions:**
   the browser keeps the key it subscribed with, so sends signed with the new
   key are refused (typically 401/403) and the stored row is neither deleted
   nor recovered — the device stays silent until the user toggles push off
   and on again. Treat a rotation as a user-visible event, not a silent
   maintenance step.

## 1a. Deployment account and CI

The tag workflow deploys as a dedicated account. Record and verify each item once:

- Create `sick-deploy`, give it Docker access, and make its home the stack directory owner. The
  account must be able to run `docker compose` without sudo.
- Authorize the CI deploy public key in `~sick-deploy/.ssh/authorized_keys`, restricted with
  `from=` and `restrict`. Keep the private half only in the GitHub `DEPLOY_KEY` secret.
- Record the VPS host-key fingerprint (the `[host]:port` known_hosts form when the port is not 22)
  in the `DEPLOY_HOST_KEY` secret.
- GitHub repository variables: `DEPLOY_PORT`, `DEPLOY_USER`, `PUBLIC_BASE_URL`. Repository secrets:
  `DEPLOY_HOST`, `DEPLOY_KEY`, `DEPLOY_HOST_KEY`. The `deploy-approval` environment carries the
  owner as required reviewer; keep the deploy secrets environment-scoped where GitHub supports it.
- Confirm the account can run the host-side scripts (`flock`, `timeout`, `curl`, `mega-put`) and
  that `~/sick-fansubs` is traversable by the container uid 1000.

## 2. Build the images (LOCAL machine)

```sh
DOCKER_BUILDKIT=1 docker build --platform linux/amd64 --target prod \
  --build-arg APP_VERSION=<version> -t ghcr.io/panosmagic32/sick-fansubs:<version> .
DOCKER_BUILDKIT=1 docker build --platform linux/amd64 -f Dockerfile.migrate -t ghcr.io/panosmagic32/sick-fansubs-migrate:latest .
DOCKER_BUILDKIT=1 docker build --platform linux/amd64 -f Dockerfile.rebuildsearch -t ghcr.io/panosmagic32/sick-fansubs-rebuildsearch:latest .
docker build --platform linux/amd64 -f Dockerfile.caddy -t ghcr.io/panosmagic32/sick-fansubs-caddy:latest .
docker save ghcr.io/panosmagic32/sick-fansubs:<version> | gzip > sick-fansubs-<version>.tar.gz
docker save ghcr.io/panosmagic32/sick-fansubs-migrate:latest | gzip > sick-fansubs-migrate.tar.gz
docker save ghcr.io/panosmagic32/sick-fansubs-rebuildsearch:latest | gzip > sick-fansubs-rebuildsearch.tar.gz
docker save ghcr.io/panosmagic32/sick-fansubs-caddy:latest | gzip > sick-fansubs-caddy.tar.gz
scp sick-fansubs-<version>.tar.gz sick-fansubs-migrate.tar.gz sick-fansubs-rebuildsearch.tar.gz sick-fansubs-caddy.tar.gz <alias>:~/sick-fansubs/
```

**Tag every fallback image with the ghcr.io reference compose resolves** — the app at
`ghcr.io/panosmagic32/sick-fansubs:${TAG}`, the auxiliaries at `:${AUX_TAG:-latest}`. A locally
loaded bare name never matches compose's reference and the stack tries to pull.

**REMEMBER `--target prod`** — production is the Dockerfile's last stage, and the
build contract stays explicit. `APP_VERSION` is the BARE semver (no `v`
prefix — the footer adds it). `--platform linux/amd64` keeps the build correct
even on an ARM workstation (no-op on amd64 hosts). The caddy image is
**custom-built** — the stock `caddy:2-alpine` lacks `dns.providers.cloudflare`
and dies at config load with `module not registered`.

## 3. Load the images (VPS)

```sh
cd ~/sick-fansubs
gzip -dc sick-fansubs-<version>.tar.gz | docker load
gzip -dc sick-fansubs-migrate.tar.gz | docker load
gzip -dc sick-fansubs-rebuildsearch.tar.gz | docker load
gzip -dc sick-fansubs-caddy.tar.gz | docker load
```

**Precondition for the offline path:** the fallback images above must be loaded under the exact
ghcr.io references compose resolves (app `:${TAG}`, migrate and caddy `:${AUX_TAG:-latest}`). On an
existing VPS, read `AUX_TAG` from `.env` first — a hardcoded `:latest` is correct only before the
first successful deploy. The backup and restore images are not built here — build them with their
Dockerfiles when the registry is unavailable.

## 4. Firewall: restrict 443 to Cloudflare edge IPs

**Why:** Docker's own iptables rules bypass ufw for published ports, so plain
`ufw allow` does not actually gate the container. The enforced gate is the
`DOCKER-USER` chain (checked before Docker's rules in FORWARD), persisted via
ufw's `after.rules`/`after6.rules` so it survives reboots and reloads. Rules
match with conntrack's **original** destination port because Docker's DNAT
rewrites the port before the filter table sees the packet. Each block **flushes
the chain first** (dockerd appends its own `-j RETURN` when it creates the
chain; without the flush that RETURN would run before our rules and open the
port to the world) and re-adds RETURN last, making reloads idempotent.
**Every rule carries `--ctstate NEW`** so it fires only on the initial SYN:
`--ctorigdstport` matches a conntrack entry's ORIGINAL tuple in BOTH directions,
so without the state restriction the DROP rule also kills the container's
SYN-ACK replies and every Cloudflare connection times out (522).
**Every rule also carries `-i <wan-iface>`**, the host's public interface
(`ip route show default` names it — `eth0` on the reference host). The same
original-tuple match would otherwise eat container-originated connections to a
remote 443 — ACME orders and web-push sends fail — so the gate must see only
traffic arriving from outside. Replace `<wan-iface>` in both blocks below.

Ranges are from <https://www.cloudflare.com/ips/> — re-check them before
reinstalling the blocks. **Run each block once** — check with
`grep sick-fansubs /etc/ufw/after.rules /etc/ufw/after6.rules` first; don't append
twice. To replace an existing block: restore the `.bak` files taken below
(`sudo cp /etc/ufw/after.rules.bak /etc/ufw/after.rules`, same for
`after6.rules`) and re-append the corrected block.

IPv4 rules:

```sh
sudo cp /etc/ufw/after.rules /etc/ufw/after.rules.bak
sudo tee -a /etc/ufw/after.rules > /dev/null <<'EOF'

# --- sick-fansubs: published port 443 restricted to Cloudflare edge IPs (v4) ---
*filter
:DOCKER-USER - [0:0]
-F DOCKER-USER
-A DOCKER-USER -i <wan-iface> -s 103.21.244.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 103.22.200.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 103.31.4.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 104.16.0.0/13 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 104.24.0.0/14 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 108.162.192.0/18 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 131.0.72.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 141.101.64.0/18 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 162.158.0.0/15 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 172.64.0.0/13 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 173.245.48.0/20 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 188.114.96.0/20 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 190.93.240.0/20 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 197.234.240.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 198.41.128.0/17 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j DROP
-A DOCKER-USER -j RETURN
COMMIT
EOF
```

IPv6 rules (same gate, the ip6tables namespace):

```sh
sudo cp /etc/ufw/after6.rules /etc/ufw/after6.rules.bak
sudo tee -a /etc/ufw/after6.rules > /dev/null <<'EOF'

# --- sick-fansubs: published port 443 restricted to Cloudflare edge IPs (v6) ---
*filter
:DOCKER-USER - [0:0]
-F DOCKER-USER
-A DOCKER-USER -i <wan-iface> -s 2400:cb00::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 2606:4700::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 2803:f800::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 2405:b500::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 2405:8100::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 2a06:98c0::/29 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -s 2c0f:f248::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -i <wan-iface> -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j DROP
-A DOCKER-USER -j RETURN
COMMIT
EOF
```

Reload and verify:

```sh
sudo ufw reload
sudo iptables -L DOCKER-USER -n -v     # ACCEPT list → DROP → RETURN
sudo ip6tables -L DOCKER-USER -n -v    # same shape in the v6 table
```

`ufw reload` must complete **without errors** — an error means the block did not
load and the port stays open.

## 5. First bring-up

```sh
cd ~/sick-fansubs
docker compose config -q              # validates compose + env substitution
docker compose --profile migrate run --rm migrate   # fresh volume: apply migrations
docker compose up -d
docker compose ps                     # app → (healthy), caddy → running
```

The caddy image is shipped like the others (no Docker Hub pull). Caddy obtains
the `sickfansubs.com` certificate on first request (DNS challenge through the
API token) — give the first curl a few seconds.

## 6. What should be visible

- `docker compose ps`: `app` (healthy), `caddy` (running); the app has **no
  host ports** (Caddy holds 443).

## 7. Acceptance checks

1. **Automatic TLS** — `curl -sI --resolve sickfansubs.com:443:127.0.0.1
https://sickfansubs.com/` returns 200 with `server: Caddy` at the origin;
   the public `curl -sI https://sickfansubs.com/` is the Cloudflare edge
   check (its `Server` header is rewritten); `docker compose logs caddy` shows
   `certificate obtained successfully` (the cert Cloudflare's Full-strict pull
   validated — that 200 itself is the proof; the cert Cloudflare presents to
   browsers is its own edge cert, so `curl -v` is not evidence about the
   origin).
2. **No direct access** — REQUIRED, from OUTSIDE the VPS (e.g. phone on
   cellular):

   ```sh
   curl -sk --max-time 8 -o /dev/null -w '%{http_code}\n' https://<vps-ip>:443/health/live
   ```

   `curl -w` prints `000` when no HTTP response arrives, so accept exit code
   28 **and** `000` together. Any real status code (200, 404, …) means the
   gate is broken. From the VPS itself a connect + cert error is _expected_
   (that path is served by Docker's proxy, not the firewall) — it is not
   evidence either way.

3. **Health** — `curl -s https://sickfansubs.com/health/live` and
   `/health/ready` return 200 JSON.
4. **Smoke** — browser on `sickfansubs.com`: register → sign-out → sign-in; a
   Greek search (e.g. `?q=ναρουτό`) returns the 200 envelope (empty results
   on an empty database); the footer shows the deployed version.
5. **Secrets** — `.env` is mode 600 and never left the VPS. The token appears
   only in the caddy container's env and `docker compose config` output on
   your owner-only host (expected by design — never paste either). Image
   layers hold no secrets: `docker history ghcr.io/panosmagic32/sick-fansubs:<version> | grep -i
token` prints nothing.
6. **Data survives** — register an account → `docker compose up -d
--force-recreate app` → sign in with it again.

## 8. Operating notes

- Logs: `docker compose logs -f app caddy`.
- **Backups, restore, and the Mega push** are a separate runbook:
  [`docs/ops/backup.md`](../docs/ops/backup.md) (the daily cron, the disk
  check and weekly cleanup, the pre-deploy/shutdown triggers, and the restore
  drill).
- **Version updates and rollback run through the tag workflow** — see
  [deploy.md](../docs/ops/deploy.md) rules 1, 6, and 7. Push a semver tag;
  the workflow tests, builds, deploys, health-gates, and probes the public
  edge. Never hand-edit a script on the VPS: every deploy ships the current
  files.
- **Version pairing:** the workflow publishes migrate/backup/restore/rebuildsearch/caddy under
  `:latest` and the version tag; the compose stack pins caddy/migrate/backup through
  `${AUX_TAG:-latest}` (written beside `TAG` and reset on rollback by `deploy.sh`). Restore and
  rebuildsearch have no compose service — their one-offs use the same tag; the manual offline path
  must load the aux images under the `AUX_TAG` value compose will resolve.
- **Secret rotation:** edit the `SMTP_API_KEY` or `VAPID_PRIVATE_KEY` line in `.env`, then
  `docker compose up -d --force-recreate app` — the app reads secrets only at startup, and compose
  does not recreate the service when only a secret source changes. No image rebuild, no repo
  change, no CI. The Cloudflare token's rotation order is deploy.md rule 5's.
- **Full teardown:** `docker compose down -v` destroys the volumes — only with
  owner sign-off.
