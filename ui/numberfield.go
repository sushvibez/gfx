package ui

import (
	"math"
	"strconv"
	"strings"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
)

// dragShift and dragCtrl are the modifiers of the latest press or pointer
// move, for drag handlers (OnDrag is told only the distance).
var dragShift, dragCtrl bool

// DragModifiers reports whether Shift and Ctrl were held at the latest
// press or pointer move — what a drag handler reads to go fine or coarse.
func DragModifiers() (shift, ctrl bool) { return dragShift, dragCtrl }

// NumberOpts configures a NumberField. The zero value is an unbounded
// field stepping by 1 and showing up to three decimals.
type NumberOpts struct {
	// Min and Max bound the value when Max > Min (otherwise unbounded).
	Min, Max float64
	// Step is one arrow click or Up/Down key (zero: 1). A drag moves a
	// Step every PixelsPerStep pixels (zero: 6); Shift makes it ten times
	// finer, Ctrl snaps the dragged value to whole Steps.
	Step          float64
	PixelsPerStep float32
	// Decimals is the shown precision: that many places, or (negative) as
	// many as the value needs up to MaxDecimals (zero: 2). Typed text is
	// kept at full precision.
	Decimals, MaxDecimals int
	// Label is drawn on the left inside the field (the value then sits on
	// the right, as in Blender; without one the value is centred). Unit is
	// drawn after the box, outside it.
	Label, Unit string
	// OnModified runs after every change the user makes: each arrow click,
	// key, drag step and typed commit (Blender's live update).
	OnModified func()
	// OnDragEnd runs when a drag that changed the value lets go — the
	// moment to land an undo step for the whole drag.
	OnDragEnd func()
	Disabled  bool
	Width     float32 // zero: 160
}

// NumberState is a NumberField's retained state: whether it is being typed
// into, the text field it types into, and the value a drag started from.
type NumberState struct {
	Input   *InputState
	editing bool
	start   float64
	moved   bool
	opened  string // the text the edit opened with: committing it unchanged keeps the unrounded value
}

// NewNumberState returns the state a NumberField keeps between frames.
func NewNumberState() *NumberState { return &NumberState{Input: NewInput("")} }

// Editing reports whether the field is being typed into.
func (s *NumberState) Editing() bool { return s.editing }

// NumberField is a Blender-style number: one rounded box with the label on
// the left, the value in the middle and step arrows at both ends. Drag
// sideways on it to scrub the value; double-click it (or press Enter) to type (Enter or a click
// elsewhere commits, refused text puts the value back); click an arrow or
// press Up/Down (PageUp/PageDown ten steps) to step. Every change lands in
// value at once and calls OnModified.
func NumberField(value *core.Property[float64], st *NumberState, o NumberOpts) *DivEl {
	if o.Step <= 0 {
		o.Step = 1
	}
	if o.PixelsPerStep <= 0 {
		o.PixelsPerStep = 6
	}
	w := o.Width
	if w <= 0 {
		w = 160
	}
	bounded := o.Max > o.Min
	clampV := func(v float64) float64 {
		if bounded {
			v = math.Min(math.Max(v, o.Min), o.Max)
		}
		return v
	}
	set := func(v float64) {
		v = clampV(v)
		if v != value.Get() {
			value.Set(v)
			if o.OnModified != nil {
				o.OnModified()
			}
		}
	}
	tm := CurrentTheme()
	text := tm.Text
	dim := tm.Text
	dim.A *= 0.6
	cur := value.Get()
	shown := formatField(cur, o)
	const h = 26
	box := Div().Row().ItemsCenter().Size(w, h).Bg(tm.Field).Border(1, tm.Border).Rounded(5)
	box.Access(Access{Role: RoleSpinButton, Name: o.Label, Value: shown, Checked: -1, Expanded: -1, Disabled: o.Disabled})
	withUnit := func(el *DivEl) *DivEl {
		if o.Unit == "" {
			return el
		}
		return Div().Row().ItemsCenter().Gap(6).Children(el, Text(o.Unit, dim))
	}
	if o.Disabled {
		inner := Div().Grow().Padding(0, 8, 0, 8).Row().ItemsCenter()
		if o.Label != "" {
			inner.Children(Text(o.Label, dim), Div().Grow(), Text(shown, text))
		} else {
			inner.Children(Div().Grow(), Text(shown, text), Div().Grow())
		}
		return withUnit(box.Opacity(0.5).Children(inner))
	}

	if st.editing {
		in := st.Input
		finish := func(s string) {
			if !st.editing {
				return
			}
			st.editing = false
			if s == st.opened {
				return
			}
			if v, ok := ParseNumber(s); ok {
				set(v)
			}
		}
		in.OnSubmit(finish)
		in.OnBlur(finish)
		in.OnCancel(func() { st.editing = false }) // Escape: the value stays
		return withUnit(box.Children(Input(in, w-2).Bg(tm.Field).TextColor(text).RequestFocus()))
	}

	arrow := func(s string, n float64) *DivEl {
		return Div().Size(18, h).JustifyCenter().ItemsCenter().Cursor(app.CursorPointer).
			Children(Text(s, dim)).OnClick(func() { set(value.Get() + n*o.Step) })
	}
	middle := Div().Grow().Row().ItemsCenter().Size(0, h).Padding(0, 6, 0, 6).Cursor(app.CursorEW)
	if o.Label != "" {
		middle.Children(Text(o.Label, dim), Div().Grow(), Text(shown, text))
	} else {
		middle.Children(Div().Grow(), Text(shown, text), Div().Grow())
	}
	middle.OnDrag(func(dx, _ float32, phase DragPhase) {
		switch phase {
		case DragStart:
			st.start, st.moved = value.Get(), false
		case DragMove:
			if !st.moved && dx > -3 && dx < 3 {
				return // a click, so far
			}
			st.moved = true
			shift, ctrl := DragModifiers()
			steps := float64(dx / o.PixelsPerStep)
			if shift {
				steps /= 10
			}
			v := st.start + steps*o.Step
			if ctrl {
				v = math.Round(v/o.Step) * o.Step
			}
			set(v)
		case DragEnd:
			if st.moved && o.OnDragEnd != nil {
				o.OnDragEnd()
			}
		}
	})
	edit := func() {
		t := shown // at the field's precision, without fixed trailing zeros
		if o.Decimals > 0 {
			t = FormatNumberUpTo(cur, o.Decimals)
		}
		st.editing, st.opened = true, t
		st.Input.SetText(t)
		st.Input.SelectAll()
	}
	// A double-click types (a single click is half a drag-scrub, and the
	// release of a drag must not open the editor).
	middle.OnDoubleClick(func() {
		if !st.moved {
			edit()
		}
	})
	box.Children(arrow("‹", -1), middle, arrow("›", 1))
	out := withUnit(box)
	out.OnKeyAccept(func(ev app.KeyEvent) bool {
		if !ev.Down {
			return false
		}
		switch ev.Key {
		case app.KeyUp:
			set(value.Get() + o.Step)
		case app.KeyDown:
			set(value.Get() - o.Step)
		case app.KeyPageUp:
			set(value.Get() + 10*o.Step)
		case app.KeyPageDown:
			set(value.Get() - 10*o.Step)
		case app.KeyEnter:
			edit()
		default:
			return false
		}
		return true
	})
	return out
}

// formatField is a field's shown value under its precision options.
func formatField(v float64, o NumberOpts) string {
	if o.Decimals >= 0 {
		return FormatNumber(v, o.Decimals)
	}
	m := o.MaxDecimals
	if m <= 0 {
		m = 2
	}
	return FormatNumberUpTo(v, m)
}

// FormatNumberUpTo shows v with as few decimals as it needs, at most max,
// without a trailing ".0" or "-0".
func FormatNumberUpTo(v float64, max int) string {
	s := strconv.FormatFloat(v, 'f', max, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

// FormatNumber shows v with decimals places, or (decimals < 0) with as few
// as it needs, up to six, without a trailing ".0" or "-0".
func FormatNumber(v float64, decimals int) string {
	if decimals < 0 {
		s := strconv.FormatFloat(v, 'f', 6, 64)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		if s == "-0" {
			s = "0"
		}
		return s
	}
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if strings.Trim(s, "-0.") == "" {
		s = strings.TrimPrefix(s, "-")
	}
	return s
}

// ParseNumber reads typed text: a number, optionally with a unit suffix
// ("1.5 m", "90°") or a simple sum or product of two ("2*1.5", "10-0.25"),
// the arithmetic a value field is usually typed with.
func ParseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, " abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ°%")
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v, true
	}
	// One binary operator, not the sign of the first operand.
	for i := len(s) - 1; i > 0; i-- {
		op := s[i]
		if op != '+' && op != '-' && op != '*' && op != '/' {
			continue
		}
		if prev := s[i-1]; prev == 'e' || prev == 'E' {
			continue // an exponent's sign
		}
		a, err1 := strconv.ParseFloat(strings.TrimSpace(s[:i]), 64)
		b, err2 := strconv.ParseFloat(strings.TrimSpace(s[i+1:]), 64)
		if err1 != nil || err2 != nil {
			continue
		}
		switch op {
		case '+':
			return a + b, true
		case '-':
			return a - b, true
		case '*':
			return a * b, true
		default:
			if b == 0 {
				return 0, false
			}
			return a / b, true
		}
	}
	return 0, false
}
