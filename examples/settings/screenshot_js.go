//go:build js

package main

import (
	"fmt"

	"github.com/sushvibez/gfx/ui/shell"
)

func screenshot(shell.Options, *state, string) error {
	return fmt.Errorf("-screenshot is native-only; on the web just look at the canvas")
}
