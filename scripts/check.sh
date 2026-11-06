#!/usr/bin/env bash
# The full verification sweep: format, build+vet for every platform,
# all tests, and headless screenshot smoke. CI runs this; so should you.
set -euo pipefail
# GFX_TEST_FLAGS adds flags to every go test and go run below, e.g.
# GFX_TEST_FLAGS=-ldflags=-s on a Windows machine whose application-control
# policy blocks some freshly linked test binaries (a stripped build is a
# different binary and gets through).
TF="${GFX_TEST_FLAGS:-}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "== gofmt"
unformatted=$(gofmt -l . | grep -v '^build/' || true)
if [ -n "$unformatted" ]; then echo "needs gofmt:"; echo "$unformatted"; exit 1; fi

# The framework is one module; examples/ is a second one (so the root
# module carries no demo-only dependencies) that replaces the root with ../.
MODULES=". examples"
for target in "linux amd64" "windows amd64" "js wasm"; do
  set -- $target
  echo "== build+vet GOOS=$1"
  for m in $MODULES; do
    (cd "$m" && GOOS=$1 GOARCH=$2 go build ./... && GOOS=$1 GOARCH=$2 go vet ./...)
  done
done

echo "== layering"
go test $TF -count=1 ./internal/layering/...

echo "== tests"
# The output is filtered but the result is not: piping go test straight
# into grep threw its exit status away, and a failing package went
# unnoticed in the sweep for days.
failed=""
for m in $MODULES; do
  if out=$( (cd "$m" && go test $TF -count=1 -timeout 300s ./...) 2>&1 ); then :; else failed="$failed $m"; fi
  echo "$out" | grep -v 'no test files' || true
done
if [ -n "$failed" ]; then echo "FAILED tests in:$failed"; exit 1; fi

echo "== headless screenshot smoke"
if [ -f "build/wgpu/libwgpu_native.so" ] || [ -f "build/wgpu/wgpu_native.dll" ] || [ -n "${GFX_WGPU_LIB:-}" ]; then
  tmp=$(mktemp -d)
  (cd examples && go run $TF ./settings -screenshot "$tmp/settings.png" >/dev/null)
  (cd examples && go run $TF ./kitchensink -screenshot "$tmp/kitchensink.png" >/dev/null)
  # frost exercises the offscreen-layer path (a second render pass per frame).
  (cd examples && go run $TF ./frost -screenshot "$tmp/frost.png" >/dev/null)
  test -s "$tmp/settings.png" && test -s "$tmp/kitchensink.png" && test -s "$tmp/frost.png"
  rm -rf "$tmp"
  echo "screenshots ok"
else
  echo "wgpu-native missing; skipped (run scripts/fetch-wgpu.sh)"
fi
echo "== all checks passed"
