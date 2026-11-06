package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestColourComponentsNeedAColour: QML draws the line where JavaScript
// does — a `property color` (or a Qt.rgba/Qt.color result) is a colour
// object whose .r/.g/.b are readable, while a `property string` holding
// "#00D1A9" is just text and `.r` on it is UNDEFINED. The corpus depends on
// it: its Style.mainColor is a string, so the shapes page's
// `Qt.rgba(Style.mainColor.r, ..., 0.1)` fill is BLACK at 10% in Qt, where
// we painted it teal.
func TestColourComponentsNeedAColour(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
Item {
    property color realColor: "#00D1A9"
    property string strColor: "#00D1A9"
    property var fromColour: realColor.g
    property var fromString: strColor.g
    property var converted: Qt.color(strColor).g
    property var mixed: Qt.rgba(strColor.r, strColor.g, strColor.b, 1)
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	get := func(name string) any {
		r, ok := env.vars[name].(Ref)
		if !ok {
			t.Fatalf("%s is not a property", name)
		}
		return r.get()
	}
	if g, _ := toFloat64(get("fromColour")); g < 0.8 || g > 0.83 {
		t.Errorf("a colour's .g = %v, want ~0.82", get("fromColour"))
	}
	if v := get("fromString"); v != nil {
		t.Errorf("a string's .g = %#v, want undefined", v)
	}
	if g, _ := toFloat64(get("converted")); g < 0.8 || g > 0.83 {
		t.Errorf("Qt.color(s).g = %v, want ~0.82 (an explicit conversion IS a colour)", get("converted"))
	}
	// Qt.rgba of three undefineds is black — what the corpus' arc fill is.
	if s := str(get("mixed")); s != "#000000" && s != "#ff000000" {
		t.Errorf("Qt.rgba(string.r, ...) = %q, want black", s)
	}
}
