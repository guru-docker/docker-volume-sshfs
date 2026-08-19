#!/usr/bin/env bash
#
# End-to-end test for the sshfs volume plugin.
#
# Builds the managed plugin, starts a throwaway sshd container, then drives the
# plugin through the real Docker volume API. Every case writes through one
# container and reads back through another, so a silent no-op mount fails.
#
# Requires: docker with plugin support, and permission to install plugins
# (usually root). Override DOCKER=... to target a specific engine.
#
#   ./.travis/integration.sh
#
set -euo pipefail

cd "$(dirname "$0")/.."

DOCKER=${DOCKER:-docker}
PLUGIN_NAME=${PLUGIN_NAME:-glabservices/plugin-sshfs}
PLUGIN_TAG=${PLUGIN_TAG:-test}
PLUGIN="${PLUGIN_NAME}:${PLUGIN_TAG}"
SSHD_IMAGE=${SSHD_IMAGE:-docker-volume-sshfs-testsshd}
SSHD_PORT=${SSHD_PORT:-2222}
VOLUME=${VOLUME:-sshvolume}

sshd_cid=""
failures=0

log()  { echo; echo "=== $*"; }
fail() { echo "!!! FAIL: $*" >&2; failures=$((failures + 1)); }

cleanup() {
	local rc=$?
	log "cleanup"
	$DOCKER volume rm -f "$VOLUME" >/dev/null 2>&1 || true
	[ -n "$sshd_cid" ] && $DOCKER rm -f "$sshd_cid" >/dev/null 2>&1 || true
	$DOCKER plugin disable -f "$PLUGIN" >/dev/null 2>&1 || true
	$DOCKER plugin rm -f "$PLUGIN" >/dev/null 2>&1 || true
	exit $rc
}
trap cleanup EXIT

# Creates a volume, writes through one container, reads back through another.
# Any extra arguments are passed to `docker volume create`.
roundtrip() {
	local name=$1; shift
	local runopts=${RUN_OPTS:-}

	$DOCKER volume rm -f "$VOLUME" >/dev/null 2>&1 || true
	if ! $DOCKER volume create -d "$PLUGIN" "$@" "$VOLUME" >/dev/null; then
		fail "$name: volume create failed"
		return
	fi
	# shellcheck disable=SC2086  # runopts is an intentional word-split list
	if ! $DOCKER run --rm $runopts -v "$VOLUME:/write" busybox \
		sh -c "echo hello > /write/probe"; then
		fail "$name: write failed"
		return
	fi
	# shellcheck disable=SC2086
	if ! $DOCKER run --rm $runopts -v "$VOLUME:/read" busybox \
		grep -Fxq hello /read/probe; then
		fail "$name: readback failed"
		return
	fi
	$DOCKER volume rm -f "$VOLUME" >/dev/null 2>&1 || true
	echo "--- ok: $name"
}

log "pull fixtures"
$DOCKER pull -q busybox
$DOCKER build -q -t "$SSHD_IMAGE" .travis/ssh

log "build and enable plugin $PLUGIN"
PLUGIN_NAME="$PLUGIN_NAME" PLUGIN_TAG="$PLUGIN_TAG" DOCKER="$DOCKER" make
$DOCKER plugin enable "$PLUGIN"
$DOCKER plugin ls

log "start sshd on port $SSHD_PORT"
sshd_cid=$($DOCKER run -d -p "$SSHD_PORT:22" "$SSHD_IMAGE")
for _ in $(seq 30); do
	$DOCKER exec "$sshd_cid" sh -c 'pgrep sshd >/dev/null' 2>/dev/null && break
	sleep 1
done

log "case: password auth"
roundtrip "password auth" \
	-o sshcmd=root@localhost:/tmp -o port="$SSHD_PORT" -o password=root

log "case: allow_other, unprivileged reader"
RUN_OPTS="-u nobody" roundtrip "allow_other" \
	-o sshcmd=root@localhost:/tmp -o allow_other -o port="$SSHD_PORT" -o password=root

log "case: pass-through sshfs options"
roundtrip "pass-through options" \
	-o sshcmd=root@localhost:/tmp -o Compression=no -o Ciphers=aes128-ctr \
	-o port="$SSHD_PORT" -o password=root

log "case: relocated state.source"
$DOCKER plugin disable "$PLUGIN"
$DOCKER plugin set "$PLUGIN" state.source=/tmp
$DOCKER plugin enable "$PLUGIN"
roundtrip "relocated state" \
	-o sshcmd=root@localhost:/tmp -o port="$SSHD_PORT" -o password=root

log "case: ssh key auth"
$DOCKER plugin disable "$PLUGIN"
$DOCKER plugin set "$PLUGIN" sshkey.source="$(pwd)/.travis/ssh/"
$DOCKER plugin enable "$PLUGIN"
roundtrip "ssh key auth" \
	-o sshcmd=root@localhost:/tmp -o port="$SSHD_PORT"

log "case: sshcmd is required"
if $DOCKER volume create -d "$PLUGIN" -o port="$SSHD_PORT" "$VOLUME" >/dev/null 2>&1; then
	fail "sshcmd required: volume create should have been rejected"
	$DOCKER volume rm -f "$VOLUME" >/dev/null 2>&1 || true
else
	echo "--- ok: sshcmd is required"
fi

log "case: volume is released after use"
$DOCKER volume create -d "$PLUGIN" \
	-o sshcmd=root@localhost:/tmp -o port="$SSHD_PORT" -o password=root "$VOLUME" >/dev/null
$DOCKER run --rm -v "$VOLUME:/mnt" busybox true
if ! $DOCKER volume rm "$VOLUME" >/dev/null; then
	fail "release: volume could not be removed after its container exited"
else
	echo "--- ok: volume is released after use"
fi

log "result"
if [ "$failures" -ne 0 ]; then
	echo "$failures case(s) failed" >&2
	exit 1
fi
echo "all cases passed"
