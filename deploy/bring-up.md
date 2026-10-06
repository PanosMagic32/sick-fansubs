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
- **VPS:** Docker + Compose, ufw active, host port 443 free. ✓
- **Cloudflare:** apex `sickfansubs.com` proxied with Full (strict); **"Always
  Use HTTPS" enabled** (Edge Certificates — plaintext HTTP is redirected at the
  edge and never reaches origin port 443); Zone:DNS:Edit API token created. No
  Origin Rule — Cloudflare proxies straight to Caddy on 443. Bot Fight Mode
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

## 2. Build the images (LOCAL machine)

```sh
DOCKER_BUILDKIT=1 docker build --platform linux/amd64 --target prod \
  --build-arg APP_VERSION=<version> -t ghcr.io/panosmagic32/sick-fansubs:<version> .
DOCKER_BUILDKIT=1 docker build --platform linux/amd64 -f Dockerfile.migrate -t ghcr.io/panosmagic32/sick-fansubs-migrate:latest .
docker build --platform linux/amd64 -f Dockerfile.caddy -t ghcr.io/panosmagic32/sick-fansubs-caddy:latest .
docker save ghcr.io/panosmagic32/sick-fansubs:<version> | gzip > sick-fansubs-<version>.tar.gz
docker save ghcr.io/panosmagic32/sick-fansubs-migrate:latest | gzip > sick-fansubs-migrate.tar.gz
docker save ghcr.io/panosmagic32/sick-fansubs-caddy:latest | gzip > sick-fansubs-caddy.tar.gz
scp sick-fansubs-<version>.tar.gz sick-fansubs-migrate.tar.gz sick-fansubs-caddy.tar.gz <alias>:~/sick-fansubs/
```

**Tag every fallback image with the ghcr.io reference compose resolves** — the app at
`ghcr.io/panosmagic32/sick-fansubs:${TAG}`, the auxiliaries at `:${AUX_TAG:-latest}`. A locally
loaded bare name never matches compose's reference and the stack tries to pull.

**REMEMBER `--target prod`** — the Dockerfile's default target is the dev/air
stage. `APP_VERSION` is the BARE semver (no `v`
prefix — the footer adds it). `--platform linux/amd64` keeps the build correct
even on an ARM workstation (no-op on amd64 hosts). The caddy image is
**custom-built** — the stock `caddy:2-alpine` lacks `dns.providers.cloudflare`
and dies at config load with `module not registered`.

## 3. Load the images (VPS)

```sh
cd ~/sick-fansubs
gzip -dc sick-fansubs-<version>.tar.gz | docker load
gzip -dc sick-fansubs-migrate.tar.gz | docker load
gzip -dc sick-fansubs-caddy.tar.gz | docker load
```

**Precondition for the offline path:** the three fallback images above must be loaded under the
exact ghcr.io references compose resolves (app `:${TAG}`, migrate and caddy
`:${AUX_TAG:-latest}`). The backup and restore images are not built here — build them with their
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
-A DOCKER-USER -s 103.21.244.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 103.22.200.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 103.31.4.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 104.16.0.0/13 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 104.24.0.0/14 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 108.162.192.0/18 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 131.0.72.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 141.101.64.0/18 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 162.158.0.0/15 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 172.64.0.0/13 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 173.245.48.0/20 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 188.114.96.0/20 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 190.93.240.0/20 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 197.234.240.0/22 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 198.41.128.0/17 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j DROP
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
-A DOCKER-USER -s 2400:cb00::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 2606:4700::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 2803:f800::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 2405:b500::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 2405:8100::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 2a06:98c0::/29 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -s 2c0f:f248::/32 -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j ACCEPT
-A DOCKER-USER -p tcp -m conntrack --ctstate NEW --ctorigdstport 443 -j DROP
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

1. **Automatic TLS** — `curl -sI https://sickfansubs.com/` returns 200 with
   `server: Caddy`; `docker compose logs caddy` shows
   `certificate obtained successfully` (the cert Cloudflare's Full-strict pull
   validated — that 200 itself is the proof; the cert Cloudflare presents to
   browsers is its own edge cert, so `curl -v` is not evidence about the
   origin).
2. **No direct access** — REQUIRED, from OUTSIDE the VPS (e.g. phone on
   cellular):

   ```sh
   curl -sk --max-time 8 -o /dev/null -w '%{http_code}\n' https://<vps-ip>:443/health/live
   ```

   Must print **nothing** and exit with a timeout (exit code 28) — the DROP
   rule silently discards. If it prints any HTTP status code (200, 404, …) the
   gate is broken. From the VPS itself a connect + cert error is _expected_
   (that path is served by Docker's proxy, not the firewall) — it is not
   evidence either way.

3. **Health** — `curl -s https://sickfansubs.com/health/live` and
   `/health/ready` return 200 JSON.
4. **Smoke** — browser on `sickfansubs.com`: register → sign-out → sign-in; a
   Greek search (e.g. `?q=ναρουτό`) returns the 200 envelope (empty results
   until the imports land content); the footer shows the deployed version.
5. **Secrets** — `.env` is mode 600 and never left the VPS. The token appears
   only in the caddy container's env and `docker compose config` output on
   your owner-only host (expected by design — never paste either). Image
   layers hold no secrets: `docker history sick-fansubs:<version> | grep -i
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
- **Version pairing:** the workflow publishes migrate/backup/restore/caddy under `:latest` and the
  version tag; the compose stack pins caddy/migrate/backup through `${AUX_TAG:-latest}` (written
  beside `TAG` and reset on rollback by `deploy.sh`). Restore has no compose service — the
  break-glass one-off rides `:latest`. The manual offline path rides the `:latest` default.
- **Secret rotation:** edit the `SMTP_API_KEY` line in `.env`,
  then `docker compose up -d` — Compose recreates the container and
  re-mounts the secret. No image rebuild, no repo change, no CI. The
  Cloudflare token's rotation order is deploy.md rule 5's.
- **Full teardown:** `docker compose down -v` destroys the volumes — only with
  owner sign-off.
