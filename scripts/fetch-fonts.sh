#!/usr/bin/env bash
# Fetch the CJK fallback font used by the GPUI example ports (text_wrapper's
# 你好 / こんにちは run). Single-face OTF (OFL) — x/image/opentype can't open
# the .ttc collections most distros ship. Lands in build/fonts/, which the
# example shell's fallbackFontBytes() looks for. build/ is git-ignored.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/build/fonts"
FILE="$DEST/NotoSansCJKsc-Regular.otf"
URL="https://github.com/notofonts/noto-cjk/raw/main/Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Regular.otf"

mkdir -p "$DEST"
# Skip, do not exit: everything below this — Inter, the mono face, the
# sample images — was unreachable the moment the CJK font was present,
# which is every run after the first.
if [ -s "$FILE" ]; then
  echo "CJK fallback font already present: $FILE"
else
  echo "fetching $URL"
  curl -sfL -o "$FILE" "$URL"
  echo "Noto Sans CJK SC ready in $DEST ($(wc -c < "$FILE") bytes)"
fi

# Sample images + a GIF for the image_gallery / gif_viewer / opacity examples.
IMG="$ROOT/build/images"
mkdir -p "$IMG"
for i in 1 2 3 4 5 6; do
  f="$IMG/img$i.jpg"
  [ -s "$f" ] || curl -sfL -o "$f" "https://picsum.photos/seed/$((i*37))/400/200.jpg" || true
done
GIF="$IMG/anim.gif"
[ -s "$GIF" ] || curl -sfL -o "$GIF" "https://upload.wikimedia.org/wikipedia/commons/2/2c/Rotating_earth_%28large%29.gif" || true
echo "example images ready in $IMG"

# Color-emoji fallback (CBDT PNGs) for machines without a system Noto Color
# Emoji — the render.Font emoji path looks in build/fonts/ first.
EMOJI="$DEST/NotoColorEmoji.ttf"
if [ ! -s "$EMOJI" ] && [ ! -s /usr/share/fonts/truetype/noto/NotoColorEmoji.ttf ]; then
  curl -sfL -o "$EMOJI" "https://github.com/googlefonts/noto-emoji/raw/main/fonts/NotoColorEmoji.ttf" || true
  [ -s "$EMOJI" ] && echo "Noto Color Emoji ready in $DEST"
fi

# Inter (UI font used by the markdown app chrome; Shapr3D/Obsidian
# both use it). Three weights, not one: a interface drawn from a single
# face has to fake its headings by making them bigger, and everything
# ends up the same colour on the page.
INTER="$DEST/Inter-Regular.ttf"
if [ ! -s "$INTER" ] || [ ! -s "$DEST/Inter-SemiBold.ttf" ] || [ ! -s "$DEST/Inter-Medium.ttf" ]; then
  TMP="$(mktemp -d)"
  curl -sfL -o "$TMP/inter.zip" "https://github.com/rsms/inter/releases/download/v4.1/Inter-4.1.zip" &&
    unzip -o -q -j "$TMP/inter.zip" \
      "extras/ttf/Inter-Regular.ttf" "extras/ttf/Inter-Medium.ttf" "extras/ttf/Inter-SemiBold.ttf" \
      -d "$DEST" || true
  rm -rf "$TMP"
fi
[ -s "$INTER" ] && echo "fonts: $INTER"

# JetBrains Mono for the numbers. A HUD is mostly digits that change ten
# times a second, and a proportional face makes them dance; the fixed
# pitch we fake in code is a workaround for not having this.
MONO="$DEST/JetBrainsMono-Regular.ttf"
if [ ! -s "$MONO" ]; then
  TMP="$(mktemp -d)"
  curl -sfL -o "$TMP/mono.zip" "https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip" &&
    unzip -o -q -j "$TMP/mono.zip" "fonts/ttf/JetBrainsMono-Regular.ttf" "fonts/ttf/JetBrainsMono-Medium.ttf" -d "$DEST" || true
  rm -rf "$TMP"
fi
[ -s "$MONO" ] && echo "fonts: $MONO"
