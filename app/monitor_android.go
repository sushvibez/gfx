//go:build android

package app

import "errors"

// Android apps get one surface covering the device screen; there is no
// monitor layout to enumerate (external displays go through the Presentation
// API, which this backend does not wire up). Monitors reports that one
// screen so portable code can ask unconditionally.
func Monitors() ([]Monitor, error) {
	if androidScreenW <= 0 || androidScreenH <= 0 {
		return nil, errors.New("app: no Android surface yet (call NewWindow first)")
	}
	scale := scaleOverride()
	if scale <= 0 {
		scale = 1
	}
	return []Monitor{{
		Name:    "screen",
		Width:   androidScreenW,
		Height:  androidScreenH,
		Scale:   scale,
		Primary: true,
	}}, nil
}

// SetFullscreen is a no-op: an Android activity surface is already
// fullscreen (immersive-mode system bars are an activity-theme concern).
func (w *Window) SetFullscreen(bool) {}

// Fullscreen is always true on Android.
func (w *Window) Fullscreen() bool { return true }

// MonitorIndex is always 0: there is exactly one reported screen.
func (w *Window) MonitorIndex() int { return 0 }
