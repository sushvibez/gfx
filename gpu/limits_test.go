//go:build !js

package gpu

import "testing"

// TestDeviceLimitsFollowTheAdapter: a device is requested with its
// adapter's buffer-size limits, so a buffer past WebGPU's 256 MB default
// is valid wherever the GPU allows it.
func TestDeviceLimitsFollowTheAdapter(t *testing.T) {
	if err := Load(libPath(t)); err != nil {
		t.Skip(err)
	}
	inst, err := NewInstance()
	if err != nil {
		t.Skip(err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		t.Skip(err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		t.Fatal(err)
	}
	lim := device.Limits()
	t.Logf("maxBufferSize %d MB, maxStorageBufferBindingSize %d MB", lim.MaxBufferSize>>20, lim.MaxStorageBindingSize>>20)
	if lim.MaxBufferSize < MaxBufferSize || lim.MaxStorageBindingSize < MaxStorageBindingSize {
		t.Fatalf("limits below WebGPU's guaranteed minimums: %+v", lim)
	}
	if lim.MaxBufferSize <= MaxBufferSize {
		t.Skip("this adapter allows no more than the default; nothing to prove")
	}
	before := ErrorCount()
	size := min(int64(MaxBufferSize)+64<<20, lim.MaxBufferSize)
	buf := device.CreateBuffer(BufferStorage|BufferCopyDst, int(size))
	device.WaitIdle()
	if n := ErrorCount(); n != before {
		t.Fatalf("creating a %d MB buffer raised %d device errors", size>>20, n-before)
	}
	buf.Release()
}
