package ui

import (
	"strings"
	"unicode"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
)

// prevWord returns the start of the word before position i: whitespace is
// skipped first, then the word's runes (the usual editor Ctrl+Left target).
func prevWord(runes []rune, i int) int {
	for i > 0 && unicode.IsSpace(runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(runes[i-1]) {
		i--
	}
	return i
}

// nextWord returns the start of the word after position i: the rest of the
// current word is skipped, then the whitespace run (Windows Ctrl+Right).
func nextWord(runes []rune, i int) int {
	n := len(runes)
	for i < n && !unicode.IsSpace(runes[i]) {
		i++
	}
	for i < n && unicode.IsSpace(runes[i]) {
		i++
	}
	return i
}

// clipboard is the framework's in-process clipboard (Ctrl+C/X/V), used
// until SetClipboard wires the OS clipboard — and always in tests and
// headless runs, which must not touch the user's real clipboard.
var clipboard string

var (
	clipboardGet = func() string { return clipboard }
	clipboardSet = func(s string) { clipboard = s }
)

// SetClipboard routes Ctrl+C/X/V through an external clipboard (the shell
// installs the OS clipboard here for windowed apps). Passing nils restores
// the in-process buffer.
func SetClipboard(get func() string, set func(string)) {
	if get == nil {
		get = func() string { return clipboard }
	}
	if set == nil {
		set = func(s string) { clipboard = s }
	}
	clipboardGet, clipboardSet = get, set
}

// InputState is the retained state of a text field (the GPUI-entity side
// of the hybrid): own it in your view struct, render it each frame with
// ui.Input. Text is a reactive property, so bindings and Changed signals
// work like any other. It supports a selection (caret + anchor) edited by
// keyboard (shift to extend, Ctrl+A/C/X/V) and mouse drag.
type InputState struct {
	Text    core.Property[string]
	caret   int          // active end, rune index
	anchor  int          // selection anchor; anchor == caret means no selection
	submit  func(string) // Enter handler (QML TextField.onAccepted)
	focused bool         // holds keyboard focus (QML activeFocus)
	// accept vets a candidate value BEFORE it is committed (QML's
	// `validator:`, which rejects a keystroke rather than flagging it
	// afterwards). nil accepts everything.
	accept func(string) bool
	maxLen int // rune cap (QML maximumLength); 0 = unlimited
	// multiline is a text area's state (textarea.go); lines is the
	// layout the area last drew, for Up/Down/Home/End.
	multiline bool
	lines     []TextLine
}

// Accept installs a validator: fn sees each candidate value before it is
// committed and a false rejects the edit, leaving text and caret where they
// were. That is QML's `validator:` contract — a field with an IntValidator
// never contains "12x", because the "x" is refused as it is typed.
func (s *InputState) Accept(fn func(string) bool) { s.accept = fn }

// MaxLen caps the field's length in runes (QML maximumLength); 0 removes the
// cap. Text already in the field is left alone, as Qt does.
func (s *InputState) MaxLen(n int) { s.maxLen = n }

// commit applies a candidate value if the validator and the length cap
// allow it, and reports whether it went in. caretTo is where the caret
// belongs afterwards.
func (s *InputState) commit(text string, caretTo int) bool {
	if s.maxLen > 0 && len([]rune(text)) > s.maxLen {
		return false
	}
	if s.accept != nil && !s.accept(text) {
		return false
	}
	s.Text.Set(text)
	s.caret = caretTo
	s.anchor = caretTo
	return true
}

// HasFocus reports whether this field currently holds keyboard focus —
// QML's TextField.activeFocus. Maintained by the UI as focus moves.
func (s *InputState) HasFocus() bool { return s.focused }

// SetFocused marks the state focused or not without a Context — for
// hosts and tests that drive an input directly (QML forceActiveFocus on
// a detached item). Inside a UI, focus moves through the Context.
func (s *InputState) SetFocused(on bool) { s.focused = on }

// OnSubmit registers fn to run with the field's text when Enter is
// pressed (QML TextField.onAccepted).
func (s *InputState) OnSubmit(fn func(string)) { s.submit = fn }

// SetText replaces the text programmatically, placing the caret at the end
// and clearing any selection (QML TextField text setter). Use this instead
// of Text.Set so the caret can't be left past the end of the new text.
func (s *InputState) SetText(t string) {
	s.Text.Set(t)
	s.caret = len([]rune(t))
	s.anchor = s.caret
}

// NewInput creates input state with initial text, caret at the end.
func NewInput(initial string) *InputState {
	s := &InputState{}
	s.Text.Set(initial)
	s.caret = len([]rune(initial))
	s.anchor = s.caret
	return s
}

// selRange returns the ordered selection bounds [lo, hi).
func (s *InputState) selRange() (lo, hi int) {
	if s.caret <= s.anchor {
		return s.caret, s.anchor
	}
	return s.anchor, s.caret
}

func (s *InputState) hasSelection() bool { return s.caret != s.anchor }

// deleteSelection removes the selected runes, collapsing the caret; it
// reports whether anything was removed.
func (s *InputState) deleteSelection(runes []rune) ([]rune, bool) {
	if !s.hasSelection() {
		return runes, false
	}
	lo, hi := s.selRange()
	runes = append(runes[:lo], runes[hi:]...)
	s.caret, s.anchor = lo, lo
	return runes, true
}

// Key applies one editing/selection key to the state.
func (s *InputState) Key(ev app.KeyEvent) {
	if !ev.Down {
		return
	}
	if s.multiKey(ev) {
		return
	}
	if ev.Key == app.KeyEnter {
		if s.submit != nil {
			s.submit(s.Text.Get())
		}
		return
	}
	runes := []rune(s.Text.Get())
	n := len(runes)
	s.caret = min(max(s.caret, 0), n)
	s.anchor = min(max(s.anchor, 0), n)

	// Ctrl shortcuts.
	if ev.Ctrl {
		switch ev.Rune {
		case 'a':
			s.anchor, s.caret = 0, n
			return
		case 'c':
			if s.hasSelection() {
				lo, hi := s.selRange()
				clipboardSet(string(runes[lo:hi]))
			}
			return
		case 'x':
			if s.hasSelection() {
				lo, hi := s.selRange()
				del, _ := s.deleteSelection(runes)
				if s.commit(string(del), lo) {
					clipboardSet(string(runes[lo:hi]))
				}
			}
			return
		case 'v':
			runes, _ = s.deleteSelection(runes)
			// Single-line field: fold pasted line breaks into spaces.
			ins := []rune(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").
				Replace(clipboardGet()))
			pasted := append(runes[:s.caret:s.caret], append(ins, runes[s.caret:]...)...)
			s.commit(string(pasted), s.caret+len(ins))
			return
		}
	}

	switch {
	case ev.Key == app.KeyBackspace:
		if del, ok := s.deleteSelection(runes); ok {
			runes = del
		} else if s.caret > 0 {
			to := s.caret - 1
			if ev.Ctrl { // Ctrl+Backspace deletes the previous word
				to = prevWord(runes, s.caret)
			}
			runes = append(runes[:to], runes[s.caret:]...)
			s.caret = to
			s.anchor = s.caret
		}
	case ev.Key == app.KeyDelete:
		if del, ok := s.deleteSelection(runes); ok {
			runes = del
		} else if s.caret < n {
			to := s.caret + 1
			if ev.Ctrl { // Ctrl+Delete deletes to the next word
				to = nextWord(runes, s.caret)
			}
			runes = append(runes[:s.caret], runes[to:]...)
		}
	case ev.Key == app.KeyLeft:
		switch {
		case ev.Ctrl: // word-wise: Ctrl moves, Ctrl+Shift selects
			s.moveCaret(prevWord(runes, s.caret), ev.Shift)
		case s.hasSelection() && !ev.Shift:
			lo, _ := s.selRange()
			s.caret, s.anchor = lo, lo // collapse to the left edge
		default:
			s.moveCaret(max(s.caret-1, 0), ev.Shift)
		}
	case ev.Key == app.KeyRight:
		switch {
		case ev.Ctrl:
			s.moveCaret(nextWord(runes, s.caret), ev.Shift)
		case s.hasSelection() && !ev.Shift:
			_, hi := s.selRange()
			s.caret, s.anchor = hi, hi // collapse to the right edge
		default:
			s.moveCaret(min(s.caret+1, n), ev.Shift)
		}
	case ev.Key == app.KeyHome:
		s.moveCaret(0, ev.Shift)
	case ev.Key == app.KeyEnd:
		s.moveCaret(n, ev.Shift)
	// AltGr (intl layouts) arrives as Ctrl+Alt+rune: those chars must
	// insert; plain Ctrl chords must not.
	case ev.Key == app.KeyNone && ev.Rune >= ' ' && (!ev.Ctrl || ev.Alt):
		runes, _ = s.deleteSelection(runes)
		typed := append(runes[:s.caret:s.caret], append([]rune{ev.Rune}, runes[s.caret:]...)...)
		// A rejected keystroke leaves the field exactly as it was: the
		// validator refuses the character, it does not flag it after the
		// fact (QML's contract, and what a numeric field must do).
		s.commit(string(typed), s.caret+1)
		return
	default:
		return
	}
	// Deletions are always allowed through — a validator that refuses the
	// empty string must not trap the user with text they cannot clear,
	// which is Qt's behaviour too.
	s.Text.Set(string(runes))
}

// moveCaret sets the caret, extending the selection when shift is held,
// else collapsing it.
func (s *InputState) moveCaret(to int, extend bool) {
	s.caret = to
	if !extend {
		s.anchor = to
	}
}

// SelectAll selects the whole field.
func (s *InputState) SelectAll() {
	s.anchor = 0
	s.caret = len([]rune(s.Text.Get()))
}

// Input renders a single-line text field bound to state. Clicking it
// focuses it; keys routed through UI.Key edit it. Defaults to the dark
// skin; use the chained setters (Bg/Border/TextColor/Accent/Placeholder/
// Rounded) for a light field or other themes.
func Input(state *InputState, w float32) *InputEl {
	return &InputEl{
		state:   state,
		w:       w,
		bg:      RGB(0x101018),
		borderC: RGB(0x3b3b4f),
		accentC: RGB(inputAccent),
		textC:   RGB(0xe8e8f0),
		selC:    RGBA{R: 0.23, G: 0.36, B: 0.86, A: 0.5},
		radius:  6,
		borderW: 2,
	}
}

// InputEl is the per-frame element for an InputState.
type InputEl struct {
	state *InputState
	w     float32
	rect  Rect

	bg            RGBA
	borderC       RGBA // border when unfocused
	accentC       RGBA // border + caret accent when focused
	textC         RGBA
	selC          RGBA
	radius        float32
	borderW       float32
	placeholder   string
	placeholderC  RGBA
	hasPlaceholdr bool
	wantFocus     bool
	sizePx        float64 // text pixel size; 0 = the UI font's default
	center        bool    // draw the text centred, not left-aligned
	// echo replaces every character on screen (QML echoMode: Password uses
	// a bullet; NoEcho shows nothing). The field's VALUE is untouched — only
	// what is drawn, measured and clicked through changes.
	echo    rune
	hasEcho bool
	noEcho  bool
	// readOnly takes the field out of the input path (QML's readOnly, and
	// `enabled: false`): it still DRAWS its text, but nothing focuses it,
	// types into it or drags a selection through it.
	readOnly bool
	// area is a TextArea: areaH tall, wrapped, areaTop the first line
	// shown (textarea.go).
	area    bool
	areaH   float32
	areaTop int
}

// ReadOnly stops the field taking input while it keeps drawing its content —
// QML's `readOnly: true`, and what `enabled: false` amounts to for a field.
func (e *InputEl) ReadOnly(v bool) *InputEl { e.readOnly = v; return e }

// Echo draws every character as mask instead of itself — QML's
// `echoMode: TextInput.Password`. Pass 0 for NoEcho, which draws nothing at
// all (and gives a click nowhere to land but the ends).
func (e *InputEl) Echo(mask rune) *InputEl {
	e.echo, e.hasEcho, e.noEcho = mask, mask != 0, mask == 0
	return e
}

// shown is the string the field DRAWS: the value, or its masked echo. Caret
// and selection maths run on this, so a click in a password field lands
// where the bullets are.
func (e *InputEl) shown() string {
	text := e.state.Text.Get()
	switch {
	case e.noEcho:
		return ""
	case e.hasEcho:
		masked := make([]rune, 0, len(text))
		for range []rune(text) {
			masked = append(masked, e.echo)
		}
		return string(masked)
	}
	return text
}

// Focus requests initial focus (QML focus: true): the field grabs focus
// when it appears and nothing else is focused.
func (e *InputEl) Focus() *InputEl { e.wantFocus = true; return e }

func (e *InputEl) wantsInitialFocus() bool { return e.wantFocus }

// Bg sets the field background colour.
func (e *InputEl) Bg(c RGBA) *InputEl { e.bg = c; return e }

// Border sets the resting border width and colour.
func (e *InputEl) Border(w float32, c RGBA) *InputEl { e.borderW, e.borderC = w, c; return e }

// Accent sets the focused border + caret colour.
func (e *InputEl) Accent(c RGBA) *InputEl { e.accentC = c; return e }

// TextColor sets the typed-text and caret colour.
func (e *InputEl) TextColor(c RGBA) *InputEl { e.textC = c; return e }

// Rounded sets the corner radius.
func (e *InputEl) Rounded(r float32) *InputEl { e.radius = r; return e }

// Placeholder shows greyed hint text while the field is empty.
func (e *InputEl) Placeholder(s string, c RGBA) *InputEl {
	e.placeholder, e.placeholderC, e.hasPlaceholdr = s, c, true
	return e
}

// SizePx overrides the text pixel size (QML font.pixelSize, GPUI text_size);
// the field's height follows the sized font.
func (e *InputEl) SizePx(px float64) *InputEl { e.sizePx = px; return e }

// Center draws the text centred in the field rather than left-aligned
// (QML's horizontalAlignment: Text.AlignHCenter — a spin box's value sits
// between its two steppers).
func (e *InputEl) Center() *InputEl { e.center = true; return e }

// textX is where the text starts: the padding edge, or the offset that
// centres it.
func (e *InputEl) textX(ctx *Context, font *render.Font) float32 {
	if !e.center || font == nil {
		return e.rect.X + inputPadX
	}
	w, _ := ctx.R.MeasureText(font, e.shown())
	return e.rect.X + max((e.rect.W-w)/2, inputPadX)
}

// font resolves the field's render font: the UI font, resized when SizePx
// was set.
func (e *InputEl) font(ctx *Context) *render.Font {
	if ctx.Font == nil {
		return nil
	}
	if e.sizePx > 0 {
		return ctx.Font.AtSize(e.sizePx)
	}
	return ctx.Font
}

const (
	inputPadX   = 10
	inputPadY   = 6
	inputAccent = 0x3b5bdb
)

func (e *InputEl) measure(ctx *Context) (float32, float32) {
	if e.area {
		return e.w, e.areaH
	}
	h := float32(24)
	if f := e.font(ctx); f != nil {
		h = f.Height()
	}
	return e.w, h + 2*inputPadY
}

func (e *InputEl) layout(ctx *Context, r Rect) {
	e.rect = r
	ctx.focusables = append(ctx.focusables, e) // text fields are focusable
}

func (e *InputEl) isFocusable() bool       { return true }
func (e *InputEl) cursorShape() app.Cursor { return app.CursorText }

// dirty while focused: the caret blinks and the text is being edited.
func (e *InputEl) dirty(ctx *Context) bool {
	if ctx == nil {
		return e.state != nil && e.state.HasFocus()
	}
	return ctx.focus != nil && ctx.focus == e.state
}

func (e *InputEl) paint(ctx *Context) {
	if ctx.culled(e) {
		return
	}
	focused := ctx.focus == e.state
	border := e.borderC
	if focused {
		border = e.accentC
	}
	ctx.R.Quad(e.rect.X, e.rect.Y, e.rect.W, e.rect.H, e.bg, e.radius, e.borderW, border)
	font := e.font(ctx)
	if font == nil {
		return
	}
	if e.area {
		e.paintArea(ctx, font, focused)
		return
	}
	text := e.shown()
	runes := []rune(text)
	tx, ty := e.textX(ctx, font), e.rect.Y+inputPadY
	prefixW := func(i int) float32 {
		i = min(max(i, 0), len(runes))
		w, _ := ctx.R.MeasureText(font, string(runes[:i]))
		return w
	}
	// Placeholder hint while empty — while the VALUE is empty: a NoEcho
	// field draws nothing, and it is not therefore empty.
	if e.state.Text.Get() == "" && e.hasPlaceholdr {
		ctx.R.DrawText(font, tx, ty, e.placeholder, e.placeholderC)
	}
	// Selection highlight behind the text.
	if focused && e.state.hasSelection() {
		lo, hi := e.state.selRange()
		x0, x1 := tx+prefixW(lo), tx+prefixW(hi)
		ctx.R.FillRect(x0, ty, x1-x0, font.Height(), e.selC)
	}
	ctx.R.DrawText(font, tx, ty, text, e.textC)
	if focused {
		ctx.R.FillRect(tx+prefixW(e.state.caret), ty, 1.5, font.Height(), e.textC)
	}
}

// caretAtX maps a pixel x to the nearest rune boundary (for click/drag
// selection). Needs ctx for text measurement.
func (e *InputEl) caretAtX(ctx *Context, x float32) int {
	font := e.font(ctx)
	if font == nil {
		return 0
	}
	runes := []rune(e.shown())
	rel := x - e.textX(ctx, font)
	for i := 1; i <= len(runes); i++ {
		w, _ := ctx.R.MeasureText(font, string(runes[:i]))
		prev, _ := ctx.R.MeasureText(font, string(runes[:i-1]))
		if rel < (prev+w)/2 {
			return i - 1
		}
	}
	return len(runes)
}

func (e *InputEl) hit(x, y float32) Element {
	if e.readOnly {
		return nil // not focusable, not selectable: the press falls through
	}
	if e.rect.contains(x, y) {
		return e
	}
	return nil
}

// onClick returns a no-op: the click itself is consumed (and UI.Mouse
// handles focusing on press).
func (e *InputEl) onClick() func() { return func() {} }

func (e *InputEl) bounds() Rect { return e.rect }

func (e *InputEl) fixedWH() (float32, float32) { return e.w, 0 }
