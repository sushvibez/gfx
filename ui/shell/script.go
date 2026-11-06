//go:build linux || windows

package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// An interaction script drives a page the way a person would — click a
// tab, drag a slider, type into a field — and captures a PNG after every
// step. Screenshots alone only prove a page LOOKS right when it opens;
// these prove it still does once touched, which is where the parity gaps
// that matter to a user live.
//
// One step per line (blank lines and #-comments ignored):
//
//	click 120,60          press and release at a point
//	press 120,60          press and hold
//	move 200,60           move the pointer (hover states, drag tracking)
//	release 200,60        let go
//	drag 40,80 200,80     press-drag-release across a drag region
//	fling 40,80 40,-60     flick with velocity (list momentum)
//	wheel 200,300 -120     scroll (positive scrolls content down)
//	key Enter             a named key, optionally ctrl+/shift+/alt+ prefixed
//	type hello            each rune as a key event
//	wait                  no input, just settle and capture
//
// A wheel delta is in Qt's units (120 = one notch), converted here to the
// pixels our platform layer delivers, so the same number means the same
// gesture on both sides.
//
// Steps run in order; step N's PNG is <base>-NN-<step>.png.
type step struct {
	line string
	run  func(u *ui.UI)
}

// wheelNotchPx is what one wheel notch is worth in pixels — the constant
// the x11 and win32 backends deliver (app.wheelNotchPx), measured from Qt:
// one notch moves a ListView 72 pixels.
const wheelNotchPx = 72

var namedKeys = map[string]app.Key{
	"backspace": app.KeyBackspace, "delete": app.KeyDelete,
	"left": app.KeyLeft, "right": app.KeyRight, "up": app.KeyUp, "down": app.KeyDown,
	"home": app.KeyHome, "end": app.KeyEnd, "enter": app.KeyEnter, "return": app.KeyEnter,
	"tab": app.KeyTab, "escape": app.KeyEscape, "esc": app.KeyEscape,
	"pageup": app.KeyPageUp, "pagedown": app.KeyPageDown, "insert": app.KeyInsert,
	"f1": app.KeyF1, "f2": app.KeyF2, "f3": app.KeyF3, "f4": app.KeyF4,
	"f5": app.KeyF5, "f6": app.KeyF6, "f7": app.KeyF7, "f8": app.KeyF8,
	"f9": app.KeyF9, "f10": app.KeyF10, "f11": app.KeyF11, "f12": app.KeyF12,
}

// parseScript turns the text into steps, reporting the first bad line
// rather than silently skipping it — a typo in a script that still "passes"
// is worse than no script.
func ParseScript(text string) ([]step, error) {
	var steps []step
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		s, err := parseStep(line)
		if err != nil {
			return nil, fmt.Errorf("script line %d: %w", n+1, err)
		}
		steps = append(steps, s)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("script has no steps")
	}
	return steps, nil
}

func parseStep(line string) (step, error) {
	fields := strings.Fields(line)
	verb, args := strings.ToLower(fields[0]), fields[1:]
	mouse := func(evs ...app.MouseEvent) func(*ui.UI) {
		return func(u *ui.UI) { u.Mouse(evs) }
	}
	switch verb {
	case "wait":
		return step{line, func(*ui.UI) {}}, nil
	case "click", "press", "release", "move":
		p, err := point(args, 1)
		if err != nil {
			return step{}, err
		}
		down := app.MouseEvent{Kind: app.MouseDown, X: p[0], Y: p[1], Button: 1}
		up := app.MouseEvent{Kind: app.MouseUp, X: p[0], Y: p[1], Button: 1}
		mv := app.MouseEvent{Kind: app.MouseMove, X: p[0], Y: p[1]}
		switch verb {
		case "click":
			if os.Getenv("GFX_PROBE") != "" {
				return step{line, func(u *ui.UI) {
					for yy := 160; yy <= 230; yy += 5 {
						row := ""
						for xx := 60; xx <= 430; xx += 10 {
							if u.Click(float32(xx), float32(yy)) {
								row += "X"
							} else {
								row += "."
							}
						}
						fmt.Println("probe y=", yy, row)
					}
				}}, nil
			}
			// Through the real event path, not UI.Click: a press-then-release
			// is what sets hover and pressed state, and what a control's
			// click-on-release semantics are written against.
			return step{line, mouse(mv, down, up)}, nil
		case "press":
			return step{line, mouse(mv, down)}, nil
		case "release":
			return step{line, mouse(mv, up)}, nil
		default:
			return step{line, mouse(mv)}, nil
		}
	case "drag", "fling":
		p, err := point(args, 2)
		if err != nil {
			return step{}, err
		}
		x0, y0, x1, y1 := p[0], p[1], p[2], p[3]
		if verb == "drag" {
			// Raw pointer motion, the way a mouse delivers it: press, a few
			// moves, release. Never UI.Drag — that hands the gesture to the
			// topmost DRAG REGION containing the start point, which on a
			// scrollable page is the flickable, so dragging a slider
			// scrolled the page instead. The real event path lets the UI
			// apply its own priority (slider handle, scrollbar thumb, drag
			// region) exactly as it does for a person.
			return step{line, func(u *ui.UI) {
				const steps = 4
				evs := []app.MouseEvent{
					{Kind: app.MouseMove, X: x0, Y: y0},
					{Kind: app.MouseDown, X: x0, Y: y0, Button: 1},
				}
				px, py := x0, y0
				for i := 1; i <= steps; i++ {
					t := float32(i) / steps
					x, y := x0+(x1-x0)*t, y0+(y1-y0)*t
					evs = append(evs, app.MouseEvent{Kind: app.MouseMove, X: x, Y: y, DX: x - px, DY: y - py})
					px, py = x, y
				}
				evs = append(evs, app.MouseEvent{Kind: app.MouseUp, X: x1, Y: y1, Button: 1})
				u.Mouse(evs)
			}}, nil
		}
		// A flick's velocity is the gesture itself: cover the distance in a
		// tenth of a second, which is what a real flick does.
		return step{line, func(u *ui.UI) { u.Fling(x0, y0, x1, y1, (x1-x0)*10, (y1-y0)*10) }}, nil
	case "wheel":
		if len(args) != 2 {
			return step{}, fmt.Errorf("wheel wants X,Y DELTA")
		}
		p, err := point(args[:1], 1)
		if err != nil {
			return step{}, err
		}
		d, err := strconv.ParseFloat(args[1], 32)
		if err != nil {
			return step{}, fmt.Errorf("wheel delta %q: %w", args[1], err)
		}
		// The delta is in Qt's wheel units (120 per notch), so one script
		// drives both engines; our platforms deliver a notch as 48px.
		return step{line, mouse(app.MouseEvent{Kind: app.MouseScroll, X: p[0], Y: p[1],
			ScrollY: float32(d) / 120 * wheelNotchPx})}, nil
	case "key":
		if len(args) != 1 {
			return step{}, fmt.Errorf("key wants one NAME (ctrl+a, Enter)")
		}
		ev, err := parseKey(args[0])
		if err != nil {
			return step{}, err
		}
		return step{line, func(u *ui.UI) {
			u.Key(ev)
			off := ev
			off.Down = false
			u.Key(off)
		}}, nil
	case "type":
		text := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		if text == "" {
			return step{}, fmt.Errorf("type wants text")
		}
		return step{line, func(u *ui.UI) {
			for _, r := range text {
				u.Key(app.KeyEvent{Down: true, Rune: r})
				u.Key(app.KeyEvent{Rune: r})
			}
		}}, nil
	}
	return step{}, fmt.Errorf("unknown step %q", fields[0])
}

// point reads n comma-separated X,Y pairs, one per field.
func point(args []string, n int) ([4]float32, error) {
	var out [4]float32
	if len(args) != n {
		return out, fmt.Errorf("want %d X,Y coordinate(s), got %d", n, len(args))
	}
	for i, a := range args {
		xy := strings.Split(a, ",")
		if len(xy) != 2 {
			return out, fmt.Errorf("coordinate %q is not X,Y", a)
		}
		for j, s := range xy {
			v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
			if err != nil {
				return out, fmt.Errorf("coordinate %q: %w", a, err)
			}
			out[i*2+j] = float32(v)
		}
	}
	return out, nil
}

func parseKey(spec string) (app.KeyEvent, error) {
	ev := app.KeyEvent{Down: true}
	parts := strings.Split(spec, "+")
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "ctrl", "control":
			ev.Ctrl = true
		case "shift":
			ev.Shift = true
		case "alt":
			ev.Alt = true
		default:
			return ev, fmt.Errorf("unknown modifier %q", m)
		}
	}
	name := parts[len(parts)-1]
	if k, ok := namedKeys[strings.ToLower(name)]; ok {
		ev.Key = k
		return ev, nil
	}
	if r := []rune(name); len(r) == 1 {
		ev.Rune = r[0]
		return ev, nil
	}
	return ev, fmt.Errorf("unknown key %q", name)
}

// stepName is the step's verb and coordinates, made safe for a filename,
// so a directory of PNGs reads as the script that produced it.
func stepName(s step) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		case r == ',', r == '+', r == ' ':
			return '-'
		}
		return -1
	}, s.line)
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return strings.Trim(name, "-")
}

// runScript plays the steps against a headless render, writing one PNG per
// step next to base: base itself for the untouched page, then
// base-01-<step>.png and so on.
//
// Exported so every front end can be driven the same way: the .gml runner
// (examples/qml -script) and the Go ports (examples/qmlgo/*) replay the
// same step files against the same harness.
func RunScript(opts Options, hooks Hooks, spec, base string) error {
	text := spec
	if strings.HasPrefix(spec, "@") {
		b, err := os.ReadFile(spec[1:])
		if err != nil {
			return err
		}
		text = string(b)
	}
	steps, err := ParseScript(text)
	if err != nil {
		return err
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".png"
	}
	return ScreenshotScript(opts, hooks, func(i int, u *ui.UI) (string, bool) {
		// Step 0 is the page as it opens, so a script's images always start
		// from the same baseline the static compare uses.
		if i == 0 {
			return base, len(steps) > 0
		}
		s := steps[i-1]
		s.run(u)
		return fmt.Sprintf("%s-%02d-%s%s", stem, i, stepName(s), ext), i < len(steps)
	})
}
