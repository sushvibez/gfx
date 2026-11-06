package ecs

import (
	"unsafe"

	"github.com/sushvibez/gfx/gpu"
)

// GPUColumn mirrors a component column into a GPU storage buffer
// (DESIGN.md §4): T must be POD with a WGSL-compatible layout (the gfx gen
// tool will enforce this; until then it is the caller's contract). Sync
// uploads when the column changed; compute and render passes bind the
// buffer directly and operate with stride = sizeof(T).
type GPUColumn[T any] struct {
	comp      *Component[T]
	dev       *gpu.Device
	buf       *gpu.Buffer
	usage     gpu.BufferUsage
	rows      int // rows uploaded to the GPU
	lastLo    int
	lastBytes int
}

// NewGPUColumn wraps comp for GPU sharing. Extra usage flags are OR-ed
// onto Storage|CopyDst (pass gpu.BufferCopySrc for readback in tests).
func NewGPUColumn[T any](dev *gpu.Device, comp *Component[T], extraUsage gpu.BufferUsage) *GPUColumn[T] {
	return &GPUColumn[T]{
		comp:  comp,
		dev:   dev,
		usage: gpu.BufferStorage | gpu.BufferCopyDst | extraUsage,
	}
}

// Buffer returns the GPU buffer (valid after the first Sync).
func (g *GPUColumn[T]) Buffer() *gpu.Buffer { return g.buf }

// Rows reports how many rows the GPU currently holds.
func (g *GPUColumn[T]) Rows() int { return g.rows }

// Stride is the size of one row in bytes.
func (g *GPUColumn[T]) Stride() int {
	var t T
	return int(unsafe.Sizeof(t))
}

// Sync uploads the rows that changed since the last sync (one merged
// range), growing the buffer — and uploading fully — when the column
// outgrew it. It reports whether any upload happened. LastUpload exposes
// what was sent, for tests and profiling.
func (g *GPUColumn[T]) Sync(q *gpu.Queue) bool {
	col := g.comp.Column()
	lo, hi := g.comp.dirtyLo, g.comp.dirtyHi
	grew := false
	size := max(len(col)*g.Stride(), g.Stride()) // never zero-sized
	if g.buf == nil || g.buf.Size() < size {
		if g.buf != nil {
			g.buf.Release()
		}
		g.buf = g.dev.CreateBuffer(g.usage, size)
		grew = true
	}
	if grew {
		lo, hi = 0, len(col)-1 // fresh buffer needs everything
	}
	g.rows = len(col)
	g.comp.dirtyLo = -1
	if lo < 0 || len(col) == 0 {
		g.lastLo, g.lastBytes = 0, 0
		return false
	}
	hi = min(hi, len(col)-1)
	n := (hi - lo + 1) * g.Stride()
	data := unsafe.Slice((*byte)(unsafe.Pointer(&col[lo])), n)
	q.WriteBuffer(g.buf, lo*g.Stride(), data)
	g.lastLo, g.lastBytes = lo*g.Stride(), n
	return true
}

// LastUpload reports the byte offset and length of the most recent Sync
// upload (0,0 when it skipped).
func (g *GPUColumn[T]) LastUpload() (offset, bytes int) { return g.lastLo, g.lastBytes }

// Release frees the GPU buffer.
func (g *GPUColumn[T]) Release() {
	if g.buf != nil {
		g.buf.Release()
		g.buf = nil
	}
}
