package plat

import (
	"os"
	"path/filepath"
)

// findBuildLib looks for build/wgpu/name in the working directory and
// each of its parents, so a program or test run from any depth of a
// checkout (a module root, a cmd/ directory, a package) finds the library
// that scripts/fetch-wgpu.sh put at the top. It returns "" when none has it.
func findBuildLib(name string) string {
	dir, err := filepath.Abs(".")
	if err != nil {
		return ""
	}
	for {
		p := filepath.Join(dir, "build", "wgpu", name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
