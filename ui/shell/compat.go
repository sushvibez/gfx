//go:build !js

package shell

import "github.com/sushvibez/gfx/ui"

// ScreenshotUI renders a plain ui view headlessly — the no-hooks
// counterpart of RunUI.
func ScreenshotUI(o Options, view func() ui.Element, path string) error {
	return Screenshot(o, Hooks{Init: func(*App) (func() ui.Element, error) { return view, nil }}, path)
}
