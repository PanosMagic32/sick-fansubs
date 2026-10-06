#!/bin/sh
# Serialize the backup and deploy triggers on one host lock.
#
# Sourced by the top-level scripts (deploy.sh, daily-backup.sh,
# down.sh) after they cd to ~/sick-fansubs; backup-now.sh sources it too when
# invoked directly. The lock guards the single-writer window: a cron backup,
# a shutdown, and a deploy must never interleave their stop/migrate/up steps.
#
# A nested caller (deploy.sh -> backup-now.sh) exports SICK_LOCK_HELD=1
# to skip re-acquiring: the lock lives on a per-process file descriptor, so a
# second flock in the child process would wait on its own parent forever.
#
# The wait is deliberately unbounded — the caller's own timeout is the
# ceiling, and the kernel releases the lock when the holding process dies, so
# a killed deploy cannot strand it.

if [ "${SICK_LOCK_HELD:-0}" = "1" ]; then
	return 0
fi

command -v flock >/dev/null 2>&1 || {
	echo "flock not found — install util-linux; refusing to run without the single-writer lock" >&2
	exit 1
}

exec 9>>"$HOME/sick-fansubs/.lock"
if ! flock -n 9; then
	echo "another backup or deploy is running — waiting for the single-writer lock" >&2
	flock 9 || {
		echo "could not acquire the deploy/backup lock" >&2
		exit 1
	}
fi
export SICK_LOCK_HELD=1
