//go:build !js

package gpu

import (
	"bytes"
	"testing"
)

// ReadTexture returns each format's own texels, tightly packed: one byte
// for R8, four for RGBA8, eight for RGBA16Float. A width of 70 makes every
// row need padding to the copy's 256-byte alignment.
func TestReadTextureFormats(t *testing.T) {
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
	const w, h = 70, 3
	for _, f := range []TextureFormat{FormatR8Unorm, FormatRGBA8Unorm, FormatRGBA16Float} {
		bpt := BytesPerTexel(f)
		want := make([]byte, w*h*bpt)
		for i := range want {
			want[i] = byte(i*7 + 3)
		}
		if f == FormatRGBA16Float {
			// Keep every half finite: clear the exponent's top bit so no
			// value is an infinity or NaN that a copy may canonicalise.
			for i := 1; i < len(want); i += 2 {
				want[i] &= 0x3f
			}
		}
		tex := device.CreateTexture2D(w, h, f, TextureCopySrc|TextureCopyDst|TextureBinding)
		queue.WriteTexture(tex, w, h, want)
		got, err := device.ReadTexture(queue, tex, w, h)
		tex.Release()
		if err != nil {
			t.Fatalf("%s: %v", FormatName(f), err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: read back %d bytes that differ from the %d written", FormatName(f), len(got), len(want))
		}
	}
}

func TestFormatNames(t *testing.T) {
	for _, name := range []string{"rgba8unorm", "bgra8unorm", "r8unorm", "rgba16float", "depth32float"} {
		f, ok := FormatByName(name)
		if !ok || FormatName(f) != name {
			t.Errorf("%q round-trips to %q (ok %v)", name, FormatName(f), ok)
		}
	}
	if _, ok := FormatByName("nope"); ok {
		t.Error("an unknown name resolved")
	}
}
