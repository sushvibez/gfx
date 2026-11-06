//go:build js

package gpu

import "syscall/js"

// CreateTexture2DArray allocates a 2D texture with `layers` array layers.
func (d *Device) CreateTexture2DArray(width, height, layers int, format TextureFormat, usage TextureUsage) *Texture {
	v := d.v.Call("createTexture", map[string]any{
		"size":   []any{width, height, layers},
		"format": string(format),
		"usage":  int(usage),
	})
	return &Texture{v: v, bpt: BytesPerTexel(format)}
}

// CreateViewArray returns a texture_2d_array view over all layers.
func (t *Texture) CreateViewArray(format TextureFormat, layers int) *TextureView {
	return &TextureView{t.v.Call("createView", map[string]any{
		"format":          string(format),
		"dimension":       "2d-array",
		"arrayLayerCount": layers,
	})}
}

// WriteTextureLayer uploads tightly packed RGBA8 pixels into one array
// layer of t.
func (q *Queue) WriteTextureLayer(t *Texture, layer, width, height int, data []byte) {
	q.WriteTextureRegion(t, layer, 0, 0, width, height, data)
}

// WriteTextureRegion uploads a width×height RGBA8 block at (x, y) of one
// array layer.
func (q *Queue) WriteTextureRegion(t *Texture, layer, x, y, width, height int, data []byte) {
	if len(data) == 0 {
		return
	}
	u8 := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(u8, data)
	q.v.Call("writeTexture",
		map[string]any{"texture": t.v, "origin": []any{x, y, layer}},
		u8,
		map[string]any{"bytesPerRow": width * 4, "rowsPerImage": height},
		[]any{width, height, 1})
}
