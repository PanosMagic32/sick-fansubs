#!/bin/sh
# Push the newest published backup folder to Mega.
#
# Write-only by design: this script never deletes anything in Mega — the
# Mega-side retention (keep 10, mirroring the local policy) is a manual owner
# task, so the automation holds no remote-destroy capability. The age
# ciphertext is opaque to Mega anyway. On success .last-push records the UTC
# time and the pushed folder beside this script — the readable "time since the
# last successful push" (backup.md rule 6).
#
# Prerequisites (one-time, owner — see backup.md):
#   - MEGAcmd installed and "mega-login" run interactively as sick-deploy
#     (the session persists for cron);
#   - the dedicated backup-only Mega account; mega-put on PATH (cron runs
#     with a minimal PATH — see the crontab note in backup.md).
#
# The remote destination is /sick-fansubs-backups/. "mega-put <folder>
# <remote>" uploads the folder's contents recursively into the remote path;
# the exact recursive-folder behavior is confirmed at the one-time bring-up
# (runbook), since the MEGAcmd session lives only on the VPS.
set -eu

cd "$HOME/sick-fansubs"

# A manual push must serialize with a deploy or a backup. A nested caller is
# already marked by SICK_LOCK_HELD and skips the flock (see lock.sh).
. ./lock.sh

# Byte-order globbing so the newest date folder is the last match regardless
# of the host's locale.
LC_ALL=C
export LC_ALL

command -v mega-put >/dev/null 2>&1 || {
	echo "mega-put not on PATH (cron runs with a minimal PATH — see backup.md)" >&2
	exit 1
}
command -v timeout >/dev/null 2>&1 || {
	echo "timeout not found — install coreutils; refusing an unbounded Mega push" >&2
	exit 1
}

# A stalled MEGAcmd or network must not hold the host lock forever. A timeout
# is a failed push; the local unit stays published.
MEGA_PUT_TIMEOUT="${MEGA_PUT_TIMEOUT:-20m}"

# A crashed replace can leave today's unit as YYYY-MM-DD.previous (see
# backup.md). Publishing past it would silently ship yesterday's unit, so
# fail closed until it is resolved.
for prev in backups/????-??-??.previous; do
	[ -e "$prev" ] || continue
	echo "found $prev — an interrupted backup replace is unresolved; move it back (see backup.md) before pushing" >&2
	exit 1
done

newest=""
for d in backups/????-??-??; do
	[ -d "$d" ] || continue
	newest="$d"
done
if [ -z "$newest" ]; then
	echo "no published backup folders under backups/" >&2
	exit 1
fi

# MEGAcmd starts the long-lived mega-cmd-server on first use, and the daemon
# inherits the invoking process's open descriptors. The caller holds the host
# lock on fd 9 (lock.sh), so closing it for the push keeps the daemon from
# holding the deploy/backup lock after the trigger exits.
if ! timeout "$MEGA_PUT_TIMEOUT" mega-put "$newest" /sick-fansubs-backups/ 9>&-; then
	echo "Mega push failed or exceeded $MEGA_PUT_TIMEOUT — the local unit is published; fix the session/quota and re-run" >&2
	exit 1
fi
printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$newest" >.last-push.tmp
mv .last-push.tmp .last-push
echo "pushed $newest to /sick-fansubs-backups/"
