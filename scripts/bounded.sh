#!/usr/bin/env bash
# Run a command inside a memory- and CPU-fenced cgroup, so a runaway build,
# test or render cannot take the desktop with it. Two of them did: a
# build-cache experiment reached 5.5GB of resident set, and a full check.sh
# (twelve parallel compiles and test binaries plus a headless GPU smoke)
# pushed the machine into swap and the IDE was the process the kernel
# chose to kill.
#
#   scripts/bounded.sh scripts/check.sh
#   BOUND_MEM=3G BOUND_CPU=400% scripts/bounded.sh go test ./...
#
# MemoryMax caps RESIDENT memory of the whole tree (children included) and
# MemorySwapMax=0 means the cap is hit by an OOM-kill of OUR tree, never by
# paging everything else out. CPUQuota leaves cores for the desktop, and
# GOFLAGS=-p caps how many compiles/tests go runs at once. Falls back to
# nice + the Go knobs when no user systemd is available.
set -u
mem="${BOUND_MEM:-6G}"
cpu="${BOUND_CPU:-600%}"
export GOFLAGS="${GOFLAGS:--p=4}"
export GOMAXPROCS="${GOMAXPROCS:-6}"
# BOUND_TIMES=1 prints the command's CPU time (user, sys, seconds) on
# stderr when it exits — `times` of a wrapper shell reports its children.
if [ -n "${BOUND_TIMES:-}" ]; then
  set -- bash -c 'e() { "$@"; }; e "$@"; rc=$?; times >&2; exit $rc' _ "$@"
fi
if command -v systemd-run >/dev/null 2>&1 &&
   systemd-run --user --scope -q -p MemoryMax=50M true 2>/dev/null; then
  exec systemd-run --user --scope -q \
    -p MemoryMax="$mem" -p MemorySwapMax=0 -p CPUQuota="$cpu" \
    -- nice -n 10 "$@"
fi
echo "bounded: no user systemd; falling back to nice + GOFLAGS=$GOFLAGS" >&2
exec nice -n 10 "$@"
