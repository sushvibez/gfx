//go:build linux || windows || js

// Devcheck is the on-device smoke test: it draws the triangle through the
// full stack and, on the way, exercises the pieces that were only ever
// compile-checked on a new platform — the adapter's limits, the system
// clipboard round trip, and a short audio tone — printing a PASS/FAIL line
// for each. On Android the lines reach `adb logcat -s gfx`.
//
//	go run ./devcheck -frames 60
//	scripts/build-apk.sh devcheck x86_64   # then adb install, logcat -s gfx
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	flag.BoolVar(&noAudio, "noaudio", false, "skip the audio check (bisecting)")
	flag.BoolVar(&noClip, "noclip", false, "skip the clipboard check (bisecting)")
	flag.Parse()
	if err := run(*frames); err != nil {
		fmt.Fprintln(os.Stderr, "devcheck:", err)
		os.Exit(1)
	}
}
