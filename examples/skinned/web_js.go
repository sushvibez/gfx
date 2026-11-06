//go:build js

package main

import "fmt"

// The web build has no filesystem to load the model files from (they come
// from scripts/fetch-assets.sh into build/), so it says so instead of
// opening an empty canvas. Run this example natively.
func main() {
	fmt.Println("skinned: needs model files from disk; run it natively")
}
