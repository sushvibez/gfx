//go:build !js

package gpu

import "testing"

// TestSubmitDropsFailedEncoding: a frame drawn into an invalid target (as
// after a refused allocation) is dropped by Submit and counted, instead of
// wgpu aborting the process at the submit.
func TestSubmitDropsFailedEncoding(t *testing.T) {
	if err := Load(libPath(t)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	inst, err := NewInstance()
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Skip("no GPU adapter:", err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Skip("no GPU device:", err)
	}
	queue := device.Queue()
	// A texture over the device's limit is an invalid object, as a
	// texture the device had no memory for is.
	bad := device.CreateRenderTexture(1<<16, 4, FormatRGBA8Unorm)
	view := bad.CreateView()
	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, Color{A: 1})
	pass.End()
	cb := enc.Finish()
	if !cb.failed {
		t.Fatal("the encoding into an invalid target was not seen to fail")
	}
	dropped := DroppedSubmits()
	queue.Submit(cb) // must not abort
	if DroppedSubmits() != dropped+1 {
		t.Fatalf("DroppedSubmits = %d, want %d", DroppedSubmits(), dropped+1)
	}
	// A good frame after it still goes.
	good := device.CreateRenderTexture(4, 4, FormatRGBA8Unorm)
	gv := good.CreateView()
	enc = device.CreateCommandEncoder()
	pass = enc.BeginRenderPass(gv, Color{A: 1})
	pass.End()
	cb = enc.Finish()
	if cb.failed {
		t.Fatal("a valid encoding was marked failed")
	}
	queue.Submit(cb)
	if DroppedSubmits() != dropped+1 {
		t.Fatal("a valid command buffer was dropped")
	}
}

func TestIsOutOfMemory(t *testing.T) {
	if !IsOutOfMemory(2, "Validation Error\n\nCaused by:\n  In wgpuDeviceCreateBuffer\n    Not enough memory left.") {
		t.Error("wgpu's 'Not enough memory left' is out of memory")
	}
	if !IsOutOfMemory(3, "") {
		t.Error("WGPUErrorType_OutOfMemory is out of memory")
	}
	if IsOutOfMemory(2, "Buffer with '' label is invalid") {
		t.Error("an invalid object is not itself out of memory")
	}
}

// TestSimulatedVRAM: with GFX_SIM_VRAM_MB's limit reached, TryCreateBuffer
// is nil and counted as out of memory, and a released buffer gives its
// room back.
func TestSimulatedVRAM(t *testing.T) {
	if err := Load(libPath(t)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	inst, err := NewInstance()
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Skip("no GPU adapter:", err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Skip("no GPU device:", err)
	}
	old := simVRAM
	simVRAM = 8 << 20
	defer func() { simVRAM = old }()
	a := device.TryCreateBuffer(BufferVertex|BufferCopyDst, 6<<20)
	if a == nil {
		t.Fatal("6 MB of 8 refused")
	}
	oom := OutOfMemoryCount()
	if b := device.TryCreateBuffer(BufferVertex|BufferCopyDst, 4<<20); b != nil {
		t.Fatal("10 MB of 8 given")
	}
	if OutOfMemoryCount() != oom+1 {
		t.Fatalf("refusal not counted as out of memory: %d → %d", oom, OutOfMemoryCount())
	}
	a.Release()
	b := device.TryCreateBuffer(BufferVertex|BufferCopyDst, 4<<20)
	if b == nil {
		t.Fatal("refused after the room was given back")
	}
	b.Release()
}
