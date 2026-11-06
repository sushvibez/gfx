// xwarp moves the X pointer to a point on the root window and exits — a
// dev tool for the QML compare harness.
//
//	go run ./xwarp -display :1 -x 0 -y 0
//
// The reference renders open a real window on a real display (the software
// scene graph draws no effects), so Qt hands the page the ACTUAL desktop
// pointer: a page whose lens follows `mouseX`, or whose card highlights on
// hover, then depends on where the mouse happens to sit. Parking it in a
// corner makes that deterministic and matches our headless capture, which
// has no pointer at all.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sushvibez/gfx/ffi"
)

func main() {
	display := flag.String("display", "", "X display (default: $DISPLAY)")
	x := flag.Int("x", 0, "root-window x")
	y := flag.Int("y", 0, "root-window y")
	flag.Parse()

	lib, err := ffi.Open("libX11.so.6", "libX11.so")
	if err != nil {
		fmt.Fprintln(os.Stderr, "xwarp:", err)
		os.Exit(1)
	}
	var (
		xOpenDisplay       func(name *byte) uintptr
		xDefaultRootWindow func(d uintptr) uintptr
		xWarpPointer       func(d, src, dst uintptr, sx, sy int32, sw, sh uint32, dx, dy int32) int32
		xFlush             func(d uintptr) int32
		xCloseDisplay      func(d uintptr) int32
	)
	lib.Bind(&xOpenDisplay, "XOpenDisplay")
	lib.Bind(&xDefaultRootWindow, "XDefaultRootWindow")
	lib.Bind(&xWarpPointer, "XWarpPointer")
	lib.Bind(&xFlush, "XFlush")
	lib.Bind(&xCloseDisplay, "XCloseDisplay")

	name := *display
	if name == "" {
		name = os.Getenv("DISPLAY")
	}
	var namePtr *byte
	if name != "" {
		b := append([]byte(name), 0)
		namePtr = &b[0]
	}
	d := xOpenDisplay(namePtr)
	if d == 0 {
		fmt.Fprintf(os.Stderr, "xwarp: cannot open display %q\n", name)
		os.Exit(1)
	}
	root := xDefaultRootWindow(d)
	xWarpPointer(d, 0, root, 0, 0, 0, 0, int32(*x), int32(*y))
	xFlush(d)
	xCloseDisplay(d)
}
