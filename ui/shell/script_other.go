//go:build !(linux || windows)

package shell

import "fmt"

// RunScript drives a headless screenshot pass through an interaction
// script; it needs native GPU readback, so the other targets get a clear
// error instead of a build failure.
func RunScript(Options, Hooks, string, string) error {
	return fmt.Errorf("shell: scripted screenshots are native-only")
}
