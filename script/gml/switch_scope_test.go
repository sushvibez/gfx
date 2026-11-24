package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// A Switch's or CheckBox's handler sees the control's own `checked` bare,
// as in QML — `onToggled: save(checked)` — with the new value.
func TestToggleHandlerSeesChecked(t *testing.T) {
	for _, ctl := range []string{"Switch", "CheckBox"} {
		src := `Item { width: 200; height: 100
    ` + ctl + ` { x: 0; y: 0; checked: on; onToggled: report(checked ? "on" : "off") } }`
		env := NewEnv()
		env.Set("on", false)
		var got []string
		env.Func("report", func(s string) { got = append(got, s); env.Set("on", s == "on") })
		d, err := LoadString(src, env)
		if err != nil {
			t.Fatal(err)
		}
		u := ui.New(nil, nil, func() ui.Element { return d.View()() })
		frame := func() { u.Layout(200, 100); u.Layout(200, 100) }
		for i := 0; i < 2; i++ {
			frame()
			u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 10, Y: 10}, {Kind: app.MouseDown, X: 10, Y: 10, Button: 1}, {Kind: app.MouseUp, X: 10, Y: 10, Button: 1}})
			frame()
		}
		if len(got) != 2 || got[0] != "on" || got[1] != "off" {
			t.Errorf("%s: handler saw %v, want [on off]", ctl, got)
		}
	}
}
