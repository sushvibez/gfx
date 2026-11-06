#!/usr/bin/env bash
# Build wgpu-native for Android into build/wgpu/android/<abi>/.
#
# The pinned wgpu-native release (v29.0.0.0, the tag scripts/fetch-wgpu.sh
# fetches for the desktop, so the one C API the gpu bindings mirror) ships
# no Android prebuilts, so we cross-compile the tag from Rust source with
# the NDK, keeping one binding layer across every platform.
#
#   ABIS="x86_64" scripts/build-wgpu-android.sh   # emulator only (default: both)
#
# Requires: rustup with the android targets, an NDK under ANDROID_HOME.
set -euo pipefail

VERSION=v29.0.0.0
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SDK="${ANDROID_HOME:-$HOME/Android/Sdk}"
SRC="${WGPU_SRC:-/tmp/wgpu-native-$VERSION}"
NDK_HOME="$(ls -d "$SDK"/ndk/* | sort -V | tail -1)"
TOOLS="$NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin"
SYSROOT="$NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/sysroot"
API=29


if [ ! -f "$SRC/Cargo.toml" ]; then
  git clone --depth 1 --branch "$VERSION" --recurse-submodules \
    https://github.com/gfx-rs/wgpu-native "$SRC"
fi
[ -f "$SRC/ffi/webgpu-headers/webgpu.h" ] || (cd "$SRC" && git submodule update --init)
# The targets go onto the toolchain the checkout pins (rust-toolchain.toml),
# not the default one.
TOOLCHAIN="$(cd "$SRC" && rustup show active-toolchain | cut -d" " -f1)"
rustup target add --toolchain "$TOOLCHAIN" aarch64-linux-android x86_64-linux-android

build() { # <rust-target> <abi> <clang-prefix>
  local target="$1" abi="$2" prefix="$3"
  local up; up="$(echo "$target" | tr 'a-z-' 'A-Z_')"
  export CARGO_TARGET_${up}_LINKER="$TOOLS/${prefix}${API}-clang"
  export CC_${target//-/_}="$TOOLS/${prefix}${API}-clang"
  export AR_${target//-/_}="$TOOLS/llvm-ar"
  echo "== building $abi"
  (cd "$SRC" && BINDGEN_EXTRA_CLANG_ARGS="--sysroot=$SYSROOT --target=${prefix}${API}" \
    cargo build --release --target "$target")
  mkdir -p "$ROOT/build/wgpu/android/$abi"
  cp "$SRC/target/$target/release/libwgpu_native.so" "$ROOT/build/wgpu/android/$abi/"
  echo "   -> build/wgpu/android/$abi/libwgpu_native.so"
}

for abi in ${ABIS:-arm64-v8a x86_64}; do
  case "$abi" in
    arm64-v8a) build aarch64-linux-android arm64-v8a aarch64-linux-android ;;
    x86_64)    build x86_64-linux-android  x86_64    x86_64-linux-android ;;
    *) echo "unknown ABI $abi" >&2; exit 1 ;;
  esac
done
echo "done"
