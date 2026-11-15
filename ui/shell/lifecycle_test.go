//go:build !js

package shell

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
	"github.com/sushvibez/gfx/ui"
)

// needGPU skips where wgpu-native is not installed.
func needGPU(t *testing.T) {
	t.Helper()
	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		t.Skip("no wgpu-native: ", err)
	}
	inst.Release()
}

// A process can take capture after capture: each releases what it made,
// the app's own hook included, and the next starts clean.
func TestRepeatedCaptures(t *testing.T) {
	needGPU(t)
	dir := t.TempDir()
	closed := 0
	hooks := Hooks{
		Init:  func(*App) (func() ui.Element, error) { return func() ui.Element { return ui.Div().W(40).H(40) }, nil },
		Close: func(*App) { closed++ },
	}
	for i := 0; i < 4; i++ {
		out := filepath.Join(dir, "shot.png")
		if err := Screenshot(Options{Width: 160, Height: 120}, hooks, out); err != nil {
			t.Fatalf("capture %d: %v", i, err)
		}
		if st, err := os.Stat(out); err != nil || st.Size() == 0 {
			t.Fatalf("capture %d wrote nothing: %v", i, err)
		}
	}
	if closed != 4 {
		t.Fatalf("Close ran %d times for 4 captures", closed)
	}
}

// An error from a frame comes back from the capture, after the app's hook
// and the shell's own releases have run; a failed Init releases too and
// does not call Close.
func TestCaptureErrorsComeBack(t *testing.T) {
	needGPU(t)
	boom := errors.New("frame failed")
	closed := 0
	hooks := Hooks{
		Init:     func(*App) (func() ui.Element, error) { return func() ui.Element { return ui.Div() }, nil },
		Encode3D: func(*App, *gpu.CommandEncoder, *gpu.TextureView, *gpu.TextureView) error { return boom },
		Close:    func(*App) { closed++ },
	}
	err := Screenshot(Options{Width: 64, Height: 64}, hooks, filepath.Join(t.TempDir(), "x.png"))
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the frame's error", err)
	}
	if closed != 1 {
		t.Fatalf("Close ran %d times after a failed frame", closed)
	}
	initErr := errors.New("init failed")
	hooks.Init = func(*App) (func() ui.Element, error) { return nil, initErr }
	if err := Screenshot(Options{Width: 64, Height: 64}, hooks, filepath.Join(t.TempDir(), "y.png")); !errors.Is(err, initErr) {
		t.Fatalf("got %v, want Init's error", err)
	}
	if closed != 1 {
		t.Fatal("Close ran although Init failed")
	}
}

func TestReleasesRunInReverse(t *testing.T) {
	var r releases
	var got []int
	for i := 0; i < 3; i++ {
		r.add(func() { got = append(got, i) })
	}
	r.run()
	r.run() // a second run releases nothing twice
	if len(got) != 3 || got[0] != 2 || got[2] != 0 {
		t.Fatalf("released in order %v", got)
	}
}
