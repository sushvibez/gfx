//go:build linux || windows || js

// A hand-written Go port of the corpus' buttons page
// (~/workspace/qml-examples/examples/buttons/Main.qml), built straight
// against the ui builder instead of through .gml — the third render in
// scripts/qml-trio.sh.
//
//	go run ./qmlgo/buttons
package main

import (
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/examples/qmlgo/corpus"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

type state struct {
	icons     *corpus.Icons
	body      ui.ScrollState
	glowLayer *ui.LayerState // the specialized card's glow button

	// ButtonStatesCard
	status    core.Property[string]
	statusAt  time.Time
	pressed   core.Property[bool]
	hovered   core.Property[bool]
	toggled   core.Property[bool]
	notifyChk core.Property[bool]

	// ToolButtonMenuCard: which menu is open, and where it hangs.
	openMenu core.Property[string]
	menuAt   map[string]ui.Element // this frame's tool buttons, by id
	menuBox  map[string]ui.Rect    // where they were laid out LAST frame
}

func (s *state) standardCard() ui.Element {
	btn := func(text string, f func(*corpus.Button)) ui.Element {
		b := corpus.Button{Text: text, W: corpus.R(130), H: corpus.R(40)}
		if f != nil {
			f(&b)
		}
		return b.El()
	}
	return corpus.CardBox("Standard Buttons",
		ui.Div().Row().Gap(corpus.R(15)).CrossFill().Children(
			btn("Default", nil),
			btn("Highlighted", func(b *corpus.Button) { b.Highlighted = true }),
			btn("Flat Button", func(b *corpus.Button) { b.Flat = true }),
			btn("Disabled", func(b *corpus.Button) { b.Disabled = true }),
		),
		corpus.Note("flat: no background · highlighted: brighter + bold · disabled: grayed out"),
	)
}

func (s *state) iconCard() ui.Element {
	return corpus.CardBox("Icon Buttons (ToolButton)",
		ui.Div().Row().Gap(corpus.R(15)).CrossFill().Children(
			corpus.ToolButton{Text: "Settings", Icon: s.icons.Get("settings"),
				W: corpus.R(120), H: corpus.R(40)}.El(),
			corpus.ToolButton{Text: "Info", Icon: s.icons.Get("status"),
				W: corpus.R(120), H: corpus.R(40)}.El(),
			corpus.ToolButton{Icon: s.icons.Get("onoff"),
				W: corpus.R(40), H: corpus.R(40)}.El(),
		),
		corpus.Note("ToolButtons are typically used in toolbars and for icon-only actions"),
	)
}

func (s *state) statesCard() ui.Element {
	press := "Press Me"
	if s.pressed.Get() {
		press = "Pressed!"
	}
	hover := "Hover Me"
	if s.hovered.Get() {
		hover = "Hovering"
	}
	toggle := "Checkable"
	if s.toggled.Get() {
		toggle = "Checked"
	}
	return corpus.CardBox("Button States & Interactions",
		ui.Div().Row().Gap(corpus.R(15)).CrossFill().Children(
			corpus.Button{Text: press, W: corpus.R(150), H: corpus.R(40),
				OnPress: func(down bool) { s.pressed.Set(down) },
				OnClick: func() { s.status.Set("Button clicked!"); s.statusAt = time.Now() },
			}.El(),
			corpus.Button{Text: hover, W: corpus.R(150), H: corpus.R(40),
				OnHover: func(in bool) { s.hovered.Set(in) },
			}.El(),
			corpus.Button{Text: toggle, W: corpus.R(150), H: corpus.R(40),
				Checkable: true, Checked: &s.toggled,
			}.El(),
		),
		ui.Text(s.status.Get(), corpus.Main).SizePx(14),
		corpus.Note("• down: Button is being pressed\n• hovered: Mouse is over the button\n"+
			"• checkable: Button can be toggled on/off"),
	)
}

func (s *state) customCard() ui.Element {
	row := func(flat bool) ui.Element {
		mk := func(text string, fill, textC uint32) ui.Element {
			b := corpus.Button{Text: text, W: corpus.R(120), H: corpus.R(40),
				Color: ui.RGB(fill), Flat: flat}
			if !flat {
				b.TextColor = ui.RGB(textC)
			}
			return b.El()
		}
		suffix := ""
		if flat {
			suffix = " Flat"
		}
		return ui.Div().Row().Gap(corpus.R(15)).CrossFill().Children(
			mk("Success"+suffix, 0x00D1A8, 0xFFFFFF),
			mk("Warning"+suffix, 0xFFE361, 0x333333),
			mk("Danger"+suffix, 0xFF5900, 0xFFFFFF),
		)
	}
	return corpus.CardBox("Custom Styled Buttons", row(false), row(true),
		corpus.Note("palette.button changes color for both solid and flat variants"))
}

func (s *state) specializedCard() ui.Element {
	// The four specialized buttons are the corpus' own components
	// (imports/controls): a glow, a gradient, a pulse and a neumorphic
	// face. Restated here with the ui builder's own gradient and border.
	glow := corpus.Glow(s.glowLayer, "Glow Effect", ui.RGB(0x00D1A8),
		corpus.R(150), corpus.R(40), 0.6, 20, nil)
	grad := corpus.Gradient("Gradient", ui.RGB(0xFF5900), ui.RGB(0xFFE361),
		corpus.R(150), corpus.R(40), nil)
	pulse := corpus.Pulse("Pulse Animation", corpus.Main, corpus.R(150), corpus.R(40), nil)
	neu := corpus.Neumorphic("Neumorphic", corpus.Bg, corpus.R(150), corpus.R(40))
	// A Flow, not a Row: the markup wraps these onto a second line when
	// the card is too narrow for four of them (QML's Flow positioner).
	return corpus.CardBox("Specialized Button Components",
		ui.Div().Row().Wrap().Gap(corpus.R(15)).CrossFill().Children(glow, grad, pulse, neu),
		corpus.Note("These specialized components are reusable across all examples"),
	)
}

func (s *state) menuCard() ui.Element {
	tb := func(id, text, icon string, w float32) ui.Element {
		e := corpus.ToolButton{Text: text, Icon: s.icons.Get(icon), W: corpus.R(w), H: corpus.R(40),
			OnClick: func() {
				if s.openMenu.Get() == id {
					s.openMenu.Set("")
				} else {
					s.openMenu.Set(id)
				}
			}}.El()
		s.remember(id, e)
		return e
	}
	return corpus.CardBox("ToolButton with Menu",
		ui.Div().Row().Gap(corpus.R(20)).CrossFill().Children(
			tb("file", "File", "inbox", 100),
			tb("settings", "Settings", "settings", 130),
			tb("more", "", "expand", 40),
		),
		corpus.Note("Click each ToolButton to open its dropdown Menu"),
	)
}

// remember keeps each tool button's element so its menu can hang under it
// — the Go equivalent of `y: fileToolBtn.height` on a QML Menu, which
// reads its parent's geometry. A view is rebuilt every frame and a fresh
// element has no rect until it is laid out, so the box read at build time
// is LAST frame's element's (snapshotBoxes, at the top of view): the menu
// opens a frame after the click, which is when Qt's shows too.
func (s *state) remember(id string, e ui.Element) {
	if s.menuAt == nil {
		s.menuAt = map[string]ui.Element{}
	}
	s.menuAt[id] = e
}

// snapshotBoxes reads where last frame's tool buttons were laid out,
// before this frame's rebuild replaces them.
func (s *state) snapshotBoxes() {
	if s.menuBox == nil {
		s.menuBox = map[string]ui.Rect{}
	}
	for id, el := range s.menuAt {
		if r := ui.Bounds(el); r.W > 0 {
			s.menuBox[id] = r
		}
	}
}

func (s *state) menuOverlay() ui.Element {
	id := s.openMenu.Get()
	if id == "" {
		return nil
	}
	at, ok := s.menuBox[id]
	if !ok {
		return nil // not laid out yet: next frame
	}
	dismiss := func() { s.openMenu.Set("") }
	var entries []corpus.MenuEntry
	switch id {
	case "file":
		entries = []corpus.MenuEntry{{Text: "New"}, {Text: "Open"}, {Text: "Save"}, {}, {Text: "Close"}}
	case "settings":
		entries = []corpus.MenuEntry{{Text: "Preferences"}, {Text: "Account"},
			{Text: "Notifications", Checkable: true, Checked: &s.notifyChk}}
	case "more":
		entries = []corpus.MenuEntry{{Text: "Help"}, {Text: "About"}}
	}
	return corpus.Menu(at.X, at.Y+at.H, entries, dismiss)
}

func (s *state) view() ui.Element {
	// The status label clears itself two seconds after a click (the card's
	// Timer). Only a click's text expires — writing the resting text back
	// every frame bumped the property revision twice a frame, which made
	// the glow layer re-render itself sixty times a second.
	const resting = "Interact with the buttons above"
	if s.status.Get() != resting && time.Since(s.statusAt) > 2*time.Second {
		s.status.Set(resting)
	}
	s.snapshotBoxes()
	page := corpus.Page(&s.body, "Button Examples", func(availW float32) []ui.Element {
		return []ui.Element{
			ui.Grid(2, 3).Gap(corpus.R(20)).W(availW).Children(
				s.cell(s.standardCard(), 180), s.cell(s.iconCard(), 180),
				s.cell(s.statesCard(), 240), s.cell(s.customCard(), 240),
				s.cell(s.specializedCard(), 200), s.cell(s.menuCard(), 200),
			),
		}
	})
	if m := s.menuOverlay(); m != nil {
		return ui.Div().Children(page, m)
	}
	return page
}

// cell is Layout.fillWidth + Layout.preferredHeight on a grid child.
//
// NoImplicitSize is the QML default nobody writes down: Layout.minimumWidth
// on a plain Item is 0, so a card SHRINKS to its share of the grid and its
// contents run off the edge (Qt's own render cuts "Flat Butto|n" at the
// card's right edge). Without it the track sizes to the buttons inside and
// the second column leaves the window.
func (s *state) cell(e ui.Element, h float32) ui.Element {
	return ui.Div().H(corpus.R(h)).CrossFill().Clip().NoImplicitSize().Children(e)
}

func main() {
	s := &state{glowLayer: ui.NewLayerState()}
	s.status.Set("Interact with the buttons above")
	corpus.Run("buttons", func(a *shell.App) (func() ui.Element, error) {
		s.icons = corpus.NewIcons(a)
		return s.view, nil
	})
}
