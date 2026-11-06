//go:build android

package main

import "C"

// gfxAndroidMain is the render-thread entry the NativeActivity glue calls
// (see app/android_glue.c). func main above is unused on Android.
//
//export gfxAndroidMain
func gfxAndroidMain() {
	_ = run(0)
}
