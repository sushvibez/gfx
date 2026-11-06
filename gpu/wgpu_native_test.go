//go:build !js

package gpu

import (
	"os"
	"path/filepath"
	"testing"
)

// libPath finds the wgpu-native shared library: $GFX_WGPU_LIB, or the
// workspace-local copy that scripts/fetch-wgpu.sh downloads.
func libPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GFX_WGPU_LIB"); p != "" {
		return p
	}
	p, err := filepath.Abs(filepath.Join("..", "build", "wgpu", DefaultLibNames()[0]))
	if err == nil {
		if _, statErr := os.Stat(p); statErr == nil {
			return p
		}
	}
	t.Skip("wgpu-native not found; run scripts/fetch-wgpu.sh or set GFX_WGPU_LIB")
	return ""
}

func TestProbe(t *testing.T) {
	info, err := Probe(libPath(t))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	t.Logf("probe: %s", info)
	// wgpuGetVersion was removed in the 29.x ABI (returns 0); the adapter
	// name below is the meaningful liveness check.
	if info.Adapter == "" {
		t.Error("adapter name empty")
	}
	if info.Backend == "" || info.Backend == "undefined" {
		t.Errorf("backend = %q", info.Backend)
	}
}
