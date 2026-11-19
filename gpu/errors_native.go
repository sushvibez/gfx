//go:build !js

package gpu

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GPU memory running out is something a program can live through: wgpu
// reports a buffer or texture it could not allocate through the
// uncaptured-error callback (as a validation error whose message says
// "Not enough memory left", or as an out-of-memory error) and hands back
// an invalid object; whatever binds that object fails too, and the
// command buffer that holds the failure aborts the process at its
// Submit. Three things here keep it alive:
//
//   - every out-of-memory report is counted (OutOfMemoryCount), so a
//     program can watch for pressure and shed what it can;
//   - TryCreateBuffer and TryCreateTexture2D give nil instead of an
//     invalid object, for allocations a caller can do without (a terrain
//     slab, a prop's mesh);
//   - a command buffer whose Finish reported an error is never submitted
//     (Submit drops it and counts it, DroppedSubmits): the frame is lost,
//     not the process.
//
// The error lines are rate-limited: the first few of each kind, then a
// count every few seconds, not a thousand lines a second.

var (
	oomCount     uint64 // out-of-memory reports
	finishErrors uint64 // errors reported from inside wgpuCommandEncoderFinish
	droppedSubs  uint64 // command buffers Submit refused
)

// OutOfMemoryCount is how many allocations the device has refused for
// want of memory so far.
func OutOfMemoryCount() uint64 { return atomic.LoadUint64(&oomCount) }

// DroppedSubmits is how many command buffers Submit dropped because
// their encoding had failed (an invalid buffer, texture or bind group in
// them).
func DroppedSubmits() uint64 { return atomic.LoadUint64(&droppedSubs) }

// IsOutOfMemory reports whether an error message (and type) is the device
// running out of memory.
func IsOutOfMemory(errType uintptr, msg string) bool {
	return errType == 3 || strings.Contains(msg, "Not enough memory") || strings.Contains(msg, "OutOfMemory") || strings.Contains(msg, "out of memory")
}

// errLog rate-limits the error lines per kind.
var errLog struct {
	sync.Mutex
	n      map[string]int
	quiet  map[string]int // suppressed since the last summary
	last   map[string]time.Time
	silent bool
}

const errLogFirst = 6 // lines of one kind printed before they are summarised

// noteError counts an uncaptured error and prints it (rate-limited).
func noteError(errType uintptr, msg string) {
	atomic.AddUint64(&errorCount, 1)
	kind := "other"
	switch {
	case IsOutOfMemory(errType, msg) || simRefusing.Load():
		atomic.AddUint64(&oomCount, 1)
		kind = "oom"
	case strings.Contains(msg, "is invalid"):
		kind = "invalid"
	}
	if strings.Contains(msg, "wgpuCommandEncoderFinish") {
		atomic.AddUint64(&finishErrors, 1)
	}
	errLog.Lock()
	defer errLog.Unlock()
	if errLog.n == nil {
		errLog.n, errLog.quiet, errLog.last = map[string]int{}, map[string]int{}, map[string]time.Time{}
	}
	errLog.n[kind]++
	if errLog.n[kind] <= errLogFirst || kind == "other" {
		fmt.Fprintf(os.Stderr, "gpu: uncaptured error (type %d): %s\n", errType, msg)
		if errLog.n[kind] == errLogFirst && kind != "other" {
			fmt.Fprintf(os.Stderr, "gpu: further %s errors are counted, not printed\n", errKindText(kind))
		}
		errLog.last[kind] = time.Now()
		return
	}
	errLog.quiet[kind]++
	if time.Since(errLog.last[kind]) > 5*time.Second {
		fmt.Fprintf(os.Stderr, "gpu: %d more %s errors\n", errLog.quiet[kind], errKindText(kind))
		errLog.quiet[kind] = 0
		errLog.last[kind] = time.Now()
	}
}

func errKindText(kind string) string {
	switch kind {
	case "oom":
		return "out-of-memory"
	case "invalid":
		return "invalid-object"
	}
	return kind
}

// TryCreateBuffer is CreateBuffer for an allocation the caller can do
// without: nil when the device refused it for want of memory (the
// refusal is reported synchronously, so a change in OutOfMemoryCount
// across the call is this call's; another goroutine's refusal at the same
// instant can be blamed on it too, which costs a retry, not a crash).
func (d *Device) TryCreateBuffer(usage BufferUsage, size int) *Buffer {
	before := OutOfMemoryCount()
	b := d.CreateBuffer(usage, size)
	if OutOfMemoryCount() != before {
		b.Release()
		return nil
	}
	return b
}

// TryCreateTexture2D is CreateTexture2D, nil when the device refused it
// for want of memory (as TryCreateBuffer).
func (d *Device) TryCreateTexture2D(width, height int, format TextureFormat, usage TextureUsage) *Texture {
	before := OutOfMemoryCount()
	t := d.CreateTexture2D(width, height, format, usage)
	if OutOfMemoryCount() != before {
		t.Release()
		return nil
	}
	return t
}

// GFX_SIM_VRAM_MB=n makes the device behave as one with n MB for buffers:
// past that, a buffer is refused as wgpu refuses one it has no memory
// for — an invalid object, reported as out of memory — so the paths that
// live through running out (TryCreateBuffer, Submit dropping a failed
// encoding, a game shedding detail) can be tried on a big card without
// filling it. Textures are not counted, nor a readback's staging.
var simVRAM = func() int64 {
	v, err := strconv.Atoi(os.Getenv("GFX_SIM_VRAM_MB"))
	if err != nil || v <= 0 {
		return 0
	}
	return int64(v) << 20
}()

var (
	simUsed     atomic.Int64
	simRefusing atomic.Bool // the error now being reported is a simulated refusal
)

func (d *Device) simBuffer(desc bufferDescriptor, size int) *Buffer {
	if simUsed.Add(int64(size)) <= simVRAM {
		return &Buffer{h: wgpuDeviceCreateBuffer(d.h, &desc), size: size, sim: true}
	}
	simUsed.Add(-int64(size))
	// An invalid buffer, as a refused one is (a usage of none is invalid).
	bad := desc
	bad.usage = 0
	simRefusing.Store(true)
	h := wgpuDeviceCreateBuffer(d.h, &bad)
	simRefusing.Store(false)
	return &Buffer{h: h, size: size}
}
