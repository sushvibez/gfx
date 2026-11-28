//go:build !js

package gpu

import (
	"path/filepath"
	"testing"
)

// TestFindDXC: the DLL is taken from GFX_DXC_PATH, then beside the loaded
// wgpu-native, then beside the executable; FXC (no path) when forced or
// when none is there — never a path that does not exist, which wgpu would
// panic on.
func TestFindDXC(t *testing.T) {
	lib := filepath.Join("w", "build", "wgpu", "wgpu_native.dll")
	exe := filepath.Join("app", "cqb.exe")
	besideLib := filepath.Join("w", "build", "wgpu", dxcName)
	besideExe := filepath.Join("app", dxcName)
	inExeWgpu := filepath.Join("app", "wgpu", dxcName)
	has := func(files ...string) func(string) bool {
		return func(p string) bool {
			for _, f := range files {
				if p == f {
					return true
				}
			}
			return false
		}
	}
	for _, c := range []struct {
		name, compiler, env, lib string
		files                    []string
		want                     string
	}{
		{"beside lib", "", "", lib, []string{besideLib, besideExe}, besideLib},
		{"beside exe", "", "", lib, []string{besideExe}, besideExe},
		{"exe wgpu/", "", "", lib, []string{inExeWgpu}, inExeWgpu},
		{"none", "", "", lib, nil, ""},
		{"system lib", "", "", "wgpu_native.dll", []string{besideExe}, besideExe},
		{"env", "", "x.dll", lib, []string{"x.dll", besideLib}, "x.dll"},
		{"env missing", "", "x.dll", lib, []string{besideLib}, ""},
		{"forced fxc", "fxc", "", lib, []string{besideLib}, ""},
	} {
		if got := findDXC(c.compiler, c.env, c.lib, exe, has(c.files...)); got != c.want {
			t.Errorf("%s: findDXC = %q, want %q", c.name, got, c.want)
		}
	}
}
