#!/bin/sh
# Report disk headroom for the stack and fail closed when the host is
# critically full. backup-now.sh runs this before it stops the app, so a
# nearly-full disk cannot half-write a backup or strand a stopped stack.
#
# Thresholds combine an absolute free-space floor with a used-percentage
# ceiling, checked on the filesystem holding the Docker root (images,
# container logs) and the stack directory (backups, lock, logs). A warning
# prints and continues; a critical result exits 1.
set -eu

WARN_AVAIL_KB=5242880 # 5 GiB
CRIT_AVAIL_KB=2097152 # 2 GiB
WARN_PCT=85
CRIT_PCT=95

status=0

human_kb() {
	if [ "$1" -ge 1048576 ]; then
		printf '%s GiB' "$(($1 / 1048576))"
	elif [ "$1" -ge 1024 ]; then
		printf '%s MiB' "$(($1 / 1024))"
	else
		printf '%s KiB' "$1"
	fi
}

check_path() {
	label=$1
	path=$2
	if [ ! -d "$path" ]; then
		echo "disk $label: $path is missing — checking / instead" >&2
		path=/
	fi
	line=$(df -Pk "$path" | awk 'NR == 2 {print $4 " " $5}')
	case "$line" in
	[0-9]*" "[0-9]* | [0-9]*" "[0-9]*"%") ;;
	*)
		echo "disk $label CRITICAL: unreadable df output — check the filesystem" >&2
		status=1
		return
		;;
	esac
	avail_kb=${line%% *}
	used_pct=${line##* }
	used_pct=${used_pct%\%}
	case "$avail_kb$used_pct" in
	*[!0-9]*)
		echo "disk $label CRITICAL: unreadable df output — check the filesystem" >&2
		status=1
		return
		;;
	esac
	printf 'disk %s: %s free (%s%% used) at %s\n' "$label" "$(human_kb "$avail_kb")" "$used_pct" "$path"
	if [ "$avail_kb" -lt "$CRIT_AVAIL_KB" ] || [ "$used_pct" -ge "$CRIT_PCT" ]; then
		echo "disk $label CRITICAL: under $((CRIT_AVAIL_KB / 1048576)) GiB free or ${CRIT_PCT}% used — reclaim space before writing backups" >&2
		status=1
	elif [ "$avail_kb" -lt "$WARN_AVAIL_KB" ] || [ "$used_pct" -ge "$WARN_PCT" ]; then
		echo "disk $label warning: under $((WARN_AVAIL_KB / 1048576)) GiB free or ${WARN_PCT}% used" >&2
	fi
}

docker_root=$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || true)
[ -n "$docker_root" ] || docker_root=/var/lib/docker

check_path docker "$docker_root"
check_path stack "$HOME/sick-fansubs"

if command -v docker >/dev/null 2>&1; then
	echo "docker system df:"
	docker system df 2>/dev/null || true
fi

exit "$status"
