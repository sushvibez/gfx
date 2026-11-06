#!/usr/bin/env bash
# Side-by-side parity renders for the QML-Examples-Collection corpus:
# each examples/<name>/Main.qml page rendered by real Qt (offscreen,
# grabToImage) next to our gml engine (examples/qml -screenshot).
#
#   scripts/qml-compare.sh            # all pages -> build/qml-compare/
#   scripts/qml-compare.sh buttons    # one page
#
# Knobs: QT_QML (qml runtime), CORPUS (corpus checkout), OUT (output dir).
# See script/gml/check/QML-EXAMPLES.md for corpus status and notes.
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
QT_QML="${QT_QML:-$HOME/Qt/6.10.1/gcc_64/bin/qml}"
CORPUS="${CORPUS:-$HOME/workspace/qml-examples}"
OUT="${OUT:-$ROOT/build/qml-compare}"
W=900
H=700

# Pages whose reference needs a real GPU: they draw through layer.enabled,
# a ShaderEffect, QtQuick.Particles or QtQuick3D, none of which the software
# scene graph renders (it draws a layered item as NOTHING). They open a
# window on GFX_REF_DISPLAY (default :1) for a couple of seconds.
GPU_PAGES="aircraftmap animations cards2d5 diorama frostglass graphics3d images lensphoto lenstabs particles portal shaders spritestack switches transforms visualeffects"

if [ ! -x "$QT_QML" ]; then echo "qml-compare: qml runtime not found at $QT_QML (set QT_QML)"; exit 1; fi

# The pointer parker (see the GPU branch below), built once.
XWARP="$(mktemp -u /tmp/gfx-xwarp.XXXXXX)"
(cd "$ROOT/examples" && go build -o "$XWARP" ./xwarp) || XWARP=/bin/true
trap 'rm -f "$XWARP"' EXIT
if [ ! -d "$CORPUS/examples" ]; then echo "qml-compare: corpus not found at $CORPUS (set CORPUS)"; exit 1; fi
mkdir -p "$OUT/ref" "$OUT/ours" "$OUT/pairs" "$OUT/hosts"

# --- Qt5Compat.GraphicalEffects shim: pass-through stubs so pages load in
# kits without the compat module (effects drop, content renders).
SHIM="$OUT/shim/Qt5Compat/GraphicalEffects"
mkdir -p "$SHIM"
printf 'module Qt5Compat.GraphicalEffects\n' > "$SHIM/qmldir"
for t in DropShadow ColorOverlay Glow GaussianBlur InnerShadow Colorize Desaturate LinearGradient RadialGradient OpacityMask FastBlur; do
  cat > "$SHIM/$t.qml" <<'EOF'
import QtQuick
Item { property var source; property var color; property real radius: 0; property real samples: 0; property real horizontalOffset: 0; property real verticalOffset: 0; property real spread: 0; property var maskSource; property real desaturation: 0; property var hue; property var saturation; property var lightness; property bool cached: false; property bool transparentBorder: false }
EOF
  printf '%s 1.0 %s.qml\n' "$t" "$t" >> "$SHIM/qmldir"
done

# --- fonts: the corpus loads Quicksand through FontLoader with qrc-style
# absolute paths ("/assets/fonts/..."), which resolve only inside the built
# executable's resource tree. The bare `qml` tool resolves them against the
# filesystem root, the loaders fail, and every page renders in Qt's default
# family — so the reference would differ from ours by its FONT everywhere,
# drowning out the differences worth looking at. Patch a private copy of the
# corpus' imports to point at the real files and put it first on the import
# path.
IMPORTS="$OUT/imports-ref"
rm -rf "$IMPORTS"
mkdir -p "$IMPORTS"
cp -r "$CORPUS/imports/." "$IMPORTS/" 2>/dev/null
# Every asset path in the corpus funnels through this one file: the two
# FontLoaders plus iconPath/imagePath/meshPath.
if [ -f "$IMPORTS/utils/Style.qml" ]; then
  sed -i "s|\"/assets/|\"file://$CORPUS/imports/assets/|g" "$IMPORTS/utils/Style.qml"
fi

# Some pages name an asset by a qrc path of their own ("qrc:/assets/images/
# particle.png" for an ImageParticle's texture), which only exists inside the
# built executable: the bare `qml` tool cannot open it and the element draws
# NOTHING (every particle card came out empty). Reference-side only — patch a
# copy of the pages the same way as the imports, and load the reference from
# it. Ours reads the corpus untouched.
PAGES_REF="$OUT/examples-ref"
rm -rf "$PAGES_REF"
mkdir -p "$PAGES_REF"
cp -r "$CORPUS/examples/." "$PAGES_REF/" 2>/dev/null
find "$PAGES_REF" -name '*.qml' -exec \
  sed -i "s|qrc:/assets/|file://$CORPUS/imports/assets/|g; s|\"/assets/|\"file://$CORPUS/imports/assets/|g" {} +

# A ShaderEffect names its shaders as "qrc:/qt/qml/<module>/shaders/x.qsb",
# built by CMake from the .vert/.frag sources the corpus ships. The bare tool
# finds no such resource ("Failed to find shader"), so the effect draws
# nothing — the lens/frost pages' references were missing whole panels.
# Compile them here with Qt's own qsb and point the copy at the files.
# A qmldir's "prefer :/qt/qml/<module>/" line tells Qt to load the module's
# components from the qrc RESOURCE, which only the built executable has: the
# bare tool then reports every sibling card as "Type unavailable" and the
# page's reference is blank. Drop it from the copy.
find "$PAGES_REF" -name qmldir -exec sed -i '/^prefer /d' {} +

QSB="$(dirname "$QT_QML")/qsb"
if [ -x "$QSB" ]; then
  find "$PAGES_REF" \( -name '*.vert' -o -name '*.frag' \) -print | while read -r sh; do
    "$QSB" --glsl 120,150,300es --hlsl 50 --msl 12 -o "$sh.qsb" "$sh" >/dev/null 2>&1
  done
  find "$PAGES_REF" -name '*.qml' -exec \
    sed -i "s|qrc:/qt/qml/|file://$PAGES_REF/|g" {} +
fi

names=("$@")
if [ ${#names[@]} -eq 0 ]; then
  for d in "$CORPUS"/examples/*/; do
    [ -f "$d/Main.qml" ] && names+=("$(basename "$d")")
  done
fi

for n in "${names[@]}"; do
  page="$CORPUS/examples/$n/Main.qml"
  [ -f "$page" ] || { echo "skip $n (no Main.qml)"; continue; }

  # Reference: a Window host that loads the page like the app's Dashboard
  # does (sets fullSize), then grabs itself — headless via the offscreen
  # platform, so nothing opens on the desktop.
  # The reference loads the PATCHED copy of the page (see PAGES_REF).
  refpage="$PAGES_REF/$n/Main.qml"
  [ -f "$refpage" ] || refpage="$page"
  host="$OUT/hosts/$n.qml"
  cat > "$host" <<EOF
import QtQuick
import QtQuick.Window
Window {
    id: host
    width: $W; height: $H; visible: true
    color: "#2b2d31"
    Loader {
        id: page
        anchors.fill: parent
        active: false
        source: "$refpage"
        onLoaded: {
            if (item && "fullSize" in item) item.fullSize = true
            host.loads++
            if (host.loads == 1) {
                // INSTANTIATE THE PAGE TWICE. The first instantiation is
                // what compiles this page's own shaders and effect
                // pipelines, and those frames run long: a page that clocks
                // itself off frameTime with the corpus' clamp
                // (Math.min(frameTime, 0.05)) throws the excess away, so
                // its own clock ended half a second behind the declarative
                // animations in the SAME page (the portal's swirl sat at
                // 2.03s while its tweens were at 2.5). Qt caches the
                // compiled pipelines per process, so the second
                // instantiation starts on a warm cache and every clock in
                // the page keeps up.
                reload.start()
                return
            }
            host.started = true
            host.animT = 0
            host.repaintCanvases(item)
            phaseAnim.restart()   // the same clock the page's tweens use
        }
    }
    // WARM THE WINDOW BEFORE THE PAGE EXISTS. Mapping a window, building
    // the scene graph and compiling the first shaders makes the early
    // frames long: a page that caps its own step (Math.min(frameTime,
    // 0.05), the corpus idiom) loses the excess of every one of them,
    // while a declarative animation does not — so the two kinds of page
    // ended up at different times, and the drift varied per run (three
    // captures of graphics3d used to differ by up to Delta 12). Warming
    // up with the Loader INACTIVE means the page is instantiated onto a
    // warm scene and every clock it owns starts together, which is the
    // state our headless capture is in from its first step.
    property int warm: 0
    property int loads: 0
    property bool started: false
    property real animT: 0
    property bool grabbed: false
    // The grab fires off an ANIMATION, not off summed frame times. Both
    // measure 2.5 seconds, but only the animation shares its clock with
    // the page's own animations: summing frameTime missed the long frame
    // that instantiates the page (its cost reaches the page's tweens but
    // not the sum), so a spinning cube was grabbed at 2.92s of animation
    // while the sum said 2.5 -- 37 degrees of rotation past our headless
    // capture, which advances exactly 2.5s.
    property real phase: 0
    NumberAnimation on phase {
        id: phaseAnim
        running: false
        from: 0; to: 1; duration: 2500
    }
    onPhaseChanged: if (host.phase >= 1 && !host.grabbed) {
        host.grabbed = true
        host.contentItem.grabToImage(function(result) {
            result.saveToFile("$OUT/ref/$n.png")
            Qt.quit()
        })
    }
    FrameAnimation {
        running: true
        onTriggered: {
            if (!host.started) {
                if (frameTime < 0.025) { host.warm++ } else { host.warm = 0 }
                host.animT += frameTime
                if (host.warm >= 3 || host.animT > 3.0) {
                    page.active = true   // onLoaded starts the clocks
                }
                return
            }
            host.animT += frameTime   // reported only; the grab is on phase
        }
    }
    // A Canvas paints on `onAvailableChanged: if (available) requestPaint()`
    // (the corpus idiom), and on the SECOND instantiation the target can
    // already be available when the handler is connected — so the signal
    // never comes and the tapes/dials stay blank. Ask every Canvas in the
    // page to paint once; its content does not depend on the clock, so
    // this changes nothing but whether it is there at all.
    function repaintCanvases(obj) {
        if (!obj)
            return
        if (typeof obj.requestPaint === "function")
            obj.requestPaint()
        var kids = obj.children
        if (kids)
            for (var i = 0; i < kids.length; ++i)
                host.repaintCanvases(kids[i])
        if (obj.contentItem)
            host.repaintCanvases(obj.contentItem)
    }
    // The re-instantiation: drop the page and build it again, a few frames
    // later so the discarded one has left the scene graph.
    Timer {
        id: reload
        interval: 800; repeat: false
        onTriggered: { page.active = false; page.active = true }
    }
    // Backstop: a page that never renders a frame (a load error) would
    // never grab at all.
    Timer {
        interval: 8000; running: true
        onTriggered: if (!host.grabbed) {
            host.grabbed = true
            host.contentItem.grabToImage(function(result) {
                result.saveToFile("$OUT/ref/$n.png")
                Qt.quit()
            })
        }
    }
}
EOF
  # The app selects its Controls style application-wide through
  # qtquickcontrols2.conf (Style=...), which is embedded as a Qt resource in
  # the real executable — the bare `qml` tool never sees it, so pass the
  # same style through the environment or the reference renders in Basic.
  # (tr strips the conf's CRLF line endings, which Qt would take as part of
  # the style name.)
  STYLE="$(sed -n 's/^ *Style *= *//p' "$CORPUS/qtquickcontrols2.conf" 2>/dev/null | tr -d '\r' | head -1)"
  # GFX_REF_GPU=1 renders the reference on a REAL display through Qt's GL
  # backend instead of the offscreen/software one. That matters for any page
  # built on `layer.enabled` or a ShaderEffect: the software scene graph
  # draws a layered item as NOTHING, so the default reference for the
  # effect pages (diorama, frostglass, graphics3d, lenstabs, cards2d5) is
  # blank where Qt on a GPU draws the effect. It is opt-in because it opens
  # a real window on GFX_REF_DISPLAY (default :1) for a couple of seconds.
  # Which backend the REFERENCE needs. The software scene graph (what the
  # offscreen platform always loads) cannot draw a layered item, a
  # ShaderEffect, QtQuick.Particles or QtQuick3D — those pages need a real
  # GPU, on a real display. Everything else stays on the offscreen backend:
  # it needs no window, and the X11 platform picks up the DESKTOP's palette,
  # which shifts every Basic control's greys (an unchecked TabButton reads
  # 221 there and 159 offscreen) and would make the comparison about the
  # theme rather than about us. GFX_REF_GPU=1 forces GPU for every page.
  gpu=""
  case " $GPU_PAGES " in *" $n "*) gpu=1 ;; esac
  [ -n "${GFX_REF_GPU:-}" ] && gpu=1
  if [ -n "$gpu" ]; then
    # Park the desktop POINTER in a corner first. A real window on a real
    # display gets the actual mouse, so a page that follows `mouseX` (the
    # optical lens) or highlights on hover (a depth card) rendered whatever
    # the pointer happened to be doing — the lensphoto lens sat wherever
    # the mouse was, and cards2d5' first card came out hovered. Our
    # headless capture has no pointer at all, so a corner is the state both
    # sides agree on.
    "$XWARP" -display "${GFX_REF_DISPLAY:-:1}" -x 0 -y 0 2>/dev/null || true
    DISPLAY="${GFX_REF_DISPLAY:-:1}" \
      QT_QUICK_CONTROLS_STYLE="${QT_QUICK_CONTROLS_STYLE:-$STYLE}" \
      QT_QUICK_CONTROLS_STYLE_PATH="$CORPUS/styles" \
      QML_IMPORT_PATH="$CORPUS/styles:$IMPORTS:$OUT/shim" \
      timeout 60 "$QT_QML" -I "$IMPORTS" -I "$CORPUS/styles" -I "$OUT/shim" \
      "$host" > "$OUT/ref/$n.log" 2>&1
  else
    QT_QPA_PLATFORM=offscreen QT_QUICK_BACKEND=software \
      QT_QUICK_CONTROLS_STYLE="${QT_QUICK_CONTROLS_STYLE:-$STYLE}" \
      QT_QUICK_CONTROLS_STYLE_PATH="$CORPUS/styles" \
      QML_IMPORT_PATH="$CORPUS/styles:$IMPORTS:$OUT/shim" \
      timeout 30 "$QT_QML" -I "$IMPORTS" -I "$CORPUS/styles" -I "$OUT/shim" \
      "$host" > "$OUT/ref/$n.log" 2>&1
  fi

  # Ours. A Qt app ships its shaders COMPILED (its build turns the .vert/
  # .frag into .qsb inside the resource tree) and so does our ShaderEffect
  # path: point it at the copies the harness compiled above, since the
  # corpus itself carries only the GLSL sources.
  (cd "$ROOT/examples" && GFX_QSB_PATH="$PAGES_REF/$n" timeout 30 go run ./qml -file "$page" -w $W -h $H \
    -screenshot "$OUT/ours/$n.png" > "$OUT/ours/$n.log" 2>&1)

  r=""; o=""
  [ -s "$OUT/ref/$n.png" ] && r=ref || r=MISSING-REF
  [ -s "$OUT/ours/$n.png" ] && o=ours || o=MISSING-OURS
  echo "$n: $r $o"
done

# --- compose labeled pairs + a contact sheet -----------------------------
python3 - "$OUT" $W $H "${names[@]}" <<'EOF'
import os, sys
from PIL import Image, ImageDraw

out, w, h = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
names = sys.argv[4:]
label_h = 26
pairs = []
for n in names:
    ref_p, ours_p = f"{out}/ref/{n}.png", f"{out}/ours/{n}.png"
    pair = Image.new("RGB", (w * 2 + 12, h + label_h), "#101114")
    d = ImageDraw.Draw(pair)
    d.text((8, 6), f"{n} — Qt (reference)", fill="#9aa0ac")
    d.text((w + 20, 6), "gml (ours)", fill="#9aa0ac")
    for i, p in enumerate((ref_p, ours_p)):
        if os.path.exists(p):
            im = Image.open(p).convert("RGB")
            if im.size != (w, h):
                im = im.resize((w, h))
            pair.paste(im, (i * (w + 12), label_h))
    pair.save(f"{out}/pairs/{n}.png")
    pairs.append(n)

# Contact sheet: every pair scaled down, stacked in a grid.
cols = 2
tw = 720
th = int(tw * (h + label_h) / (w * 2 + 12))
rows = (len(pairs) + cols - 1) // cols
sheet = Image.new("RGB", (cols * (tw + 8) + 8, rows * (th + 8) + 8), "#101114")
for i, n in enumerate(sorted(pairs)):
    im = Image.open(f"{out}/pairs/{n}.png").resize((tw, th))
    sheet.paste(im, (8 + (i % cols) * (tw + 8), 8 + (i // cols) * (th + 8)))
sheet.save(f"{out}/contact-sheet.png")
print(f"wrote {len(pairs)} pairs + contact-sheet -> {out}")
EOF
