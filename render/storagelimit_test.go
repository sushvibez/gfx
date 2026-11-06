package render

import (
	"testing"

	"github.com/sushvibez/gfx/gpu"
)

// A storage buffer doubles as it grows, but never past what a bind group
// can bind: 140 MB of quads used to ask for 279 MB.
func TestStorageGrowStopsAtTheBindingLimit(t *testing.T) {
	const mb = 1 << 20
	if got := storageGrow(1*mb, 1*mb+1); got != 2*mb {
		t.Errorf("small growth = %d, want a doubling", got)
	}
	if got := storageGrow(100*mb, 101*mb); got > gpu.MaxStorageBindingSize {
		t.Errorf("grew to %d MB, past the %d MB limit", got/mb, gpu.MaxStorageBindingSize/mb)
	}
}

// A frame with more instances than a binding holds draws the ones that
// fit, whole, and says so once.
func TestFitStorageCutsWholeInstances(t *testing.T) {
	r := &Renderer2D{}
	stride := floatsPerQuad * 4
	small := make([]byte, 10*stride)
	if d, n := r.fitStorage(small, stride, "quads"); len(d) != len(small) || n != 10 {
		t.Errorf("small frame cut to %d bytes, %d instances", len(d), n)
	}
	big := make([]byte, gpu.MaxStorageBindingSize+5*stride)
	d, n := r.fitStorage(big, stride, "quads")
	if len(d) > gpu.MaxStorageBindingSize || len(d)%stride != 0 || n != len(d)/stride {
		t.Errorf("cut to %d bytes, %d instances", len(d), n)
	}
	if !r.overflowWarned {
		t.Error("the cut was not reported")
	}
}
