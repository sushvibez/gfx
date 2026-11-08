package ui

import "github.com/sushvibez/gfx/core"

// Dialogs (QML's Dialog and DialogButtonBox): a card with a title band, the
// content, and a strip of standard buttons along the bottom. A button of the
// accept role (OK, Save, Yes…) closes the dialog and runs OnAccepted, one of
// the reject role (Cancel, No, Close…) runs OnRejected. A click on the dim
// scrim or Escape closes it as a reject would, without OnRejected — Qt's
// closePolicy.

// StandardButton is one of QDialogButtonBox's standard buttons; a dialog's
// Buttons is an OR of them. The values are Qt's, so a raw QML int matches.
type StandardButton uint32

const (
	ButtonOk              StandardButton = 0x00000400
	ButtonSave            StandardButton = 0x00000800
	ButtonSaveAll         StandardButton = 0x00001000
	ButtonOpen            StandardButton = 0x00002000
	ButtonYes             StandardButton = 0x00004000
	ButtonYesToAll        StandardButton = 0x00008000
	ButtonNo              StandardButton = 0x00010000
	ButtonNoToAll         StandardButton = 0x00020000
	ButtonAbort           StandardButton = 0x00040000
	ButtonRetry           StandardButton = 0x00080000
	ButtonIgnore          StandardButton = 0x00100000
	ButtonClose           StandardButton = 0x00200000
	ButtonCancel          StandardButton = 0x00400000
	ButtonDiscard         StandardButton = 0x00800000
	ButtonHelp            StandardButton = 0x01000000
	ButtonApply           StandardButton = 0x02000000
	ButtonReset           StandardButton = 0x04000000
	ButtonRestoreDefaults StandardButton = 0x08000000
)

// standardButtons is the order a button box lays them out, with each one's
// label and role.
var standardButtons = []struct {
	b      StandardButton
	label  string
	accept bool
}{
	{ButtonOk, "OK", true},
	{ButtonSave, "Save", true},
	{ButtonSaveAll, "Save All", true},
	{ButtonOpen, "Open", true},
	{ButtonYes, "Yes", true},
	{ButtonYesToAll, "Yes to All", true},
	{ButtonApply, "Apply", true},
	{ButtonNo, "No", false},
	{ButtonNoToAll, "No to All", false},
	{ButtonDiscard, "Discard", false},
	{ButtonCancel, "Cancel", false},
	{ButtonClose, "Close", false},
	{ButtonAbort, "Abort", false},
	{ButtonRetry, "Retry", false},
	{ButtonIgnore, "Ignore", false},
	{ButtonReset, "Reset", false},
	{ButtonRestoreDefaults, "Restore Defaults", false},
	{ButtonHelp, "Help", false},
}

// Label is the button's text ("OK", "Save All").
func (b StandardButton) Label() string {
	for _, s := range standardButtons {
		if s.b == b {
			return s.label
		}
	}
	return ""
}

// Accepts reports whether the button plays the accept role.
func (b StandardButton) Accepts() bool {
	for _, s := range standardButtons {
		if s.b == b {
			return s.accept
		}
	}
	return false
}

// A DialogButtonBox's metrics, measured from Qt's Basic style.
const (
	dialogFooterH   = 64
	dialogFooterPad = 12
	dialogButtonW   = 188
	dialogButtonH   = 40
)

// DialogButtonBox is a strip of standard buttons (QML's DialogButtonBox in
// the Basic style): trailing, sharing a light band; onButton runs with the
// button clicked.
func DialogButtonBox(buttons StandardButton, onButton func(StandardButton)) *DivEl {
	box := Div().Row().Gap(1).H(dialogFooterH).ItemsCenter().Justify(AlignEnd).
		P(dialogFooterPad).Bg(RGB(0xefefef))
	for _, s := range standardButtons {
		if buttons&s.b == 0 {
			continue
		}
		b := s.b
		btn := Div().Row().Size(dialogButtonW, dialogButtonH).ItemsCenter().JustifyCenter().
			HoverBg(RGB(0xe0e0e0)).PressBg(RGB(0xd0d0d0)).Focusable().
			Children(Text(s.label, RGB(0x111111)).SizePx(14))
		if onButton != nil {
			btn.OnClick(func() { onButton(b) })
		}
		box.Children(btn)
	}
	return box
}

// DialogOpts configures a dialog.
type DialogOpts struct {
	Title   string
	Buttons StandardButton
	// OnAccepted and OnRejected run after a button of that role closes the
	// dialog; OnClosed after either, and after a dismiss.
	OnAccepted, OnRejected, OnClosed func()
	// Dark draws the card for a dark page.
	Dark bool
	// NoDim: no scrim behind the dialog (QML's modal: false); it still
	// closes on an outside click.
	NoDim bool
}

// DialogFrame is the dialog's card: the title band, content and button box.
// close closes the dialog (set its open state false); a button calls it,
// then runs the role's handler and OnClosed.
func DialogFrame(content Element, close func(), o DialogOpts) *DivEl {
	frame := Div().Col().MinW(320)
	if o.Title != "" {
		c := RGB(0x111111)
		if o.Dark {
			c = RGB(0xe8e8f0)
		}
		// Qt's header: the title leads, centred in a 48 px band.
		frame.Children(Div().Row().H(48).ItemsCenter().Padding(0, 20, 0, 20).
			Children(Text(o.Title, c).SizePx(18).Bold()))
	}
	if content != nil {
		frame.Children(content)
	}
	if o.Buttons != 0 {
		frame.Children(DialogButtonBox(o.Buttons, func(b StandardButton) {
			if close != nil {
				close()
			}
			if b.Accepts() {
				if o.OnAccepted != nil {
					o.OnAccepted()
				}
			} else if o.OnRejected != nil {
				o.OnRejected()
			}
			if o.OnClosed != nil {
				o.OnClosed()
			}
		}))
	}
	if o.Dark {
		frame.DarkPopupSkin()
	} else {
		frame.PopupSkin()
	}
	return frame
}

// Dialog floats DialogFrame centred over the window while open is set,
// dimming the page behind it; a click outside or Escape closes it. Closed,
// it takes no space.
func Dialog(open *core.Property[bool], content Element, o DialogOpts) Element {
	if !open.Get() {
		return Div()
	}
	close := func() { open.Set(false) }
	dismiss := func() {
		close()
		if o.OnClosed != nil {
			o.OnClosed()
		}
	}
	dim := RGBA{A: 0.45}
	if o.NoDim {
		dim = RGBA{}
	}
	return Modal(DialogFrame(content, close, o), dim, dismiss)
}
