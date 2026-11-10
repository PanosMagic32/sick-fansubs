#!/bin/sh
# Versioned deploy. Runs on the VPS as
# sick-deploy, invoked by the GH Actions deploy workflow with NEW_TAG set.
# The workflow ships this script and compose.yaml together, so the deployed
# commands are always the versioned, reviewed ones from the repo.
#
# Sequence (two phases):
#
# 1. preflight — pull the NEW app image + the version-paired
#    migrate/backup/caddy images (tagged with NEW_TAG) and the restore
#    image (:latest — the break-glass recovery path), refuse when Caddy's
#    state directories are not owned by uid 1000, stop the app
#    (single-writer rule for migrations and backups), and take the
#    PRE-DEPLOY BACKUP (the rollback safety net
#    this deploy falls back to; the stop/backup/restart semantics live in
#    backup-now.sh). NOTHING in this phase changes the running
#    configuration: the staged Caddyfile.new from the workflow scp is not
#    swapped in until the deploy phase, so any failure leaves the old stack
#    running the old Caddyfile and aborts.
# 2. deploy — run compose on the NEW pair (exported into the shell
#    environment; .env is committed only after the health gate, so a kill
#    can never destroy the rollback target) → swap the staged Caddyfile in
#    (saving the previous one) → run migrations through the migrate profile
#    (idempotent no-op when none are pending) → up -d → wait for the compose
#    healthcheck (polls /health/ready, a stably running caddy container, and
#    a loopback TLS request for the site name).
#    Any failure here restores the previous TAG/AUX_TAG pair and the
#    previous Caddyfile and brings the stack back up.
#
# Rollback honesty: the image swap is attempted first. If the rollback stack
# does NOT become healthy, the script prints the operator decision — the
# STOP message names the three causes (a migration applied by the failed
# deploy, a configuration fault, or an edge that fails the loopback TLS
# probe) and the recovery options.
#
# Serialization and interruptions: all backup/deploy triggers share a host
# lock (lock.sh), so a deploy waits for a running cron backup instead of
# racing it. HUP/INT/TERM trigger a best-effort recovery (a rollback once the
# deploy phase began, an old-stack restart before it); SIGKILL and a VPS
# reboot cannot be trapped, so an interrupted run is recovered through the
# job log and a re-run. Phase timings print as "elapsed".
set -eu

cd "$HOME/sick-fansubs"

. ./lock.sh

# A killed run can leave a half-written .env temp copy; sweep it under the
# lock before anything reads .env.
rm -f .env.tmp.*

NEW_TAG="${NEW_TAG:?NEW_TAG is required}"
PREV_TAG=$(grep -E '^TAG=' .env | cut -d= -f2-)
if [ -z "$PREV_TAG" ]; then
	echo "no previous TAG in .env — aborting" >&2
	exit 1
fi

# PREV_AUX_TAG defaults to :latest — the pre-pairing state. After the
# first successful paired deploy it equals the previous TAG, so a rollback
# restores the aux images the previous app was paired with.
PREV_AUX_TAG=$(grep -E '^AUX_TAG=' .env | cut -d= -f2-)
[ -z "$PREV_AUX_TAG" ] && PREV_AUX_TAG="latest"

APP_IMAGE="ghcr.io/panosmagic32/sick-fansubs"
MIGRATE_IMAGE="ghcr.io/panosmagic32/sick-fansubs-migrate"
BACKUP_IMAGE="ghcr.io/panosmagic32/sick-fansubs-backup"
RESTORE_IMAGE="ghcr.io/panosmagic32/sick-fansubs-restore"
CADDY_IMAGE="ghcr.io/panosmagic32/sick-fansubs-caddy"
# Matches the site address in the Caddyfile. The loopback TLS probe resolves
# this name locally, so the probe tests the edge that serves production
# without depending on public DNS or Cloudflare.
SITE_DOMAIN="sickfansubs.com"

started_at=$(date +%s)
elapsed() {
	echo "$(($(date +%s) - started_at))s"
}

env_value() {
	grep -E "^$1=" .env | head -n 1 | cut -d= -f2-
}

# sed_escape backslash-escapes the characters sed treats specially in a
# replacement (\, &, /): the rollback path feeds values read from the
# operator-editable .env.
sed_escape() {
	printf '%s' "$1" | sed 's/[\\&/]/\\&/g'
}

# write_env_pair rewrites both keys in one temp-file + mv pass: TAG and
# AUX_TAG can never be left out of step by a crash, and a missing .env is
# created. The temp file is chmod 600 before the swap — .env holds the
# deploy's secrets and keeps its owner-only mode. The compose shell
# environment overrides .env, so the deploy phase runs on exported values
# and commits here only after the stack is healthy.
write_env_pair() {
	key1="$1"
	val1=$(sed_escape "$2")
	key2="$3"
	val2=$(sed_escape "$4")
	tmp=".env.tmp.$$"
	if [ -f .env ]; then
		(umask 077 && sed -e "s/^${key1}=.*/${key1}=${val1}/" -e "s/^${key2}=.*/${key2}=${val2}/" .env >"$tmp") || {
			rm -f "$tmp"
			return 1
		}
	else
		(umask 077 && : >"$tmp") || return 1
	fi
	# A file whose last line lacks a newline would merge with an append.
	if [ -s "$tmp" ] && [ -n "$(tail -c 1 "$tmp")" ]; then
		printf '\n' >>"$tmp" || {
			rm -f "$tmp"
			return 1
		}
	fi
	grep -qE "^${key1}=" "$tmp" || printf '%s=%s\n' "$key1" "$2" >>"$tmp" || {
		rm -f "$tmp"
		return 1
	}
	grep -qE "^${key2}=" "$tmp" || printf '%s=%s\n' "$key2" "$4" >>"$tmp" || {
		rm -f "$tmp"
		return 1
	}
	chmod 600 "$tmp" || {
		rm -f "$tmp"
		return 1
	}
	mv "$tmp" .env || {
		rm -f "$tmp"
		return 1
	}
}

caddy_up() {
	caddy=$(docker compose ps --format '{{.Status}}' caddy) || return 1
	caddy=$(printf '%s\n' "$caddy" | head -n 1)
	case "$caddy" in
	"Up "*) return 0 ;;
	*)
		echo "caddy not running (status: $caddy)" >&2
		return 1
		;;
	esac
}

# caddy_state_repair_hint prints the one-time ownership repair from
# docs/ops/deploy.md rule 4. Caddy must be stopped for it; CHOWN changes the
# owner and DAC_READ_SEARCH traverses the uid-1000 directories, without the
# write bypass DAC_OVERRIDE would grant.
caddy_state_repair_hint() {
	echo "repair it while caddy is stopped, then start the deploy again:" >&2
	echo "  cd ~/sick-fansubs" >&2
	echo "  docker compose stop caddy" >&2
	echo "  docker compose run --rm --no-deps --user root --cap-add CHOWN --cap-add DAC_READ_SEARCH --entrypoint chown caddy -R 1000:1000 /data /config" >&2
}

# caddy_state_owned fails when a path in Caddy's state directories is not
# owned by uid 1000. A root-owned certificate cache entry makes the non-root
# process serve no certificate at all: the container stays Up, the app stays
# healthy, and every handshake aborts with a TLS internal error. The one-off
# runs the image being deployed, mounts only the state volumes of the
# existing caddy container, running or stopped (or, after `docker compose
# down`, the volumes derived from the compose project name), and has no
# network or capabilities; it runs as uid 1000, so a directory that uid
# cannot traverse fails too. Missing state directories fail separately from
# unreadable ones, so a chown is never offered for a path that does not exist.
# The mount roots are excluded: Caddy writes only inside `/data/caddy` and
# `/config/caddy`, so a traversable root-owned mount root is harmless and an
# untraversable one fails the check. This is an early refusal; the loopback
# TLS probe remains the runtime gate.
caddy_state_owned() {
	data_vol=""
	config_vol=""
	cid=$(docker compose ps -a -q caddy) || return 1
	cid=$(printf '%s\n' "$cid" | head -n 1)
	if [ -n "$cid" ]; then
		mounts=$(docker inspect --format '{{range .Mounts}}{{.Destination}}={{.Name}}{{"\n"}}{{end}}' "$cid") || return 1
		data_vol=$(printf '%s\n' "$mounts" | sed -n 's|^/data=||p')
		config_vol=$(printf '%s\n' "$mounts" | sed -n 's|^/config=||p')
		if [ -z "$data_vol" ] || [ -z "$config_vol" ]; then
			echo "the caddy container has no /data and /config named volumes — see compose.yaml" >&2
			return 1
		fi
	else
		# A shutdown (`docker compose down`) removes the container but keeps
		# the volumes, so the deploy after it must still check them. Compose
		# names volumes `<project>_<key>`; an explicit project name wins over
		# the file's `name:`.
		project="${COMPOSE_PROJECT_NAME:-}"
		[ -n "$project" ] || project=$(env_value COMPOSE_PROJECT_NAME)
		[ -n "$project" ] || project=$(sed -n 's/^name:[[:space:]]*//p' compose.yaml | head -n 1)
		case "$project" in
		"" | *[!a-zA-Z0-9_.-]*)
			echo "cannot derive the compose project name — check compose.yaml" >&2
			return 1
			;;
		esac
		data_vol="${project}_caddy-data"
		config_vol="${project}_caddy-config"
		absent=0
		docker volume inspect "$data_vol" >/dev/null 2>&1 || absent=$((absent + 1))
		docker volume inspect "$config_vol" >/dev/null 2>&1 || absent=$((absent + 1))
		if [ "$absent" -eq 2 ]; then
			echo "no caddy state volumes yet — skipping the state ownership check" >&2
			return 0
		fi
		if [ "$absent" -ne 0 ]; then
			echo "one caddy state volume is missing — expected both; check the volumes before deploying" >&2
			return 1
		fi
	fi
	rc=0
	bad=$(docker run --rm --network none --cap-drop ALL \
		--security-opt=no-new-privileges --read-only --user 1000:1000 \
		-v "$data_vol:/data:ro" -v "$config_vol:/config:ro" \
		--entrypoint sh "$CADDY_IMAGE:$NEW_TAG" -c '
		for d in /data/caddy /config/caddy; do
			[ -d "$d" ] || { echo "missing state directory: $d" >&2; exit 3; }
		done
		find /data/caddy /config/caddy ! -user 1000 -print
	') || rc=$?
	if [ "$rc" -eq 3 ]; then
		echo "caddy state directories are missing in the volumes — check the image and volume initialization" >&2
		return 1
	fi
	if [ "$rc" -ge 125 ]; then
		# 125-127 are docker/exec failures and 128+N is a killed container;
		# none of them is a state-ownership result.
		echo "the caddy state check could not run — see the error above" >&2
		return 1
	fi
	if [ "$rc" -ne 0 ]; then
		echo "caddy state is unreadable by uid 1000 (find exited $rc)" >&2
		[ -z "$bad" ] || printf '%s\n' "$bad" >&2
		caddy_state_repair_hint
		return 1
	fi
	if [ -n "$bad" ]; then
		echo "caddy state is not owned by uid 1000: $bad" >&2
		caddy_state_repair_hint
		return 1
	fi
}

# caddy_tls_ready proves the edge completes a TLS handshake and reaches the
# app. A container that reads Up can still fail every handshake, so container
# state alone does not prove the public path works. The retry window matches
# the public probe's cadence to absorb a cold certificate cache. The probe
# ignores the deploy host's curlrc and proxy environment (`-q` must stay the
# first option) and accepts HTTPS only.
caddy_tls_ready() {
	i=0
	while [ "$i" -lt 5 ]; do
		code=$(curl -q --noproxy '*' --proto '=https' -sS -o /dev/null -w '%{http_code}' --max-time 10 \
			--resolve "$SITE_DOMAIN:443:127.0.0.1" \
			"https://$SITE_DOMAIN/health/ready" || true)
		[ "$code" = "200" ] && return 0
		i=$((i + 1))
		[ "$i" -lt 5 ] || break
		sleep 5
	done
	echo "caddy TLS probe failed: https://$SITE_DOMAIN/health/ready (last status ${code:-none})" >&2
	return 1
}

# wait_healthy polls the compose healthcheck (which probes /health/ready)
# until the app reports healthy or the window expires, then requires caddy to
# be Up on two consecutive samples — a crash-looping container can read "Up"
# between restarts, and a "green" deploy with a dead edge must not pass — and
# finally a loopback TLS request for the site name. A
# compose failure (a parse-time refusal, a daemon error) aborts with its own
# message instead of masquerading as "still starting".
wait_healthy() {
	i=0
	while [ "$i" -lt 60 ]; do
		status=$(docker compose ps --format '{{.Health}}' app) || {
			echo "docker compose ps failed — see the compose error above" >&2
			return 1
		}
		status=$(printf '%s\n' "$status" | head -n 1)
		[ "$status" = "healthy" ] && break
		i=$((i + 1))
		sleep 2
	done
	[ "$i" -lt 60 ] || {
		echo "app did not become healthy within 120s" >&2
		return 1
	}

	caddy_up || return 1
	sleep 2
	caddy_up || return 1
	caddy_tls_ready || return 1
}

# preflight pulls the images and takes the pre-deploy backup. Direct
# "docker pull"s — "docker compose pull" honors the
# .env TAG, which still names the OLD app image at this point, and compose
# "run" only pulls MISSING images, so the version-paired migrate/backup/
# caddy images would otherwise go stale. The restore image rides :latest
# (the break-glass recovery path in restore.md). Each step guards
# its own failure explicitly (set -e is suspended inside the "if ! preflight"
# condition): a failed pull or stop must never fall through into the next
# step. A refusal before the stop (the caddy state check) leaves the running
# stack untouched; a failure after it restarts the old stack and aborts with
# `.env` untouched (a deploy without its rollback safety net must not
# proceed).
preflight() {
	docker pull "$APP_IMAGE:$NEW_TAG" || return 1
	docker pull "$MIGRATE_IMAGE:$NEW_TAG" || return 1
	docker pull "$BACKUP_IMAGE:$NEW_TAG" || return 1
	docker pull "$RESTORE_IMAGE:latest" || return 1
	docker pull "$CADDY_IMAGE:$NEW_TAG" || return 1
	# The running stack is untouched by a refusal here: this check runs before
	# the stop and the pre-deploy backup.
	caddy_state_owned || return 1
	# The deploy trigger may REPLACE today's existing unit: its rollback
	# point must be fresh, so a cron unit from earlier today is replaced
	# rather than reused or failed on. The backup runs on the tag being
	# deployed's paired image (BACKUP_AUX_TAG): the live history may already
	# carry this deploy's migration from a failed first attempt, and an
	# older image's embedded set would refuse it.
	if ! BACKUP_AUX_TAG="$NEW_TAG" sh backup-now.sh replace; then
		echo "pre-deploy backup failed — aborting before any change (TAG/.env untouched)" >&2
		if docker compose up -d; then
			echo "old stack restarted" >&2
		else
			echo "the old stack did NOT restart — the stack is down; fix the compose error above and run 'docker compose up -d'" >&2
		fi
		return 1
	fi
}

# deploy swaps the version, migrate, bring the stack up. Each step guards
# its own failure explicitly — set -e is suspended inside the "if ! deploy"
# condition — so a failed write or migration aborts straight to the rollback
# instead of pretending success on the old pair. The new TAG/AUX_TAG pair is
# exported (compose's shell environment overrides .env), and .env is only
# committed after the health gate: a kill before that leaves the previous
# pair as the rollback target.
deploy() {
	phase="deploy"
	export TAG="$NEW_TAG"
	export AUX_TAG="$NEW_TAG"
	# Swap the staged Caddyfile in, keeping the previous one for rollback.
	# The swap happens HERE, not in preflight: a preflight failure must
	# leave the running configuration untouched.
	if [ -f Caddyfile.new ]; then
		if [ -f Caddyfile ]; then
			cp Caddyfile Caddyfile.previous || return 1
		fi
		mv Caddyfile.new Caddyfile || return 1
	fi
	docker compose --profile migrate run --rm migrate || return 1
	docker compose up -d || return 1

	if ! wait_healthy; then
		return 1
	fi

	# The commit is the deploy's last step; read the pair back so a silent
	# write failure cannot leave .env claiming a tag the stack is not on.
	write_env_pair TAG "$NEW_TAG" AUX_TAG "$NEW_TAG" || return 1
	[ "$(env_value TAG)" = "$NEW_TAG" ] || return 1
	[ "$(env_value AUX_TAG)" = "$NEW_TAG" ] || return 1
	# Disarm the interruption trap: a signal after the commit must not roll
	# back a healthy, fully deployed stack (the flag first, so the window is
	# closed for the whole sequence).
	rollback_done=1
	trap - HUP INT TERM
}

# rollback is best-effort by design: every step is guarded so a failure
# cannot skip the stack restart or the STOP message. The compose output is
# kept (never discarded): a configuration refusal and a schema mismatch
# print differently, and the operator needs the real error before choosing.
rollback() {
	phase="rollback"
	echo "deploy failed — restoring TAG=$PREV_TAG (aux $PREV_AUX_TAG)" >&2
	export TAG="$PREV_TAG"
	export AUX_TAG="$PREV_AUX_TAG"
	write_env_pair TAG "$PREV_TAG" AUX_TAG "$PREV_AUX_TAG" || echo "WARNING: could not write the previous pair to .env" >&2
	# Restore the previous Caddyfile alongside the previous image: a
	# failed deploy must not leave a new edge config in front of the old
	# stack.
	if [ -f Caddyfile.previous ]; then
		mv Caddyfile.previous Caddyfile || echo "WARNING: could not restore the previous Caddyfile" >&2
	fi
	if ! docker compose pull; then
		echo "rollback image pull failed — see the compose error above" >&2
	fi
	if ! docker compose up -d; then
		echo "rollback 'up -d' failed — see the compose error above" >&2
	fi

	# If the rollback itself stays unhealthy, three causes: a migration applied
	# by the failed deploy (the previous image rejects the newer history), a
	# configuration fault (the compose error above), or an edge that fails the
	# loopback TLS probe (the probe line above). Stop for the operator's data
	# decision — never loop silently. The pre-deploy backup is the
	# staged-restore artifact for that decision.
	if ! wait_healthy; then
		echo "rollback stack did not become healthy." >&2
		echo "Three causes, in order of likelihood: (1) the failed deploy applied a migration — the previous image rejects newer schema history; (2) a configuration fault — see the compose error above; (3) the edge fails the loopback TLS probe — see the caddy TLS probe line above." >&2
		echo "STOP: read the compose output above first. Then decide: staged restore of the pre-deploy backup (restore.md), or fix-forward at $NEW_TAG (re-run the deploy). After a staged restore, re-run the deploy to bring the stack up at $NEW_TAG — the activated database carries the restore image's migration set, not the previous tag's." >&2
		echo "For cause 3, read the caddy TLS probe line and the Caddy log first — unreadable state (uid 1000 ownership) is one cause, a certificate or Caddyfile fault another. A database restore cannot fix an edge fault." >&2
	else
		echo "rollback stack healthy (elapsed $(elapsed))" >&2
	fi
}

phase="preflight"
rollback_done=0

# Best-effort interruption recovery: SIGKILL and a reboot cannot be caught,
# and dash runs a trap only after the foreground command returns — the
# job-side re-run remains the guaranteed recovery.
on_interrupt() {
	echo "interrupted during the $phase phase (elapsed $(elapsed))" >&2
	if [ "$rollback_done" -eq 0 ]; then
		if [ "$phase" = "deploy" ]; then
			rollback_done=1
			rollback
		else
			if docker compose up -d; then
				echo "old stack restarted" >&2
			else
				echo "the old stack did NOT restart — the stack is down; fix the compose error above and run 'docker compose up -d'" >&2
			fi
		fi
	fi
	exit 1
}
trap on_interrupt HUP INT TERM

if ! preflight; then
	echo "preflight failed (elapsed $(elapsed))" >&2
	exit 1
fi
echo "preflight complete (elapsed $(elapsed))"

if ! deploy; then
	echo "deploy failed (elapsed $(elapsed))" >&2
	rollback_done=1
	rollback
	exit 1
fi

echo "deploy complete: $NEW_TAG healthy (elapsed $(elapsed))"
