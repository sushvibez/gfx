#!/usr/bin/env bash
# Build a signed, installable APK for an example — the `gfx pack` story
# (DESIGN.md §8). No Java/Gradle: NativeActivity loads the Go .so directly.
#
# Usage: scripts/build-apk.sh <example> [abi]
#   abi: x86_64 (emulator, default) or arm64-v8a (devices)
#
# A program outside examples/ names its module directory and package:
#   GFX_APK_MODULE=. GFX_APK_PKG=./md/cmd/mdview scripts/build-apk.sh mdview arm64-v8a
# GFX_APK_ORIENTATION=landscape locks the activity's orientation (a game).
# GFX_APK_ASSETS=dir packs a directory into the APK's assets (data stores,
# models); app.Asset copies one out into the data directory on demand.
#
# Requires ANDROID_HOME (or ~/Android/Sdk) with an NDK, build-tools and a
# platform; and wgpu-native Android libs in build/wgpu/android/<abi>/
# (scripts/build-wgpu-android.sh).
set -euo pipefail

NAME="${1:?usage: build-apk.sh <example> [abi]}"
ABI="${2:-x86_64}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# Android Studio's default SDK location per host, unless ANDROID_HOME says.
case "$(uname -s)" in
  Darwin) SDK_DEFAULT="$HOME/Library/Android/sdk" ;;
  MINGW*|MSYS*|CYGWIN*) SDK_DEFAULT="${LOCALAPPDATA:-$HOME/AppData/Local}/Android/Sdk" ;;
  *) SDK_DEFAULT="$HOME/Android/Sdk" ;;
esac
SDK="${ANDROID_HOME:-$SDK_DEFAULT}"
OUT="$ROOT/build/apk/$NAME"
MINSDK=29

case "$ABI" in
  x86_64)    GOARCH=amd64; CLANG_ARCH=x86_64-linux-android ;;
  arm64-v8a) GOARCH=arm64; CLANG_ARCH=aarch64-linux-android ;;
  *) echo "unknown ABI $ABI (x86_64 | arm64-v8a)" >&2; exit 1 ;;
esac

# Resolve toolchain pieces.
NDK="$(ls -d "$SDK"/ndk/* | sort -V | tail -1)"
# An NDK ships one host toolchain (linux-x86_64, darwin-x86_64 or
# windows-x86_64); on Windows the clang wrappers are .cmd scripts and the
# SDK's build tools .exe (apksigner a .bat).
HOST_TAG="$(ls "$NDK/toolchains/llvm/prebuilt" | head -1)"
TOOLS="$NDK/toolchains/llvm/prebuilt/$HOST_TAG/bin"
CMD=""; EXE=""; BAT=""
case "$HOST_TAG" in windows-*) CMD=".cmd"; EXE=".exe"; BAT=".bat" ;; esac
BT="$(ls -d "$SDK"/build-tools/* | sort -V | tail -1)"
PLATFORM_JAR="$(ls "$SDK"/platforms/android-*/android.jar | sort -V | tail -1)"
CLANG="$TOOLS/${CLANG_ARCH}${MINSDK}-clang$CMD"
WGPU="$ROOT/build/wgpu/android/$ABI/libwgpu_native.so"

[ -f "$WGPU" ] || { echo "missing $WGPU — run scripts/build-wgpu-android.sh" >&2; exit 1; }

PKG="io.github.sushvibez.gfx.$NAME"
rm -rf "$OUT"; mkdir -p "$OUT/lib/$ABI"

echo "== compile Go .so ($ABI)"
(cd "$ROOT/${GFX_APK_MODULE:-examples}" && CGO_ENABLED=1 GOOS=android GOARCH=$GOARCH CC="$CLANG" \
  go build -buildmode=c-shared -ldflags="${GFX_LDFLAGS--s -w}" -o "$OUT/lib/$ABI/lib$NAME.so" "${GFX_APK_PKG:-./$NAME}")
rm -f "$OUT/lib/$ABI/lib$NAME.h" # cgo header — not an APK artifact
cp "$WGPU" "$OUT/lib/$ABI/"

echo "== manifest"
cat > "$OUT/AndroidManifest.xml" <<XML
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="$PKG">
  <uses-sdk android:minSdkVersion="$MINSDK" android:targetSdkVersion="34"/>
  <uses-feature android:name="android.hardware.vulkan.version" android:required="false"/>
  <uses-permission android:name="android.permission.INTERNET"/>
  <application android:label="gfx $NAME" android:hasCode="false">
    <activity android:name="android.app.NativeActivity" android:exported="true"
              android:screenOrientation="${GFX_APK_ORIENTATION:-unspecified}"
              android:configChanges="orientation|keyboardHidden|screenSize|screenLayout">
      <meta-data android:name="android.app.lib_name" android:value="$NAME"/>
      <intent-filter>
        <action android:name="android.intent.action.MAIN"/>
        <category android:name="android.intent.category.LAUNCHER"/>
      </intent-filter>
    </activity>
  </application>
</manifest>
XML

echo "== aapt2 link"
ASSETS_ARG=()
if [ -n "${GFX_APK_ASSETS:-}" ]; then
  # Data files packed into the APK's assets/ (a DEM store, models); the
  # app copies them into its data directory on first run (app.Asset).
  ASSETS_ARG=(-A "$GFX_APK_ASSETS")
  echo "== assets from $GFX_APK_ASSETS ($(du -sh "$GFX_APK_ASSETS" | cut -f1))"
fi
"$BT/aapt2$EXE" link -o "$OUT/base.apk" -I "$PLATFORM_JAR" "${ASSETS_ARG[@]}" \
  --manifest "$OUT/AndroidManifest.xml" --min-sdk-version $MINSDK --target-sdk-version 34

echo "== add native libs"
(cd "$OUT" && zip -qr base.apk "lib/$ABI")

echo "== zipalign + sign"
KS="$ROOT/build/debug.keystore"
if [ ! -f "$KS" ]; then
  keytool -genkeypair -keystore "$KS" -alias gfx -storepass android -keypass android \
    -dname "CN=gfx" -keyalg RSA -keysize 2048 -validity 10000 >/dev/null 2>&1
fi
"$BT/zipalign$EXE" -f 4 "$OUT/base.apk" "$OUT/$NAME.apk"
"$BT/apksigner$BAT" sign --ks "$KS" --ks-pass pass:android --key-pass pass:android "$OUT/$NAME.apk"

echo "APK: $OUT/$NAME.apk  ($PKG)"
