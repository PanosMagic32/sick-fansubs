#!/bin/sh
# Shutdown wrapper: back up, then take the stack down.
#
# Docker Compose offers no pre-down hook, so THIS script is the contract —
# a raw "docker compose down" does NOT back up. Run as sick-deploy from
# ~/sick-fansubs (shipped there by the deploy workflow).
#
# The host lock is held for the whole run, so a shutdown never races a
# deploy or the cron backup (see lock.sh); backup-now.sh sees SICK_LOCK_HELD
# and does not re-lock.
#
# Failure semantics: if the backup fails, the stack is left in place
# (backup-now.sh attempts a restart and reports whether it succeeded) and the
# script exits non-zero — you want to know backups are broken before tearing
# anything down. Down proceeds only on a successful backup. Volumes survive
# "docker compose down" (no -v), so the data is never removed by this
# script.
set -eu

cd "$HOME/sick-fansubs"

. ./lock.sh

if ! sh backup-now.sh; then
	echo "backup failed — not taking the stack down (see the restart status above; resolve the backup failure first, or use a raw 'docker compose down' knowingly)" >&2
	exit 1
fi

docker compose down
echo "stack down; today's backup unit is published under backups/"
