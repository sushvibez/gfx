#!/usr/bin/env bash
# BOUNDED performance measurement for one corpus page, one engine.
#
#   scripts/qml-perf.sh gml buttons 8      # our .gml runtime, 8 seconds
#   scripts/qml-perf.sh go  buttons 8      # the Go port
#
# Every run is fenced, because an unfenced one took the desktop down with
# it (a build-cache experiment reached 5.5GB of resident set and pushed
# the session into swap):
#   - resident memory capped by a cgroup (scripts/bounded.sh), so a
#     runaway allocation is OOM-killed instead of swapping the machine
#   - CPU seconds capped (ulimit -t), so a spin cannot run away
#   - wall clock capped (timeout, then SIGKILL), in the FOREGROUND — no
#     nohup, nothing outliving the command
#   - one engine at a time, and the run kills its own leftovers at exit
#
# Knobs: BOUND_MEM (default 6G, via bounded.sh), PERF_CPU_S (default 60).
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CORPUS="${CORPUS:-$HOME/workspace/qml-examples}"
OUT="${OUT:-$ROOT/build/qml-compare}"
side="${1:?usage: qml-perf.sh <gml|go> <page> [seconds]}"
page="${2:?usage: qml-perf.sh <gml|go> <page> [seconds]}"
secs="${3:-8}"
export DISPLAY="${GFX_REF_DISPLAY:-:1}"
export GFX_WGPU_LIB="$ROOT/build/wgpu/libwgpu_native.so"
export GFX_FPS=1

bin=""
case "$side" in
  gml) (cd "$ROOT/examples" && go build -o /tmp/gfx-perf-gml ./qml) || exit 1
       bin=/tmp/gfx-perf-gml
       args=(-file "$CORPUS/examples/$page/Main.qml" -w 900 -h 700)
       export GFX_QSB_PATH="$OUT/examples-ref/$page" ;;
  go)  [ -d "$ROOT/examples/qmlgo/$page" ] || { echo "perf: no Go port for $page"; exit 1; }
       (cd "$ROOT/examples" && go build -o /tmp/gfx-perf-go "./qmlgo/$page") || exit 1
       bin=/tmp/gfx-perf-go; args=() ;;
  *)   echo "perf: side must be gml or go"; exit 1 ;;
esac

log="$(mktemp /tmp/gfx-perf.XXXXXX.log)"
# Kill by PID, never by pattern: `pkill -f <binary path>` also matched the
# CALLER whose command line named the binary, and took it down.
child=""
trap '[ -n "$child" ] && kill "$child" 2>/dev/null; rm -f "$log"' EXIT
# The fence. ulimit is per-process and inherited, so it covers the render
# thread too; SIGKILL after a 5s grace in case the app ignores the term.
(
  ulimit -t "${PERF_CPU_S:-60}"                  # CPU seconds
  exec "$ROOT/scripts/bounded.sh" timeout -k 5 "$secs" "$bin" "${args[@]}"
) >"$log" 2>&1 &
child=$!
wait "$child"; rc=$?; child=""
echo "== $side / $page (${secs}s, rc=$rc)"
grep "^shell:" "$log" | tail -4
grep -iv "^shell:" "$log" | grep -i "error\|panic\|out of memory\|fatal" | head -3
