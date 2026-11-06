//go:build android || (!linux && !windows && !js)

package app

// No gamepad backend on this platform yet (Android arrives with the rest of
// its input work).
func pollGamepads() [maxGamepads]GamepadState { return [maxGamepads]GamepadState{} }
