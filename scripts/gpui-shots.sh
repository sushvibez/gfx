#!/usr/bin/env bash
# Render every ported GPUI example headlessly into build/shots/ for
# side-by-side comparison with the originals in
# zed-industries/zed crates/gpui/examples.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/build/shots"
mkdir -p "$OUT"
cd "$ROOT/examples"
for ex in $(go run ./gpui -list); do
  go run ./gpui -example "$ex" -screenshot "$OUT/gpui-$ex.png" >/dev/null
  echo "$OUT/gpui-$ex.png"
done
