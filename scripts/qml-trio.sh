#!/usr/bin/env bash
# Three live windows of the SAME corpus page, side by side on one screen:
# real Qt, our gml engine, and a hand-written Go port of the page. The
# point is what a screenshot cannot show — interaction — and whether the
# Go builder API can express what the .gml does (feature parity between
# the two front ends over the same widgets).
#
#   scripts/qml-trio.sh buttons     # launch the three, place, measure
#   scripts/qml-trio.sh --kill      # close them again
#
# Knobs: QT_QML, CORPUS, OUT (as in qml-compare.sh), TRIO_SECS (sample
# window), TRIO_Y (row's top edge), TRIO_W/TRIO_H (window size — a
# taller window keeps a page's animating item on screen; the default
# 900x700 cuts the buttons page's pulsing ring off below the fold, and Qt
# then renders one frame a second while ours still do the whole frame).
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
QT_QML="${QT_QML:-$HOME/Qt/6.10.1/gcc_64/bin/qml}"
CORPUS="${CORPUS:-$HOME/workspace/qml-examples}"
OUT="${OUT:-$ROOT/build/qml-compare}"
PIDFILE=/tmp/gfx-trio.pids
W="${TRIO_W:-900}"; H="${TRIO_H:-700}"
SECS="${TRIO_SECS:-8}"
Y="${TRIO_Y:-40}"
export DISPLAY="${GFX_REF_DISPLAY:-:1}"

kill_all() {
  [ -f "$PIDFILE" ] || { echo "trio: nothing running"; return; }
  while read -r pid _; do kill "$pid" 2>/dev/null; done < "$PIDFILE"
  sleep 1
  while read -r pid _; do kill -9 "$pid" 2>/dev/null; done < "$PIDFILE"
  rm -f "$PIDFILE"
  echo "trio: closed"
}

[ "${1:-}" = "--kill" ] && { kill_all; exit 0; }
n="${1:?usage: qml-trio.sh <page> | --kill}"
kill_all >/dev/null 2>&1

# OURS loads the page out of the CORPUS, not out of the harness' copy:
# our engine resolves `import utils` by walking UP from the file looking
# for an imports/ root, and the copy under build/ has no such ancestor
# (the page then renders black — every Style.* is an unknown identifier).
# The copy is still what Qt gets, because it carries the compiled .qsb
# shaders the corpus ships only as GLSL.
page="$CORPUS/examples/$n/Main.qml"
[ -f "$page" ] || { echo "trio: no such page: $n"; exit 1; }
refpage="$OUT/examples-ref/$n/Main.qml"
[ -f "$refpage" ] || refpage="$page"

# The page is not a window and does not show itself: every Main.qml in the
# corpus is an Item at `opacity: fullSize ? 1.0 : 0.0`, and the app's
# Dashboard sets fullSize on the Loader that brought it in. Loaded raw, a
# page is perfectly transparent — Qt showed a white window. So Qt gets the
# same host the compare harness builds, minus the grab and the quit.
host="$OUT/hosts/trio-$n.qml"
mkdir -p "$OUT/hosts"
cat > "$host" <<EOF
import QtQuick
import QtQuick.Window
Window {
    id: host
    width: $W; height: $H; visible: true
    title: "Qt: $n"
    color: "#2b2d31"
    Loader {
        anchors.fill: parent
        source: "$refpage"
        onLoaded: if (item && "fullSize" in item) item.fullSize = true
    }
}
EOF
STYLE="$(sed -n 's/^ *Style *= *//p' "$CORPUS/qtquickcontrols2.conf" 2>/dev/null | tr -d '\r' | head -1)"

# --- ours, and the Go port when one exists, built before anything opens so
# a compile does not count as the app's startup cost.
export GFX_WGPU_LIB="$ROOT/build/wgpu/libwgpu_native.so"
(cd "$ROOT/examples" && go build -o /tmp/gfx-trio-qml ./qml) || exit 1
goport=""
if [ -d "$ROOT/examples/qmlgo/$n" ]; then
  (cd "$ROOT/examples" && go build -o /tmp/gfx-trio-go "./qmlgo/$n") && goport=/tmp/gfx-trio-go
fi

: > "$PIDFILE"
# Each side runs in its own memory fence (scripts/bounded.sh): three live
# renderers on one desktop are exactly the load that once swapped it.
start() { # name cmd...
  local name="$1"; shift
  BOUND_MEM="${TRIO_MEM:-3G}" BOUND_CPU=300% "$ROOT/scripts/bounded.sh" "$@" >"/tmp/gfx-trio-$name.log" 2>&1 &
  echo "$! $name" >> "$PIDFILE"
}

# Every side logs its frames: Qt through its scene-graph timing category
# (one line per rendered frame, with its sync/render/swap milliseconds),
# ours through GFX_FPS. The cost table below divides CPU by REAL frames.
start qt env QT_QUICK_CONTROLS_STYLE="${QT_QUICK_CONTROLS_STYLE:-$STYLE}" \
  QT_QUICK_CONTROLS_STYLE_PATH="$CORPUS/styles" \
  QML_IMPORT_PATH="$CORPUS/styles:$OUT/imports-ref:$OUT/shim" \
  QSG_RENDER_TIMING=1 QT_LOGGING_RULES="qt.scenegraph.time.renderloop=true" \
  "$QT_QML" -I "$OUT/imports-ref" -I "$CORPUS/styles" -I "$OUT/shim" "$host"
start gml env GFX_QSB_PATH="$OUT/examples-ref/$n" GFX_FPS=1 \
  /tmp/gfx-trio-qml -file "$page" -w $W -h $H
[ -n "$goport" ] && start go env GFX_FPS=1 "$goport"

# --- place them in a row. Match windows to the PIDs that own them rather
# than to titles: all three name themselves differently and one of them
# (the bare qml tool) names itself after the file.
sleep 3
i=0
while read -r pid name; do
  x=$(( i * (W + 20) )); i=$((i+1))
  win="$(wmctrl -lp | awk -v p="$pid" '$3==p {print $1; exit}')"
  # Our X11 backend sets no _NET_WM_PID, so fall back to the window each
  # side names itself.
  if [ -z "$win" ]; then
    case "$name" in
      qt) pat="Qt: $n" ;;
      gml) pat="graphics: gml" ;;
      go) pat="go: " ;;
    esac
    win="$(wmctrl -l | grep -F "$pat" | awk '{print $1; exit}')"
  fi
  [ -z "$win" ] && { echo "trio: $name ($pid) opened no window — see /tmp/gfx-trio-$name.log"; continue; }
  wmctrl -i -r "$win" -b remove,maximized_vert,maximized_horz 2>/dev/null
  wmctrl -i -r "$win" -e "0,$x,$Y,$W,$H"
done < "$PIDFILE"

# Frames each side has rendered so far, from its own log (Qt 6 logs one
# render-thread line per frame; ours print fps once a second).
frames_now() { # name
  case "$1" in
    qt) grep -c "frame rendered in" "/tmp/gfx-trio-qt.log" 2>/dev/null ;;
    *)  grep -h "^shell: [0-9]* fps" "/tmp/gfx-trio-$1.log" 2>/dev/null | awk '{s+=$2} END{print s+0}' ;;
  esac
}
# --- cost. CPU% is over the sample window (not since boot), RSS is the
# process' own resident set, and GPU memory comes from the driver's
# per-process accounting, which counts a graphics context's framebuffers.
jiffies() { awk '{print $14+$15}' "/proc/$1/stat" 2>/dev/null; }
declare -A t0 f0
while read -r pid name; do t0[$pid]="$(jiffies "$pid")"; done < "$PIDFILE"
# frames()/gpu_util() are defined below; bash resolves them at call time.
declare -f frames >/dev/null 2>&1 || true
while read -r pid name; do f0[$name]="$(frames_now "$name")"; done < "$PIDFILE"
sleep "$SECS"
hz=$(getconf CLK_TCK)
gpumem() { nvidia-smi --query-compute-apps=pid,used_memory --format=csv,noheader,nounits 2>/dev/null |
             awk -F', ' -v p="$1" '$1==p {print $2; f=1} END {if(!f) print ""}'; }
gpumem_g() { nvidia-smi 2>/dev/null | awk -v p="$1" '$5==p {g=$(NF-1); sub("MiB","",g); print g}' | head -1; }
# Frames each side rendered during the window, from its own log.
frames() { frames_now "$1"; }
# Whole-GPU busy over the window: the driver does not report per-process
# utilisation for graphics contexts, so this is all three sides plus the
# desktop together.
gpu_util() { nvidia-smi --query-gpu=utilization.gpu --format=csv,noheader,nounits 2>/dev/null | head -1; }
gu0="$(gpu_util)"
# GPU busy per process (SM %), the driver's per-process sample.
gpusm() { nvidia-smi pmon -c 1 -s u 2>/dev/null | awk -v p="$1" '$2==p {print $4; f=1} END{if(!f) print "-"}'; }
printf '\n%-5s %-8s %6s %8s %8s %6s %13s\n' side pid 'cpu%' 'ram MiB' 'gpu MiB' fps 'cpu ms/frame'
while read -r pid name; do
  t1="$(jiffies "$pid")"
  if [ -z "$t1" ]; then printf '%-5s %-8s %6s\n' "$name" "$pid" gone; continue; fi
  cpu="$(awk -v a="${t0[$pid]:-0}" -v b="$t1" -v s="$SECS" -v hz="$hz" 'BEGIN{printf "%.1f", (b-a)*100/(hz*s)}')"
  rss="$(awk '/VmRSS/{printf "%.0f", $2/1024}' "/proc/$pid/status" 2>/dev/null)"
  g="$(gpumem "$pid")"; [ -z "$g" ] && g="$(gpumem_g "$pid")"; [ -z "$g" ] && g="-"
  fr=$(( $(frames "$name") - ${f0[$name]:-0} )); fps="$(awk -v f="$fr" -v s="$SECS" 'BEGIN{printf "%.0f", f/s}')"
  msf="-"; [ "$fr" -gt 0 ] && msf="$(awk -v c="$cpu" -v f="$fr" -v s="$SECS" 'BEGIN{printf "%.2f", c/100*s*1000/f}')"
  printf '%-5s %-8s %6s %8s %8s %6s %13s\n' "$name" "$pid" "$cpu" "${rss:--}" "$g" "$fps" "$msf"
done < "$PIDFILE"
echo "gpu busy (whole GPU, all sides + desktop): ${gu0}% -> $(gpu_util)%"
# Qt's own per-frame timing, averaged over the window (sync+render+swap).
if grep -q "frame rendered in" /tmp/gfx-trio-qt.log 2>/dev/null; then
  tail -n 2000 /tmp/gfx-trio-qt.log | grep "frame rendered in" | \
    sed -n 's/.* in \([0-9]*\)ms, sync=\([0-9]*\), render=\([0-9]*\), swap=\([0-9]*\).*/\1 \2 \3 \4/p' | \
    awk '{n++; t+=$1; s+=$2; r+=$3; w+=$4} END{if(n) printf "qt render thread (own timing, last %d frames): %.2f ms/frame (sync %.2f, render %.2f, swap %.2f) — whole milliseconds, so mostly 0\n", n, t/n, s/n, r/n, w/n}'
fi
echo
echo "trio: $n is up — interact with all three; scripts/qml-trio.sh --kill closes them."
