#!/bin/sh
# Daily backup — the host cron body, 03:00 host-local
# time (the VPS host stays on Europe/Berlin, so
# this fires at 04:00 Europe/Athens; the unit's folder date is computed in
# Europe/Athens inside the binary).
#
# Stops the app for the single-owner unready window (the
# database artifact and the media tarball share one capture point), creates
# the backup unit, restarts the app, then pushes the newest folder to Mega.
# A failed backup restarts the app and aborts BEFORE any push; a failed push
# OR a failed restart leaves the local unit published and exits non-zero so
# the cron surfaces it (RPO is the time since the last successful push — the
# .last-push stamp written by mega-put.sh).
#
# Run as sick-deploy from ~/sick-fansubs (shipped there by the deploy
# workflow). The cron entry lives in backup.md — the owner
# installs it once (crontab -e as sick-deploy).
set -eu

cd "$HOME/sick-fansubs"

# Held for the whole run so the cron never races a deploy or a shutdown
# (backup-now.sh sees SICK_LOCK_HELD and does not re-lock — see lock.sh).
. ./lock.sh

if ! sh backup-now.sh; then
	echo "daily backup failed — no Mega push" >&2
	exit 1
fi

# A restart failure must not swallow the already-published unit: record it,
# still attempt the push, and exit non-zero reporting both.
app_ok=1
docker compose up -d || app_ok=0

push_ok=1
if ! sh mega-put.sh; then
	push_ok=0
fi

if [ "$app_ok" -eq 0 ] || [ "$push_ok" -eq 0 ]; then
	if [ "$app_ok" -eq 0 ]; then
		echo "the app did NOT restart — the stack is down; fix the compose error above and run 'docker compose up -d'" >&2
	fi
	if [ "$push_ok" -eq 0 ]; then
		echo "Mega push failed — the local unit is published; fix the session/quota and re-run mega-put.sh" >&2
	fi
	exit 1
fi

echo "daily backup complete; newest unit pushed to Mega"
