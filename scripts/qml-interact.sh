#!/usr/bin/env bash
# Interaction parity: play the SAME script of clicks/drags/keys against a
# corpus page in real Qt and in our gml engine, capturing a PNG per step
# on both sides, then pair them up.
#
#   scripts/qml-interact.sh tabbar script/gml/check/interact/tabbar.txt
#   scripts/qml-interact.sh sliders            # uses check/interact/<name>.txt
#
# A static screenshot only proves a page loads right. These prove the page
# still matches once touched — pressed states, selection, scroll position,
# what a transition lands on.
#
# Knobs: QT_QML/QT_TESTRUNNER (Qt tools), CORPUS, OUT.
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
QTBIN="${QTBIN:-$HOME/Qt/6.10.1/gcc_64/bin}"
RUNNER="${QT_TESTRUNNER:-$QTBIN/qmltestrunner}"
CORPUS="${CORPUS:-$HOME/workspace/qml-examples}"
OUT="${OUT:-$ROOT/build/qml-interact}"
W=900
H=700

n="${1:-}"
[ -n "$n" ] || { echo "usage: qml-interact.sh NAME [SCRIPT]"; exit 1; }
SCRIPT="${2:-$ROOT/script/gml/check/interact/$n.txt}"
case "$SCRIPT" in /*) ;; *) SCRIPT="$PWD/$SCRIPT" ;; esac  # ours runs from examples/
page="$CORPUS/examples/$n/Main.qml"
[ -x "$RUNNER" ] || { echo "qml-interact: qmltestrunner not found at $RUNNER"; exit 1; }
[ -f "$page" ] || { echo "qml-interact: no page at $page"; exit 1; }
[ -f "$SCRIPT" ] || { echo "qml-interact: no script at $SCRIPT"; exit 1; }

rm -rf "$OUT/ref/$n" "$OUT/ours/$n" "$OUT/pairs/$n"
mkdir -p "$OUT/ref/$n" "$OUT/ours/$n" "$OUT/pairs/$n" "$OUT/hosts"

# The compare harness already builds the shim + patched-font imports the
# corpus needs; reuse them so the two runs differ only in the engine.
COMPARE="${COMPARE_OUT:-$ROOT/build/qml-compare}"
if [ ! -d "$COMPARE/imports-ref" ]; then
  echo "qml-interact: run scripts/qml-compare.sh $n once first (needs its imports/shim)"; exit 1
fi

# --- Qt side: the step list becomes a TestCase, which is the only headless
# way to get REAL synthesised input (mouseClick/mouseDrag/keyClick) rather
# than poking properties.
host="$OUT/hosts/tst_$n.qml"
python3 "$ROOT/scripts/qml_interact_gen.py" "$SCRIPT" "$page" "$OUT/ref/$n" > "$host" || exit 1

STYLE="$(sed -n 's/^ *Style *= *//p' "$CORPUS/qtquickcontrols2.conf" 2>/dev/null | tr -d '\r' | head -1)"
QT_QPA_PLATFORM=offscreen QT_QUICK_BACKEND=software \
  QT_QUICK_CONTROLS_STYLE="${QT_QUICK_CONTROLS_STYLE:-$STYLE}" \
  QT_QUICK_CONTROLS_STYLE_PATH="$CORPUS/styles" \
  QML_IMPORT_PATH="$CORPUS/styles:$COMPARE/imports-ref:$COMPARE/shim" \
  timeout 120 "$RUNNER" -import "$COMPARE/imports-ref" -import "$CORPUS/styles" \
  -import "$COMPARE/shim" -input "$host" > "$OUT/ref/$n.log" 2>&1

# --- ours: the same file, played by examples/qml -script.
(cd "$ROOT/examples" && timeout 120 go run ./qml -file "$page" -w $W -h $H \
  -screenshot "$OUT/ours/$n/00.png" -script "@$SCRIPT" > "$OUT/ours/$n.log" 2>&1)

python3 "$ROOT/scripts/qml_interact_pairs.py" "$OUT" "$n" "$SCRIPT" $W $H
