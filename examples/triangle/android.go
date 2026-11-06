//go:build android

package main

import "C"

// gfxAndroidMain is the render-thread entry the NativeActivity glue
// (app/android_glue.c) calls once the Go runtime and the activity are up.
// This is the Android equivalent of func main(); a future `gfx pack`
// could generate it.
//
//export gfxAndroidMain
func gfxAndroidMain() {
	_ = run(0)
}
