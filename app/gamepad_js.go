package app

// Web gamepad via navigator.getGamepads(). Browsers already normalize to the
// W3C "standard" mapping, which matches our layout index-for-index (buttons
// 0-15, axes 0-3).

import "syscall/js"

func pollGamepads() [maxGamepads]GamepadState {
	var out [maxGamepads]GamepadState
	nav := js.Global().Get("navigator")
	if nav.IsUndefined() {
		return out
	}
	pads := nav.Call("getGamepads")
	if pads.IsUndefined() || pads.IsNull() {
		return out
	}
	n := pads.Length()
	for i := 0; i < n && i < maxGamepads; i++ {
		p := pads.Index(i)
		if p.IsUndefined() || p.IsNull() || !p.Get("connected").Bool() {
			continue
		}
		g := &out[i]
		g.Connected = true
		btns := p.Get("buttons")
		for b := 0; b < btns.Length() && b < len(g.Buttons); b++ {
			g.Buttons[b] = btns.Index(b).Get("pressed").Bool()
		}
		axes := p.Get("axes")
		for a := 0; a < axes.Length() && a < len(g.Axes); a++ {
			g.Axes[a] = float32(axes.Index(a).Float())
		}
	}
	return out
}
