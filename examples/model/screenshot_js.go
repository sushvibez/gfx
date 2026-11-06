//go:build js

package main

import "fmt"

func screenshot(string, string) error {
	return fmt.Errorf("-screenshot is native-only; on the web just look at the canvas")
}
