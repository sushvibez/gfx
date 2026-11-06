#!/usr/bin/env bash
# Build an example for the browser into examples/<name>/web/.
# Usage: scripts/build-web.sh [example]   (default: triangle)
# Serve that directory (e.g. `python3 -m http.server`) and open index.html.
set -euo pipefail

NAME="${1:-triangle}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WEB="$ROOT/examples/$NAME/web"

[ -d "$ROOT/examples/$NAME" ] || { echo "unknown example: $NAME" >&2; exit 1; }
mkdir -p "$WEB"
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$WEB/wasm_exec.js"
(cd "$ROOT/examples" && GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o "$WEB/main.wasm" "./$NAME")
ls -lh "$WEB"
