#!/usr/bin/env bash
# Fetch sample assets into build/ for the demos and tests. Tests skip
# gracefully when assets are missing.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Khronos glTF sample models (examples/model, render/gltf tests).
OUT="$ROOT/build/assets"
mkdir -p "$OUT"
BASE="https://raw.githubusercontent.com/KhronosGroup/glTF-Sample-Assets/main/Models"
for m in "Duck/glTF-Binary/Duck.glb" "BoxTextured/glTF-Binary/BoxTextured.glb"; do
  name="$(basename "$m")"
  [ -f "$OUT/$name" ] || curl -sfL -o "$OUT/$name" "$BASE/$m"
  echo "$OUT/$name"
done

# Poly Haven CC0 PBR texture sets for examples/materials. Each set lands in
# build/textures/<name>_1k/ as <name>_{diff,nor_gl,arm}_1k.jpg, the layout
# the example (and GFX_TEXTURES) expects.
TEX="$ROOT/build/textures"
PH="https://dl.polyhaven.org/file/ph-assets/Textures/jpg/1k"
for t in red_brick rocky_terrain damaged_plaster cracked_concrete_wall metal_plate brick_pavement_02; do
  mkdir -p "$TEX/${t}_1k"
  for m in diff nor_gl arm; do
    f="$TEX/${t}_1k/${t}_${m}_1k.jpg"
    [ -s "$f" ] || curl -sfL -o "$f" "$PH/$t/${t}_${m}_1k.jpg" || echo "missing $t $m" >&2
  done
  echo "$TEX/${t}_1k"
done

# Skinned+animated sample character for examples/skinned.
MODELS="$ROOT/build/models"
mkdir -p "$MODELS"
CM="$MODELS/CesiumMan.glb"
[ -s "$CM" ] || curl -sfL -o "$CM" \
  "https://github.com/KhronosGroup/glTF-Sample-Assets/raw/main/Models/CesiumMan/glTF-Binary/CesiumMan.glb" \
  && echo "CesiumMan ready in $MODELS"
