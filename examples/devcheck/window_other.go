//go:build !android

package main

import (
	"fmt"

	"github.com/sushvibez/gfx/app"
)

func describeWindow(w *app.Window) string {
	ww, hh := w.Size()
	return fmt.Sprintf("%dx%d", ww, hh)
}
