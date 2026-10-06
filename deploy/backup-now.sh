#!/bin/sh
# backup-now.sh — the single-owner backup window: stop the app,
# create the daily backup unit, and on failure restart the app and exit
# non-zero. On SUCCESS the app is left STOPPED — the caller decides the next
# step (deploy migrate, up -d, or down).
#
# Usage: backup-now.sh [replace]
#
# Shared by deploy.sh (pre-deploy trigger), daily-backup.sh (03:00
# cron), and down.sh (shutdown trigger) so the stop->backup->restart-on-
# failure semantics live in exactly one place. Run as
# sick-deploy from ~/sick-fansubs (shipped there by the deploy workflow).
#
# ONLY the deploy trigger passes "replace": it
# replaces today's existing unit with a fresh pre-migrate one instead of
# failing closed — the deploy's rollback point must be fresh, and the tag
# push is the operator's explicit resolution. The same trigger also passes
# -pre-migrate: the deploy backup runs BEFORE the
# deploy's migration, so the live database may be an older prefix of the NEW
# backup image's embedded migrations (a new tag shipping a new migration) —
# the history is then validated as a contiguous prefix, not the exact set.
# The cron and down.sh keep the plain fail-closed refusal and exact history
# (never clobber a published unit).
set -eu

cd "$HOME/sick-fansubs"

# The top-level triggers hold the lock for their whole run; a direct
# invocation takes it here. A nested caller is marked by SICK_LOCK_HELD and
# skips it (see lock.sh).
. ./lock.sh

# A critically full disk must not enter the stop window: the backup would
# half-write and leave the app stopped for nothing. Warnings print and
# continue; a critical host aborts before `stop app`.
sh ./disk-check.sh

[ $# -le 1 ] || {
	echo "usage: backup-now.sh [replace]" >&2
	exit 2
}

case "${1:-}" in
"") BACKUP_ARGS="" ;;
replace) BACKUP_ARGS="-replace-today -pre-migrate" ;;
*)
	echo "usage: backup-now.sh [replace]" >&2
	exit 2
	;;
esac

docker compose stop app
# $BACKUP_ARGS is deliberately unquoted: the empty value must expand to no
# argument at all. BACKUP_AUX_TAG pins the one-shot to a specific paired
# image — the deploy preflight passes the NEW tag, because the live history
# may already carry that tag's migration while .env still names the old
# pair, and an older backup image's embedded set would refuse it.
run_backup() {
	if [ -n "${BACKUP_AUX_TAG:-}" ]; then
		AUX_TAG="$BACKUP_AUX_TAG" docker compose --profile backup run --rm backup $BACKUP_ARGS
	else
		docker compose --profile backup run --rm backup $BACKUP_ARGS
	fi
}
if ! run_backup; then
	echo "backup failed" >&2
	if docker compose up -d; then
		echo "app restarted" >&2
	else
		echo "the app did NOT restart — the stack is down; fix the compose error above and run 'docker compose up -d'" >&2
	fi
	exit 1
fi
