#!/bin/sh
# Push the newest published backup folder to Mega.
#
# Write-only by design: this script never deletes anything in Mega — the
# Mega-side retention (keep 10, mirroring the local policy) is a manual owner
# task, so the automation holds no remote-destroy capability. The age
# ciphertext is opaque to Mega anyway. On success a UTC timestamp lands in
# .last-push beside this script — the readable "time since the last
# successful push" (backup.md rule 6).
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

# Byte-order globbing so the newest date folder is the last match regardless
# of the host's locale.
LC_ALL=C
export LC_ALL

command -v mega-put >/dev/null 2>&1 || {
	echo "mega-put not on PATH (cron runs with a minimal PATH — see backup.md)" >&2
	exit 1
}

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

mega-put "$newest" /sick-fansubs-backups/
printf '%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >.last-push.tmp
mv .last-push.tmp .last-push
echo "pushed $newest to /sick-fansubs-backups/"
