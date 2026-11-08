//go:build !js && (linux || windows)

package main

import "github.com/sushvibez/gfx/ui/shell"

func screenshot(o shell.Options, s *state, path string) error {
	return shell.ScreenshotUI(o, s.view, path)
}
