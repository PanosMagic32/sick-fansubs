#!/bin/sh
# Weekly disk cleanup for the stack: reclaim only what is provably safe —
# exited Compose one-offs, dangling images, the build cache, and tagged
# sick-fansubs release images outside the retention window. Volumes are never
# touched and no broad prune runs: a broad prune would drop the image tags a
# rollback needs.
#
# Cron: 30 3 * * 0  cd /home/sick-deploy/sick-fansubs && sh disk-cleanup.sh >> disk-cron.log 2>&1 || { tail -n 30 disk-cron.log >&2; exit 1; }
set -eu

cd "$HOME/sick-fansubs"
. ./lock.sh

# The running pair (TAG/AUX_TAG from .env) always survives; per repository the
# newest KEEP_RELEASES tags also survive, so a rollback target remains.
KEEP_RELEASES=3

env_value() {
	sed -n "s/^$1=//p" .env | head -n 1
}

report() {
	df -Pk . | awk 'NR == 2 {printf "disk: %s KiB free (%s used)\n", $4, $5}'
	docker system df
}

images=$(mktemp)
candidates=$(mktemp)
trap 'rm -f "$images" "$candidates"' EXIT

echo "before:"
report

docker container prune -f --filter label=com.docker.compose.oneoff=True ||
	echo "container prune failed; continuing" >&2
docker image prune -f || echo "image prune failed; continuing" >&2
docker builder prune -f || echo "builder prune failed; continuing" >&2

docker images --format '{{.Repository}}|{{.Tag}}|{{.CreatedAt}}' |
	grep '^ghcr.io/panosmagic32/sick-fansubs' >"$images" || true
[ -s "$images" ] || echo "no sick-fansubs images found" >&2

# Newest-first per repository; everything past the retention window becomes a
# candidate, minus the pair the running stack uses.
sort -t'|' -k1,1 -k3,3r "$images" |
	awk -F'|' -v keep="$KEEP_RELEASES" '{n[$1]++; if (n[$1] > keep && $2 != "<none>") print $1 ":" $2}' >"$candidates"

TAG=$(env_value TAG)
AUX_TAG=$(env_value AUX_TAG)
[ -n "$TAG" ] || {
	echo "no TAG in .env — refusing to prune (the running pair must be known)" >&2
	exit 1
}
[ -n "$AUX_TAG" ] || AUX_TAG=latest

removed=0
failed=0
while IFS= read -r ref; do
	[ -n "$ref" ] || continue
	case "$ref" in
	"ghcr.io/panosmagic32/sick-fansubs:$TAG") continue ;;
	"ghcr.io/panosmagic32/sick-fansubs-caddy:$AUX_TAG" | \
		"ghcr.io/panosmagic32/sick-fansubs-migrate:$AUX_TAG" | \
		"ghcr.io/panosmagic32/sick-fansubs-backup:$AUX_TAG" | \
		"ghcr.io/panosmagic32/sick-fansubs-restore:$AUX_TAG") continue ;;
	esac
	echo "removing old image tag $ref"
	if docker image rm "$ref"; then
		removed=$((removed + 1))
	else
		failed=$((failed + 1))
	fi
done <"$candidates"
[ -s "$candidates" ] || echo "no release tags outside the retention window"
echo "removed $removed tag(s); $failed refused"

echo "after:"
report
