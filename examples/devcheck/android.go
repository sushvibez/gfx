//go:build android

package main

import "C"

// gfxAndroidMain is the render-thread entry the NativeActivity glue
// (app/android_glue.c) calls once the Go runtime and the activity are up.
//
//export gfxAndroidMain
func gfxAndroidMain() {
	_ = run(600)
}
