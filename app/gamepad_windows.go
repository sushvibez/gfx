package app

// Windows gamepad via XInput (Xbox-style pads; the standard layout maps 1:1),
// with a WinMM (joyGetPosEx) fallback for flight sticks and other
// DirectInput-only devices, which XInput never sees. Both DLLs load lazily
// on first poll; absence just reports no pads.

import (
	"sync"
	"syscall"
	"unsafe"
)

var (
	xinputOnce sync.Once
	xinputGet  *syscall.LazyProc
)

// xinputState mirrors XINPUT_STATE + XINPUT_GAMEPAD.
type xinputState struct {
	packet  uint32
	buttons uint16
	lt, rt  uint8
	lx, ly  int16
	rx, ry  int16
}

const (
	xiUp    = 0x0001
	xiDown  = 0x0002
	xiLeft  = 0x0004
	xiRight = 0x0008
	xiStart = 0x0010
	xiBack  = 0x0020
	xiLS    = 0x0040
	xiRS    = 0x0080
	xiLB    = 0x0100
	xiRB    = 0x0200
	xiA     = 0x1000
	xiB     = 0x2000
	xiX     = 0x4000
	xiY     = 0x8000
)

func pollGamepads() [maxGamepads]GamepadState {
	xinputOnce.Do(func() {
		for _, name := range []string{"xinput1_4.dll", "xinput1_3.dll", "xinput9_1_0.dll"} {
			dll := syscall.NewLazyDLL(name)
			p := dll.NewProc("XInputGetState")
			if p.Find() == nil {
				xinputGet = p
				return
			}
		}
	})
	var out [maxGamepads]GamepadState
	used := 0
	if xinputGet == nil {
		return pollWinMM(out, used)
	}
	for i := range out {
		var st xinputState
		r, _, _ := xinputGet.Call(uintptr(i), uintptr(unsafe.Pointer(&st)))
		if r != 0 { // ERROR_SUCCESS == 0
			continue
		}
		g := &out[i]
		g.Connected = true
		for _, m := range [...]struct {
			mask uint16
			btn  int
		}{
			{xiA, PadA}, {xiB, PadB}, {xiX, PadX}, {xiY, PadY},
			{xiLB, PadLB}, {xiRB, PadRB}, {xiBack, PadBack}, {xiStart, PadStart},
			{xiLS, PadLStick}, {xiRS, PadRStick},
			{xiUp, PadUp}, {xiDown, PadDown}, {xiLeft, PadLeft}, {xiRight, PadRight},
		} {
			g.Buttons[m.btn] = st.buttons&m.mask != 0
		}
		g.Buttons[PadLT] = st.lt > 30
		g.Buttons[PadRT] = st.rt > 30
		norm := func(v int16) float32 { return float32(v) / 32767 }
		// XInput Y axes are up-positive; the standard layout is up-negative.
		g.Axes[0], g.Axes[1] = norm(st.lx), -norm(st.ly)
		g.Axes[2], g.Axes[3] = norm(st.rx), -norm(st.ry)
		g.Raw = [6]float32{norm(st.lx), -norm(st.ly), norm(st.rx), -norm(st.ry), float32(st.lt)/255*2 - 1, float32(st.rt)/255*2 - 1}
		used = i + 1
	}
	return pollWinMM(out, used)
}

// ---- WinMM joystick fallback -------------------------------------------

var (
	winmmOnce   sync.Once
	joyGetPosEx *syscall.LazyProc
	joyNumDevs  *syscall.LazyProc
	joyCount    int
)

// joyInfoEx mirrors JOYINFOEX.
type joyInfoEx struct {
	size, flags                uint32
	x, y, z, r, u, v           uint32
	buttons, buttonNumber, pov uint32
	reserved1, reserved2       uint32
}

const (
	joyReturnAll = 0x000000FF // JOY_RETURNX..POV | JOY_RETURNBUTTONS
	joyPovCentre = 0xFFFF
)

// pollWinMM fills the free pad slots from WinMM joysticks. XInput devices
// also show up in WinMM; they are skipped when XInput already reported a
// pad (the XInput slots are always the first ones).
func pollWinMM(out [maxGamepads]GamepadState, used int) [maxGamepads]GamepadState {
	winmmOnce.Do(func() {
		dll := syscall.NewLazyDLL("winmm.dll")
		g, n := dll.NewProc("joyGetPosEx"), dll.NewProc("joyGetNumDevs")
		if g.Find() == nil && n.Find() == nil {
			joyGetPosEx, joyNumDevs = g, n
			c, _, _ := joyNumDevs.Call()
			joyCount = int(c)
			if joyCount > 16 {
				joyCount = 16
			}
		}
	})
	if joyGetPosEx == nil || used >= len(out) {
		return out
	}
	slot := used
	for id := 0; id < joyCount && slot < len(out); id++ {
		var ji joyInfoEx
		ji.size = uint32(unsafe.Sizeof(ji))
		ji.flags = joyReturnAll
		r, _, _ := joyGetPosEx.Call(uintptr(id), uintptr(unsafe.Pointer(&ji)))
		if r != 0 { // JOYERR_NOERROR == 0
			continue
		}
		if used > 0 && id < used {
			continue // an XInput pad seen through WinMM
		}
		g := &out[slot]
		slot++
		g.Connected, g.Joystick = true, true
		norm := func(v uint32) float32 { return float32(v)/32767.5 - 1 }
		g.Axes[0], g.Axes[1] = norm(ji.x), norm(ji.y) // stick: right/back positive
		g.Axes[2] = norm(ji.r)                        // twist / rudder
		g.Axes[3] = norm(ji.z)
		g.Throttle = -norm(ji.z) // slider forward (low raw value) = +1
		g.Raw = [6]float32{norm(ji.x), norm(ji.y), norm(ji.z), norm(ji.r), norm(ji.u), norm(ji.v)}
		for b := 0; b < 16 && b < padButtons; b++ {
			g.Buttons[b] = ji.buttons&(1<<uint(b)) != 0
		}
		if ji.pov != joyPovCentre && ji.pov <= 36000 {
			// POV hat in hundredths of a degree, 0 = up, clockwise.
			deg := float64(ji.pov) / 100
			g.Buttons[PadUp] = deg < 67.5 || deg > 292.5
			g.Buttons[PadRight] = deg > 22.5 && deg < 157.5
			g.Buttons[PadDown] = deg > 112.5 && deg < 247.5
			g.Buttons[PadLeft] = deg > 202.5 && deg < 337.5
		}
	}
	return out
}
