package render

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

// systemEmojiFont returns a CBDT color-emoji face if the machine has one.
func systemEmojiFont(t *testing.T) []byte {
	t.Helper()
	for _, p := range []string{
		"/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf",
		"/usr/share/fonts/noto/NotoColorEmoji.ttf",
	} {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	t.Skip("no system color-emoji font")
	return nil
}

func TestParseEmojiFont(t *testing.T) {
	data := systemEmojiFont(t)
	em, err := parseEmojiFont(data)
	if err != nil {
		t.Fatalf("parseEmojiFont: %v", err)
	}
	if em.ppem <= 0 {
		t.Fatalf("ppem = %d, want > 0", em.ppem)
	}
	// A few common single-codepoint emoji must resolve to decodable PNGs.
	for _, r := range []rune{'😀', '🎉', '❤', '🏀', '👍'} {
		if !em.covers(r) {
			t.Errorf("emoji font does not cover %q", string(r))
			continue
		}
		data, ok := em.lookup(r)
		if !ok {
			t.Errorf("lookup(%q) failed", string(r))
			continue
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Errorf("glyph for %q is not a PNG: %v", string(r), err)
			continue
		}
		if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
			t.Errorf("glyph for %q is empty", string(r))
		}
	}
	// Plain ASCII must NOT be claimed by the emoji font (it has no letter
	// glyph bitmaps), so text never routes through it by accident.
	if em.covers('A') {
		t.Error("emoji font claims to cover 'A'")
	}
}
