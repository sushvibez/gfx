//go:build js

package gpu

import "syscall/js"

// Mipmapped textures and anisotropic sampling; see mips_native.go.

// MipLevels returns the number of mip levels in a full chain for a
// width x height texture (at least 1).
func MipLevels(width, height int) int {
	n := 1
	for width > 1 || height > 1 {
		width, height = max(width/2, 1), max(height/2, 1)
		n++
	}
	return n
}

// CreateTexture2DMips allocates a 2D texture with a mip chain of mips levels.
func (d *Device) CreateTexture2DMips(width, height int, format TextureFormat, usage TextureUsage, mips int) *Texture {
	if mips < 1 {
		mips = 1
	}
	v := d.v.Call("createTexture", map[string]any{
		"size":          []any{width, height},
		"format":        string(format),
		"usage":         int(usage),
		"mipLevelCount": mips,
	})
	return &Texture{v: v, bpt: BytesPerTexel(format)}
}

// WriteTextureMip uploads tightly packed RGBA8 pixels into one mip level.
func (q *Queue) WriteTextureMip(t *Texture, mip, width, height int, data []byte) {
	if len(data) == 0 || width <= 0 || height <= 0 {
		return
	}
	noteTextureUpload(len(data))
	u8 := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(u8, data)
	q.v.Call("writeTexture",
		map[string]any{"texture": t.v, "mipLevel": mip, "origin": []any{0, 0, 0}},
		u8,
		map[string]any{"bytesPerRow": width * 4, "rowsPerImage": height},
		[]any{width, height})
}

// CreateSamplerMip returns a trilinear sampler with the requested maximum
// anisotropy (1..16); repeat selects tiling vs clamped addressing.
func (d *Device) CreateSamplerMip(repeat bool, maxAnisotropy int) *Sampler {
	mode := "clamp-to-edge"
	if repeat {
		mode = "repeat"
	}
	if maxAnisotropy < 1 {
		maxAnisotropy = 1
	}
	if maxAnisotropy > 16 {
		maxAnisotropy = 16
	}
	v := d.v.Call("createSampler", map[string]any{
		"addressModeU":  mode,
		"addressModeV":  mode,
		"addressModeW":  mode,
		"magFilter":     "linear",
		"minFilter":     "linear",
		"mipmapFilter":  "linear",
		"maxAnisotropy": maxAnisotropy,
	})
	return &Sampler{v}
}
