//go:build !js

package gpu

import (
	"encoding/binary"
	"math"
	"testing"
)

// TestComputeDoubles runs a compute shader over a storage buffer headlessly
// and reads the result back: the smallest possible proof that the compute
// path works end to end.
func TestComputeDoubles(t *testing.T) {
	if err := Load(libPath(t)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	inst, err := NewInstance()
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

	const n = 1024
	data := make([]byte, n*4)
	for i := range n {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(float32(i)))
	}
	buf := device.CreateBuffer(BufferStorage|BufferCopyDst|BufferCopySrc, len(data))
	defer buf.Release()
	queue.WriteBuffer(buf, 0, data)

	module, err := device.CreateShaderModuleWGSL("double", `
@group(0) @binding(0) var<storage, read_write> values : array<f32>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) id : vec3u) {
    if (id.x < arrayLength(&values)) {
        values[id.x] = values[id.x] * 2.0;
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
	bind := device.CreateBindGroup(layout, BindGroupEntry{Binding: 0, Buffer: buf})
	defer bind.Release()

	enc := device.CreateCommandEncoder()
	pass := enc.BeginComputePass()
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, bind)
	pass.Dispatch((n+63)/64, 1, 1)
	pass.End()
	queue.Submit(enc.Finish())

	out, err := device.ReadBuffer(queue, buf, len(data))
	if err != nil {
		t.Fatalf("ReadBuffer: %v", err)
	}
	for i := range n {
		got := math.Float32frombits(binary.LittleEndian.Uint32(out[i*4:]))
		if got != float32(i)*2 {
			t.Fatalf("values[%d] = %v, want %v", i, got, float32(i)*2)
		}
	}
}
