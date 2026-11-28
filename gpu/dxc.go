//go:build !js

package gpu

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// WGPUDx12Compiler / WGPUDxcMaxShaderModel values (wgpu.h v29).
const (
	dx12CompilerFxc      = 1
	dx12CompilerDxc      = 2
	dxcMaxShaderModel6_6 = 6
)

// dxcName is the DirectX Shader Compiler's DLL. scripts/fetch-wgpu.sh puts
// it in build/wgpu/ beside wgpu_native.dll; a packaged app ships it beside
// its executable. Only dxcompiler.dll is needed: since v1.8.2502 (wgpu's
// minimum) it no longer loads dxil.dll.
const dxcName = "dxcompiler.dll"

// dxcPath is the dxcompiler.dll DX12 should compile shaders with, or ""
// for FXC. DXC is several times faster than FXC (an app's pipelines in
// ~2-5 s rather than ~20 s) and is what wgpu recommends, but wgpu panics
// when the DLL it is pointed at cannot be loaded, so only a file that
// exists is used, and only our own copy (never one on the system PATH,
// whose version is unknown and may predate wgpu's minimum).
//
// GFX_DX12_COMPILER=fxc forces FXC; GFX_DXC_PATH names the DLL.
func dxcPath() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	exe, _ := os.Executable()
	p := findDXC(os.Getenv("GFX_DX12_COMPILER"), os.Getenv("GFX_DXC_PATH"), loadedLib, exe, isFile)
	if debugLog {
		if p == "" {
			fmt.Fprintln(os.Stderr, "gpu: DX12 shader compiler: FXC")
		} else {
			fmt.Fprintln(os.Stderr, "gpu: DX12 shader compiler: DXC", p)
		}
	}
	return p
}

// findDXC picks dxcompiler.dll: $GFX_DXC_PATH if set (and present), else
// beside the wgpu-native library that was loaded, else beside the
// executable or in its wgpu/ folder (where LibPaths looks too).
func findDXC(compiler, envPath, lib, exe string, exists func(string) bool) string {
	if compiler == "fxc" {
		return ""
	}
	if envPath != "" {
		if exists(envPath) {
			return envPath
		}
		fmt.Fprintf(os.Stderr, "gpu: GFX_DXC_PATH %s not found; using FXC\n", envPath)
		return ""
	}
	var dirs []string
	// A bare name was found on the system search path: no folder to try.
	if lib != "" && filepath.Base(lib) != lib {
		dirs = append(dirs, filepath.Dir(lib))
	}
	if exe != "" {
		d := filepath.Dir(exe)
		dirs = append(dirs, d, filepath.Join(d, "wgpu"))
	}
	for _, d := range dirs {
		if p := filepath.Join(d, dxcName); exists(p) {
			return p
		}
	}
	return ""
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
