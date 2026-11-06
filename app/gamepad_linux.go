//go:build !android

package app

// Linux gamepad via the kernel joystick interface (joydev): /dev/input/jsN
// delivers fixed 8-byte events — no cgo, no dependencies. Axis/button
// numbering follows the xpad driver (the de-facto layout for Xbox-style
// pads, which is what the standard mapping is defined against).

import (
	"encoding/binary"
	"os"
	"syscall"
)

type joyDev struct {
	f *os.File
	// raw accumulated state, remapped on snapshot
	buttons [32]bool
	axes    [16]int16
}

var joySlots [maxGamepads]*joyDev

// jsEvent is struct js_event: time u32, value s16, type u8, number u8.
const jsEventSize = 8

const (
	jsEventButton = 0x01
	jsEventAxis   = 0x02
	jsEventInit   = 0x80
)

func pollGamepads() [maxGamepads]GamepadState {
	var out [maxGamepads]GamepadState
	for i := range joySlots {
		if joySlots[i] == nil {
			// Probe (cheap open; fails fast when absent). Non-blocking so
			// reads drain without stalling the frame.
			f, err := os.OpenFile("/dev/input/js"+string(rune('0'+i)), os.O_RDONLY, 0)
			if err != nil {
				continue
			}
			if err := syscall.SetNonblock(int(f.Fd()), true); err != nil {
				f.Close()
				continue
			}
			joySlots[i] = &joyDev{f: f}
		}
		d := joySlots[i]
		if !d.drain() {
			d.f.Close()
			joySlots[i] = nil // unplugged; re-probed next poll
			continue
		}
		out[i] = d.snapshot()
	}
	return out
}

// drain consumes every pending event; false when the device is gone.
func (d *joyDev) drain() bool {
	var buf [jsEventSize * 64]byte
	for {
		n, err := d.f.Read(buf[:])
		if err != nil {
			if pe, ok := err.(*os.PathError); ok && pe.Err == syscall.EAGAIN {
				return true // no more events
			}
			return false // ENODEV etc: unplugged
		}
		for o := 0; o+jsEventSize <= n; o += jsEventSize {
			value := int16(binary.LittleEndian.Uint16(buf[o+4 : o+6]))
			typ := buf[o+6] &^ jsEventInit
			num := buf[o+7]
			switch typ {
			case jsEventButton:
				if int(num) < len(d.buttons) {
					d.buttons[num] = value != 0
				}
			case jsEventAxis:
				if int(num) < len(d.axes) {
					d.axes[num] = value
				}
			}
		}
	}
}

// snapshot remaps xpad numbering to the standard layout.
func (d *joyDev) snapshot() GamepadState {
	g := GamepadState{Connected: true}
	// xpad buttons: 0 A, 1 B, 2 X, 3 Y, 4 LB, 5 RB, 6 Back, 7 Start,
	// 8 Guide, 9 LS, 10 RS.
	std := [...]int{PadA, PadB, PadX, PadY, PadLB, PadRB, PadBack, PadStart, -1, PadLStick, PadRStick}
	for i, s := range std {
		if s >= 0 {
			g.Buttons[s] = d.buttons[i]
		}
	}
	norm := func(v int16) float32 { return float32(v) / 32767 }
	// xpad axes: 0 LX, 1 LY, 2 LT, 3 RX, 4 RY, 5 RT, 6 DPadX, 7 DPadY.
	g.Axes[0], g.Axes[1] = norm(d.axes[0]), norm(d.axes[1])
	g.Axes[2], g.Axes[3] = norm(d.axes[3]), norm(d.axes[4])
	g.Buttons[PadLT] = d.axes[2] > 0 // triggers report -32767..32767
	g.Buttons[PadRT] = d.axes[5] > 0
	g.Buttons[PadLeft] = d.axes[6] < -16384
	g.Buttons[PadRight] = d.axes[6] > 16384
	g.Buttons[PadUp] = d.axes[7] < -16384
	g.Buttons[PadDown] = d.axes[7] > 16384
	return g
}
