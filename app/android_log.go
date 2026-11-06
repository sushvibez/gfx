//go:build android

package app

/*
#include <stdlib.h>
#include "android_glue.h"
*/
import "C"

import (
	"bufio"
	"os"
	"syscall"
	"unsafe"
)

// A NativeActivity has no terminal: whatever the engine and the app
// write to stderr or stdout vanishes. Both are redirected into the
// Android log (tag "gfx") at startup, line by line, so `adb logcat -s gfx`
// shows every diagnostic the desktop build prints — the one thing a
// crash on a device needs.
func init() {
	r, w, err := os.Pipe()
	if err != nil {
		return
	}
	if err := syscall.Dup3(int(w.Fd()), 2, 0); err != nil {
		return
	}
	syscall.Dup3(int(w.Fd()), 1, 0)
	os.Stderr = os.NewFile(2, "/dev/stderr")
	os.Stdout = os.NewFile(1, "/dev/stdout")
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				continue
			}
			cs := C.CString(line)
			C.gfx_log(cs)
			C.free(unsafe.Pointer(cs))
		}
	}()
}
