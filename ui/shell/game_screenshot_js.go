//go:build js

package shell

import "fmt"

// ScreenshotGame needs GPU readback (Device.ReadTexture), which the browser
// backend doesn't expose — capture screenshots with a native build instead.
func ScreenshotGame(Options, int, float64, func(*Frame) bool, func(int, *Input), string) error {
	return fmt.Errorf("shell: ScreenshotGame is not supported on js/wasm")
}
