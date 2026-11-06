//go:build !js

package gpu

import (
	"testing"
	"time"
)

// TestUploadStaysFastAfterTextureBurst guards against the wgpu-native v29
// staging cliff: a burst of >1 MB texture writes in one submission used to
// leave every later WriteBuffer ~100× slower for the life of the device
// (0.4 ms → 50 ms per 900 KB). Banded texture writes + the automatic flush
// keep the fast path.
//
// The budget is relative: the same 900 KB write is timed on the same
// device before and after the burst, and the test fails only when the
// burst made it an order of magnitude slower. A fixed budget flaked under
// load — the write measures 0.6 ms alone and ~90 ms with the module's
// other GPU tests competing for the device and the CPU — while the cliff
// it guards is a 100× step that a ratio sees through any amount of load.
// A write that stays under the absolute fast-path figure passes outright,
// so a noisy baseline cannot fail a healthy device either.
//
// Honest limit: on 2026-10-10 the cliff itself could not be reproduced on
// this machine with the same wgpu-native v29 — thirty raw 4 MB texture
// writes, or ten frames of twelve raw 4 MB buffer writes, left the 900 KB
// write unchanged — so this test is a tripwire for the shape of failure
// seen in September, not a demonstrated catch.
func TestUploadStaysFastAfterTextureBurst(t *testing.T) {
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
	device, _ := adapter.RequestDevice()
	queue := device.Queue()
	data := make([]byte, 900*1024)
	buf := device.CreateBuffer(BufferVertex|BufferCopyDst, len(data))
	defer buf.Release()
	// One 900 KB write + submit + tick, averaged over four after a warm-up
	// (the first write grows the staging belt).
	write := func() float64 {
		var el time.Duration
		for f := 0; f < 5; f++ {
			start := time.Now()
			queue.WriteBuffer(buf, 0, data)
			enc := device.CreateCommandEncoder()
			queue.Submit(enc.Finish())
			device.Tick()
			if f > 0 {
				el += time.Since(start)
			}
		}
		return el.Seconds() * 1000 / 4
	}
	before := write()

	pix := make([]byte, 1024*1024*4)
	for i := 0; i < 30; i++ {
		tex := device.CreateTexture2D(1024, 1024, FormatRGBA8Unorm, TextureCopyDst|TextureBinding)
		queue.WriteTexture(tex, 1024, 1024, pix)
		tex.Release()
	}
	after := write()

	const fastMS, maxRatio = 10.0, 8.0
	t.Logf("900 KB write: %.2f ms before the 120 MB texture burst, %.2f ms after (×%.1f)", before, after, after/before)
	if after > fastMS && after > maxRatio*before {
		t.Fatalf("staging path degraded: %.2f ms per 900 KB write after the burst, %.2f ms before (×%.1f, want < ×%.0f or < %.0f ms)",
			after, before, after/before, maxRatio, fastMS)
	}
}
