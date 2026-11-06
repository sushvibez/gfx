//go:build !js

package ecs

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/gpu"
)

// particle is a WGSL-layout-compatible POD component (vec2 + vec2 = 16B).
type particle struct {
	X, Y   float32
	DX, DY float32
}

// TestGPUColumnRoundTrip pushes an ecs column to the GPU, mutates it with a
// compute shader, and reads it back — the DESIGN.md §4 contract end to end.
func TestGPUColumnRoundTrip(t *testing.T) {
	lib := os.Getenv("GFX_WGPU_LIB")
	if lib == "" {
		p, err := filepath.Abs(filepath.Join("..", "build", "wgpu", gpu.DefaultLibNames()[0]))
		if err == nil {
			if _, statErr := os.Stat(p); statErr == nil {
				lib = p
			}
		}
	}
	if lib == "" {
		t.Skip("wgpu-native not found; run scripts/fetch-wgpu.sh or set GFX_WGPU_LIB")
	}
	inst, err := gpu.NewInstance(lib)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Fatalf("RequestAdapter: %v", err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Fatalf("RequestDevice: %v", err)
	}
	queue := device.Queue()

	w := NewWorld()
	parts := NewComponent[particle](w)
	const n = 256
	for i := range n {
		e := w.Spawn()
		parts.Add(e, particle{X: float32(i), Y: 0, DX: 1, DY: 2})
	}

	col := NewGPUColumn(device, parts, gpu.BufferCopySrc)
	defer col.Release()
	if !col.Sync(queue) {
		t.Fatal("first Sync should upload")
	}
	if col.Sync(queue) {
		t.Fatal("clean column should not re-upload")
	}
	if col.Rows() != n || col.Stride() != 16 {
		t.Fatalf("rows=%d stride=%d", col.Rows(), col.Stride())
	}

	module, err := device.CreateShaderModuleWGSL("step", `
struct Particle { pos : vec2f, vel : vec2f }
@group(0) @binding(0) var<storage, read_write> parts : array<Particle>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) id : vec3u) {
    if (id.x < arrayLength(&parts)) {
        parts[id.x].pos += parts[id.x].vel;
    }
}`)
	if err != nil {
		t.Fatalf("shader: %v", err)
	}
	defer module.Release()
	pipeline, err := device.CreateComputePipeline(module, "main")
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	defer pipeline.Release()
	layout := pipeline.BindGroupLayout(0)
	defer layout.Release()
	bind := device.CreateBindGroup(layout, gpu.BindGroupEntry{Binding: 0, Buffer: col.Buffer()})
	defer bind.Release()

	enc := device.CreateCommandEncoder()
	pass := enc.BeginComputePass()
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, bind)
	pass.Dispatch((n+63)/64, 1, 1)
	pass.End()
	queue.Submit(enc.Finish())

	out, err := device.ReadBuffer(queue, col.Buffer(), n*col.Stride())
	if err != nil {
		t.Fatalf("ReadBuffer: %v", err)
	}
	f32 := func(off int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(out[off:]))
	}
	for i := range n {
		base := i * 16
		if got := f32(base); got != float32(i)+1 {
			t.Fatalf("particle %d x = %v, want %v", i, got, float32(i)+1)
		}
		if got := f32(base + 4); got != 2 {
			t.Fatalf("particle %d y = %v, want 2", i, got)
		}
	}

	// CPU-side mutation re-flags the column; a single dirty row uploads
	// only its own 16-byte span at the right offset.
	parts.Column()[7].X = 99
	parts.MarkDirtyRow(7)
	if !col.Sync(queue) {
		t.Fatal("dirty column should re-upload")
	}
	if off, bytes := col.LastUpload(); off != 7*16 || bytes != 16 {
		t.Fatalf("dirty-range upload = (%d, %d), want (112, 16)", off, bytes)
	}
	out2, err := device.ReadBuffer(queue, col.Buffer(), n*col.Stride())
	if err != nil {
		t.Fatalf("ReadBuffer: %v", err)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(out2[7*16:])); got != 99 {
		t.Fatalf("row 7 x = %v, want 99", got)
	}
	// Neighbors untouched by the partial upload.
	if got := math.Float32frombits(binary.LittleEndian.Uint32(out2[6*16:])); got != 7 {
		t.Fatalf("row 6 x = %v, want 7", got)
	}

	// Two scattered rows merge into one spanning range.
	parts.Column()[2].Y = 5
	parts.MarkDirtyRow(2)
	parts.Column()[10].Y = 5
	parts.MarkDirtyRow(10)
	col.Sync(queue)
	if off, bytes := col.LastUpload(); off != 2*16 || bytes != 9*16 {
		t.Fatalf("merged range = (%d, %d), want (32, 144)", off, bytes)
	}
}
