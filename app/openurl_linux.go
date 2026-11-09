//go:build linux && !android

package app

import "os/exec"

// OpenURL opens url in the desktop's browser (xdg-open).
func OpenURL(url string) error { return exec.Command("xdg-open", url).Start() }
