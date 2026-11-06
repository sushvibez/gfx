//go:build js

package shell

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// Screenshot and ScreenshotUI need native GPU readback; js builds get a
// clear error instead of a build failure.
func Screenshot(Options, Hooks, string) error {
	return fmt.Errorf("shell: screenshots are native-only (js has no ReadTexture)")
}

func ScreenshotUI(Options, func() ui.Element, string) error {
	return fmt.Errorf("shell: screenshots are native-only (js has no ReadTexture)")
}
