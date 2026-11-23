//go:build !js

package gpu

import "testing"

// TestAllocatedBytes: the device's count follows its buffers and textures
// as they are created and released, and comes back to where it started.
func TestAllocatedBytes(t *testing.T) {
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
	d, err := adapter.RequestDevice()
	if err != nil {
		t.Skip("no GPU device:", err)
	}
	base := d.AllocatedBytes()
	want := base
	check := func(what string) {
		t.Helper()
		if got := d.AllocatedBytes(); got != want {
			t.Fatalf("after %s: AllocatedBytes = %d, want %d", what, got, want)
		}
	}
	b := d.CreateBuffer(BufferVertex|BufferCopyDst, 1<<20)
	want += 1 << 20
	check("a 1 MB buffer")
	tex := d.CreateTexture2D(256, 128, FormatRGBA8Unorm, TextureBinding|TextureCopyDst)
	want += 256 * 128 * 4
	check("a 256×128 RGBA8 texture")
	mips := d.CreateTexture2DMips(8, 8, FormatRGBA8Unorm, TextureBinding|TextureCopyDst, MipLevels(8, 8))
	want += (64 + 16 + 4 + 1) * 4
	check("an 8×8 texture with its mips")
	arr := d.CreateTexture2DArray(16, 16, 3, FormatR8Unorm, TextureBinding|TextureCopyDst)
	want += 16 * 16 * 3
	check("a 3-layer array")
	msaa := d.CreateMSAATexture(32, 32, FormatRGBA8Unorm, 4)
	want += 32 * 32 * 4 * 4
	check("a 4× multisampled target")
	rt := d.CreateRenderTexture(10, 10, FormatRGBA16Float)
	want += 10 * 10 * 8
	check("an RGBA16F render texture")

	b.Release()
	tex.Release()
	mips.Release()
	arr.Release()
	msaa.Release()
	rt.Release()
	want = base
	check("releasing them all")
}
