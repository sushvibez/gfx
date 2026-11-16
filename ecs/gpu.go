package ecs

import (
	"math"
	"unsafe"

	"github.com/sushvibez/gfx/gpu"
)

// GPUColumn mirrors a component column into a GPU storage buffer
// (DESIGN.md §4): T must be POD with a WGSL-compatible layout (the gfx gen
// tool will enforce this; until then it is the caller's contract). Sync
// uploads when the column changed; compute and render passes bind the
// buffer directly and operate with stride = sizeof(T).
//
// Capacity. The buffer is as long as the column the first time it is
// made, and from then on grows geometrically (see Capacity), so it is
// usually longer than the rows it holds:
//
//   - Rows, not Buffer().Size() or WGSL's arrayLength, is how many rows
//     are live; pass the count to the shader. Rows past it hold zeros or
//     rows that have since been removed.
//   - Buffer returns a different buffer after a Sync that grew or shrank
//     it. Generation changes when it does: a bind group made over the old
//     buffer must be made again (the old buffer has been released).
//
// Several mirrors. Any number of GPUColumns may mirror one component (two
// devices, or two usages); each keeps its own dirty range, so each
// uploads every change exactly once whatever order they sync in.
//
// Lifetime. A GPUColumn belongs to the goroutine that owns the world; it
// is not safe for concurrent use. Release frees the buffer and stops
// tracking the component's changes; it may be called on a column that
// never synced, and more than once. A released column may be synced
// again: it makes a new buffer and uploads everything. Release every
// column before releasing the device that made it, and before dropping
// the last reference to it: the component keeps a column that was not
// released alive and goes on recording changes for it.
type GPUColumn[T any] struct {
	comp  *Component[T]
	dev   *gpu.Device
	buf   *gpu.Buffer
	usage gpu.BufferUsage

	rows     int     // rows uploaded to the GPU
	capRows  int     // rows the buffer holds; 0 = no buffer
	maxRows  int     // the device's limit, which doubling stops at
	gen      uint32  // counts the buffers made
	idle     int     // consecutive syncs with the buffer mostly empty
	dirty    rowSpan // this mirror's own range of changed rows
	attached bool    // dirty is registered with comp

	lastLo    int
	lastBytes int

	// The two device calls, replaced in tests so the bookkeeping can be
	// checked without a GPU: alloc swaps the buffer for one of bytes,
	// write uploads data at a byte offset.
	alloc func(bytes int)
	write func(q *gpu.Queue, offset int, data []byte)
}

// GPUColumnKeepSyncs is how many consecutive Syncs a buffer may stay less
// than a quarter full before it is replaced by a smaller one. The delay is
// what keeps a column that empties and refills every frame from making a
// buffer each time.
const GPUColumnKeepSyncs = 120

// gpuColumnShrinkBytes is the size below which a buffer is never shrunk:
// the memory is not worth a new buffer and a full upload.
const gpuColumnShrinkBytes = 64 << 10

// NewGPUColumn wraps comp for GPU sharing. Extra usage flags are OR-ed
// onto Storage|CopyDst (pass gpu.BufferCopySrc for readback in tests).
func NewGPUColumn[T any](dev *gpu.Device, comp *Component[T], extraUsage gpu.BufferUsage) *GPUColumn[T] {
	g := &GPUColumn[T]{
		comp:  comp,
		dev:   dev,
		usage: gpu.BufferStorage | gpu.BufferCopyDst | extraUsage,
		dirty: rowSpan{lo: -1},
	}
	// A storage buffer is bound whole, so the binding limit bounds it too.
	lim := dev.Limits()
	g.maxRows = rowLimit(min(lim.MaxBufferSize, lim.MaxStorageBindingSize), g.Stride())
	g.alloc = func(bytes int) {
		if g.buf != nil {
			g.buf.Release()
		}
		g.buf = g.dev.CreateBuffer(g.usage, bytes)
	}
	g.write = func(q *gpu.Queue, offset int, data []byte) {
		q.WriteBuffer(g.buf, offset, data)
	}
	return g
}

// rowLimit is how many rows of stride bytes fit in limit bytes.
func rowLimit(limit int64, stride int) int {
	if stride <= 0 || limit <= 0 {
		return math.MaxInt
	}
	return int(min(limit/int64(stride), math.MaxInt))
}

// growCapacity is how many rows a buffer of have rows (0 = none yet)
// should hold for a column of need rows: exactly the column the first
// time, so a column that never grows wastes nothing, then at least double
// whenever the column outgrows it, so a column growing a row at a time
// makes O(log rows) buffers and uploads O(rows) bytes in all rather than
// a buffer and a full upload per row. Doubling stops at limit; a column
// past the limit gets what it needs, and the device reports it.
func growCapacity(have, need, limit int) int {
	if have == 0 {
		return max(need, 1) // never zero-sized
	}
	if need <= have {
		return have
	}
	c := need
	if have <= limit/2 {
		c = max(need, 2*have)
	} else if need <= limit {
		c = limit
	}
	return c
}

// Buffer returns the GPU buffer (valid after the first Sync, until the
// Sync that changes Generation).
func (g *GPUColumn[T]) Buffer() *gpu.Buffer { return g.buf }

// Rows reports how many rows the GPU currently holds.
func (g *GPUColumn[T]) Rows() int { return g.rows }

// Capacity reports how many rows the buffer has room for (0 before the
// first Sync). It is at least Rows.
func (g *GPUColumn[T]) Capacity() int { return g.capRows }

// Generation counts the buffers this column has made: it changes exactly
// when Buffer starts returning a different buffer, which is when bind
// groups over it must be rebuilt.
func (g *GPUColumn[T]) Generation() uint32 { return g.gen }

// Stride is the size of one row in bytes.
func (g *GPUColumn[T]) Stride() int {
	var t T
	return int(unsafe.Sizeof(t))
}

// Sync uploads the rows that changed since this column's last sync (one
// merged range), replacing the buffer — and uploading fully — when the
// column outgrew it or has left it mostly empty for GPUColumnKeepSyncs
// syncs. It reports whether any upload happened. LastUpload exposes what
// was sent, for tests and profiling.
func (g *GPUColumn[T]) Sync(q *gpu.Queue) bool {
	if !g.attached {
		g.comp.attach(&g.dirty)
		g.attached = true
	}
	col := g.comp.Column()
	stride := g.Stride()
	lo, hi := g.dirty.lo, g.dirty.hi
	g.dirty.lo = -1

	want := growCapacity(g.capRows, len(col), g.maxRows)
	if want == g.capRows && len(col)*4 <= g.capRows && g.capRows*stride > gpuColumnShrinkBytes {
		// Mostly empty: shrink once it has stayed that way, to twice the
		// column so it can grow again without a new buffer.
		if g.idle++; g.idle >= GPUColumnKeepSyncs {
			want = max(2*len(col), 1)
		}
	} else {
		g.idle = 0
	}
	if want != g.capRows {
		g.alloc(max(want*stride, stride))
		g.capRows = want
		g.gen++
		g.idle = 0
		lo, hi = 0, len(col)-1 // fresh buffer needs everything
	}
	g.rows = len(col)
	if lo < 0 || len(col) == 0 {
		g.lastLo, g.lastBytes = 0, 0
		return false
	}
	hi = min(hi, len(col)-1)
	n := (hi - lo + 1) * stride
	data := unsafe.Slice((*byte)(unsafe.Pointer(&col[lo])), n)
	g.write(q, lo*stride, data)
	g.lastLo, g.lastBytes = lo*stride, n
	return true
}

// LastUpload reports the byte offset and length of the most recent Sync
// upload (0,0 when it skipped).
func (g *GPUColumn[T]) LastUpload() (offset, bytes int) { return g.lastLo, g.lastBytes }

// Release frees the GPU buffer and stops recording the component's
// changes for this column. It is safe on a column that never synced and
// safe to repeat; see the type's comment for what may follow it.
func (g *GPUColumn[T]) Release() {
	if g.attached {
		g.comp.detach(&g.dirty)
		g.attached = false
	}
	g.dirty.lo = -1
	if g.buf != nil {
		g.buf.Release()
		g.buf = nil
	}
	g.capRows, g.rows, g.idle = 0, 0, 0
}
