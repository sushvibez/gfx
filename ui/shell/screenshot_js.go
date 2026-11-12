//go:build js

package shell

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// Screenshot, ScreenshotUI and ScreenshotScript need native GPU readback; js builds get a
// clear error instead of a build failure.
func Screenshot(Options, Hooks, string) error {
	return fmt.Errorf("shell: screenshots are native-only (js has no ReadTexture)")
}

func ScreenshotUI(Options, func() ui.Element, string) error {
	return fmt.Errorf("shell: screenshots are native-only (js has no ReadTexture)")
}

// ScreenshotScript is native-only for the same reason.
func ScreenshotScript(Options, Hooks, func(i int, u *ui.UI) (string, bool)) error {
	return fmt.Errorf("shell: screenshots are native-only (js has no ReadTexture)")
}
