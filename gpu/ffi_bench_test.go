//go:build !js

package gpu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/sushvibez/gfx/ffi"
)

// BenchmarkFFICall compares the cost of one real wgpu call pair
// (wgpuDeviceGetQueue + wgpuQueueRelease) through purego.RegisterFunc
// (reflect.MakeFunc, what ffi.Bind uses today) against purego.SyscallN.
func BenchmarkFFICall(b *testing.B) {
	path := os.Getenv("GFX_WGPU_LIB")
	if path == "" {
		path, _ = filepath.Abs(filepath.Join("..", "build", "wgpu", DefaultLibNames()[0]))
	}
	if err := Load(path); err != nil {
		b.Fatalf("Load: %v", err)
	}
	inst, err := NewInstance()
	if err != nil {
		b.Fatal(err)
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		b.Fatal(err)
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		b.Fatal(err)
	}
	lib, err := ffi.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	getQ, _ := lib.Sym("wgpuDeviceGetQueue")
	relQ, _ := lib.Sym("wgpuQueueRelease")

	b.Run("GeneratedStub", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			q := wgpuDeviceGetQueue(device.h)
			wgpuQueueRelease(q)
		}
	})
	b.Run("SyscallN", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			q, _, _ := purego.SyscallN(getQ, device.h)
			purego.SyscallN(relQ, q)
		}
	})
}
