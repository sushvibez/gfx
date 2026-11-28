#!/usr/bin/env bash
# Fetch the pinned wgpu-native release into build/wgpu/ for local dev and CI.
# The gpu module's tests look for the library there (or in $GFX_WGPU_LIB).
set -euo pipefail

VERSION=v29.0.0.0
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/build/wgpu"

# The host by default. WGPU_OS / WGPU_ARCH fetch for somewhere else —
# packaging a Windows build on Linux needs the Windows DLL, and there is
# nothing host-specific about downloading it. A cross fetch lands in its
# own directory so it cannot overwrite the one this machine runs on.
case "${WGPU_OS:-$(uname -s)}" in
  Linux|linux)  os=linux ;;
  Darwin|macos) os=macos ;;
  MINGW*|MSYS*|CYGWIN*|windows) os=windows ;;
  *) echo "unsupported OS: ${WGPU_OS:-$(uname -s)}" >&2; exit 1 ;;
esac

case "${WGPU_ARCH:-$(uname -m)}" in
  x86_64|amd64) arch=x86_64 ;;
  aarch64|arm64) arch=aarch64 ;;
  *) echo "unsupported arch: ${WGPU_ARCH:-$(uname -m)}" >&2; exit 1 ;;
esac

if [ -n "${WGPU_OS:-}" ] || [ -n "${WGPU_ARCH:-}" ]; then
  DEST="$ROOT/build/wgpu-$os-$arch"
fi

# v29 renamed the Windows assets to carry an ABI infix; use the MSVC build.
# Linux/macOS keep the plain wgpu-$os-$arch-release.zip spelling.
if [ "$os" = "windows" ]; then
  asset="wgpu-$os-$arch-msvc-release.zip"
else
  asset="wgpu-$os-$arch-release.zip"
fi
url="https://github.com/gfx-rs/wgpu-native/releases/download/$VERSION/$asset"

rm -rf "$DEST"
mkdir -p "$DEST"
echo "fetching $url"
curl -sfL -o "$DEST/$asset" "$url"
unzip -o -q "$DEST/$asset" -d "$DEST"
rm "$DEST/$asset"
# v22+ release zips nest the library under lib/; flatten so the loader finds
# build/wgpu/libwgpu_native.{so,dll,a} at the top level (as LibPaths expects).
if [ -d "$DEST/lib" ]; then
  cp -f "$DEST/lib/"* "$DEST/" 2>/dev/null || true
fi
echo "$VERSION" > "$DEST/commit-sha"
echo "wgpu-native $VERSION ready in $DEST"

# Windows: the DirectX Shader Compiler beside the DLL. gpu (dxc.go) has DX12
# compile shaders with it when it is there — several times faster than FXC,
# wgpu's default (an app's pipelines in ~2-5 s rather than ~20 s). Only
# dxcompiler.dll (MIT / LLVM licences, shipped beside it) is needed: since
# v1.8.2502, wgpu's minimum, it no longer loads dxil.dll.
if [ "$os" = "windows" ]; then
  DXC_VERSION=v1.9.2609
  dxc_asset=dxc_2026_09_29.zip
  case "$arch" in
    x86_64) dxc_arch=x64 ;;
    aarch64) dxc_arch=arm64 ;;
  esac
  dxc_url="https://github.com/microsoft/DirectXShaderCompiler/releases/download/$DXC_VERSION/$dxc_asset"
  echo "fetching $dxc_url"
  tmp="$DEST/dxc-tmp"
  mkdir -p "$tmp"
  curl -sfL -o "$tmp/$dxc_asset" "$dxc_url"
  # The zip's paths use backslashes (bin\x64\...): match them with ? and
  # flatten; unzip warns about them with exit status 1, which is not a failure.
  unzip -o -q -j "$tmp/$dxc_asset" "bin?$dxc_arch?dxcompiler.dll" LICENCE-MIT.txt LICENSE-LLVM.txt -d "$tmp" 2>/dev/null \
    || [ $? -eq 1 ]
  cp -f "$tmp/dxcompiler.dll" "$DEST/"
  cp -f "$tmp/LICENCE-MIT.txt" "$DEST/dxcompiler-LICENSE-MIT.txt"
  cp -f "$tmp/LICENSE-LLVM.txt" "$DEST/dxcompiler-LICENSE-LLVM.txt"
  rm -rf "$tmp"
  echo "$DXC_VERSION" > "$DEST/dxc-version"
  echo "DXC $DXC_VERSION ready in $DEST"
fi
