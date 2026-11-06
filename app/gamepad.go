package app

// Gamepad input, polled once per frame (Godot/Ebiten-style). Gamepads()
// returns a snapshot of every connected pad in the STANDARD layout — buttons
// and axes are remapped per backend so the same indices mean the same
// physical controls everywhere:
//
//	Buttons: 0 A/Cross  1 B/Circle  2 X/Square  3 Y/Triangle
//	         4 LB  5 RB  6 LT  7 RT  8 Back/Select  9 Start
//	         10 L-stick  11 R-stick  12 DPad-Up  13 DPad-Down
//	         14 DPad-Left  15 DPad-Right
//	Axes:    0 LX  1 LY  2 RX  3 RY   (-1..1, up/left negative)
//
// Backends: Linux joydev (/dev/input/js*), Windows XInput, web
// navigator.getGamepads. Hot-plug is handled by re-probing closed slots.
const (
	PadA = iota
	PadB
	PadX
	PadY
	PadLB
	PadRB
	PadLT
	PadRT
	PadBack
	PadStart
	PadLStick
	PadRStick
	PadUp
	PadDown
	PadLeft
	PadRight
	padButtons // count
)

// GamepadState is one pad's polled state in the standard layout.
type GamepadState struct {
	Connected bool
	Buttons   [padButtons]bool
	Axes      [4]float32
	// Joystick marks a flight-stick style device (Windows WinMM fallback
	// for non-XInput sticks): Axes[2] is the twist/rudder and Throttle the
	// slider (−1 back .. +1 forward), instead of a second thumb-stick.
	Joystick bool
	Throttle float32
	// Raw is every physical axis the backend saw, −1..1, for user mapping:
	// WinMM X Y Z R U V; XInput LX LY RX RY LT RT.
	Raw [6]float32
}

// Pressed reports button b (a Pad* constant); out-of-range is false.
func (g *GamepadState) Pressed(b int) bool {
	return b >= 0 && b < len(g.Buttons) && g.Buttons[b]
}

// maxGamepads is the number of pad slots polled.
const maxGamepads = 4

// Gamepads returns a snapshot of all pad slots (fixed length maxGamepads;
// check Connected). Call once per frame.
func Gamepads() [maxGamepads]GamepadState { return pollGamepads() }
