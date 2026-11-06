package app

import (
	"fmt"
	"os"
	"time"
)

// inputTrace (GFX_INPUT_TRACE=1) prints every pointer-related window
// message as it arrives — mouse buttons and moves, the wheel, WM_TOUCH,
// the WM_POINTER family and gesture messages — for finding out what a
// device really sends (a touchpad's tap, a pen) when it does not behave
// like a mouse.
var inputTrace = os.Getenv("GFX_INPUT_TRACE") != ""

var traceStart = time.Now()

var traceNames = map[uintptr]string{
	0x0200: "WM_MOUSEMOVE", 0x0201: "WM_LBUTTONDOWN", 0x0202: "WM_LBUTTONUP",
	0x0203: "WM_LBUTTONDBLCLK", 0x0204: "WM_RBUTTONDOWN", 0x0205: "WM_RBUTTONUP",
	0x0207: "WM_MBUTTONDOWN", 0x0208: "WM_MBUTTONUP", 0x020A: "WM_MOUSEWHEEL",
	0x020E: "WM_MOUSEHWHEEL", 0x0240: "WM_TOUCH", 0x0119: "WM_GESTURE",
	0x011A: "WM_GESTURENOTIFY", 0x0245: "WM_POINTERUPDATE", 0x0246: "WM_POINTERDOWN",
	0x0247: "WM_POINTERUP", 0x0249: "WM_POINTERENTER", 0x024A: "WM_POINTERLEAVE",
	0x024B: "WM_POINTERACTIVATE", 0x024C: "WM_POINTERCAPTURECHANGED",
	0x024E: "WM_POINTERWHEEL", 0x0215: "WM_CAPTURECHANGED", 0x0021: "WM_MOUSEACTIVATE",
	0x02A1: "WM_MOUSEHOVER", 0x02A3: "WM_MOUSELEAVE", 0x00A1: "WM_NCLBUTTONDOWN",
	0x00A2: "WM_NCLBUTTONUP",
}

func traceInput(msg, wparam, lparam uintptr) {
	name, ok := traceNames[msg]
	if !ok {
		return
	}
	x, y := int16(lparam&0xFFFF), int16((lparam>>16)&0xFFFF)
	fmt.Fprintf(os.Stderr, "input %8.3fs %-24s wparam=%#x x=%d y=%d\n",
		time.Since(traceStart).Seconds(), name, wparam, x, y)
}
