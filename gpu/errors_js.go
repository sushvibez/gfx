//go:build js

package gpu

// On the web the browser's WebGPU reports errors through promises and the
// uncapturederror event (logged to the console); nothing is counted, and
// an allocation the browser refuses is the browser's to report.

// OutOfMemoryCount is always 0 on the web.
func OutOfMemoryCount() uint64 { return 0 }

// DroppedSubmits is always 0 on the web.
func DroppedSubmits() uint64 { return 0 }

// IsOutOfMemory reports whether an error message is the device running
// out of memory.
func IsOutOfMemory(errType uintptr, msg string) bool { return errType == 3 }

// TryCreateBuffer is CreateBuffer (the web cannot tell a refusal here).
func (d *Device) TryCreateBuffer(usage BufferUsage, size int) *Buffer {
	return d.CreateBuffer(usage, size)
}

// TryCreateTexture2D is CreateTexture2D (as TryCreateBuffer).
func (d *Device) TryCreateTexture2D(width, height int, format TextureFormat, usage TextureUsage) *Texture {
	return d.CreateTexture2D(width, height, format, usage)
}
