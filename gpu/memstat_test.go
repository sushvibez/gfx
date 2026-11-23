package gpu

import "testing"

// TestTextureBytes: a texture's memory is every mip level of every layer
// times its samples.
func TestTextureBytes(t *testing.T) {
	cases := []struct {
		w, h, layers, mips, samples int
		f                           TextureFormat
		want                        int64
	}{
		{256, 256, 1, 1, 1, FormatRGBA8Unorm, 256 * 256 * 4},
		{4, 4, 1, 3, 1, FormatRGBA8Unorm, (16 + 4 + 1) * 4},
		{4, 2, 1, 3, 1, FormatR8Unorm, 8 + 2 + 1}, // the short side stops at 1
		{64, 64, 6, 1, 1, FormatRGBA16Float, 64 * 64 * 6 * 8},
		{100, 50, 1, 1, 4, FormatDepth32Float, 100 * 50 * 4 * 4},
		{0, 0, 0, 0, 0, FormatRGBA8Unorm, 4}, // degenerate sizes count as one texel
	}
	for _, c := range cases {
		if got := textureBytes(c.w, c.h, c.layers, c.mips, c.samples, c.f); got != c.want {
			t.Errorf("textureBytes(%d×%d, %d layers, %d mips, %d samples) = %d, want %d",
				c.w, c.h, c.layers, c.mips, c.samples, got, c.want)
		}
	}
}
