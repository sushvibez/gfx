package gpu

import "sync/atomic"

// memAccount is a device's running total of the bytes its live buffers
// and textures hold. It is atomic because a loading goroutine creates
// resources while the frame loop reads the total.
type memAccount struct{ n atomic.Int64 }

// add counts n bytes against the account (nil-safe) and returns the
// account to keep on the resource, so its Release can give them back.
func (a *memAccount) add(n int64) *memAccount {
	if a == nil || n <= 0 {
		return nil
	}
	a.n.Add(n)
	return a
}

// sub gives n bytes back (nil-safe).
func (a *memAccount) sub(n int64) {
	if a != nil {
		a.n.Add(-n)
	}
}

// AllocatedBytes is how much memory the device's live buffers and
// textures take, as this package created them: each buffer's size, and
// each texture's texels over all its mip levels, layers and samples. It is
// the application's own count, not the driver's: alignment, padding and
// the swapchain's images are not in it, and a buffer the device refused
// counts until it is released. Cheap enough to read every frame (a
// performance overlay's VRAM line).
func (d *Device) AllocatedBytes() int64 {
	if d == nil || d.mem == nil {
		return 0
	}
	return d.mem.n.Load()
}

// textureBytes is the memory a texture of these dimensions holds: every
// mip level of every layer, times its samples.
func textureBytes(width, height, layers, mips, samples int, format TextureFormat) int64 {
	layers, mips, samples = max(layers, 1), max(mips, 1), max(samples, 1)
	var texels int64
	w, h := max(width, 1), max(height, 1)
	for range mips {
		texels += int64(w) * int64(h)
		w, h = max(w/2, 1), max(h/2, 1)
	}
	return texels * int64(layers) * int64(samples) * int64(BytesPerTexel(format))
}
