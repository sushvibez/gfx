package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

func TestSpinBoxWrapFormatAndModified(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Item {
    id: root
    width: 400; height: 300
    property int mods: 0
    SpinBox { id: sb; x: 0; y: 0; from: 1; to: 3; value: 3; wrap: true
        textFromValue: function(value, locale) { return "#" + value }
        onValueModified: root.mods += 1 }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	frame := func() { u.Layout(400, 300); u.Layout(400, 300) }
	frame()
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	if got := get("sb.displayText"); got != "#3" {
		t.Errorf("displayText = %q, want #3 (textFromValue)", got)
	}
	// The + button: the last 32 px box of the row, at 96..128.
	u.Mouse([]app.MouseEvent{{Kind: app.MouseMove, X: 112, Y: 16}, {Kind: app.MouseDown, X: 112, Y: 16, Button: 1},
		{Kind: app.MouseUp, X: 112, Y: 16, Button: 1}})
	frame()
	if got := get("sb.value") + "," + get("root.mods"); got != "1,1" {
		t.Errorf("+ from 3 with wrap: value,mods = %s, want 1,1", got)
	}
}
