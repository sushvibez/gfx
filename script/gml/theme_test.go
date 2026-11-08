package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// Material.theme / Material.accent on an item theme the controls inside
// it, a nested item can switch back, and a bound theme follows its
// binding.
func TestMaterialTheme(t *testing.T) {
	defer ui.SetTheme(ui.LightTheme())
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Controls.Material 2.15
Item {
    id: root
    width: 400; height: 300
    property bool dark: true
    Material.theme: root.dark ? Material.Dark : Material.Light
    Material.accent: Material.Teal
    Column {
        TabBar { id: outer; TabButton { text: "A" } }
        Pane {
            Material.theme: Material.Light
            TabBar { id: inner; TabButton { text: "B" } }
        }
        Button { id: go; text: "Go" }
        Button { id: red; text: "Red"; palette.highlight: "#ff0000" }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(400, 300); u.Layout(400, 300) }
	fill := func(id string) ui.RGBA {
		c, _ := ui.FillColor(d.doc.elems.get(d.doc.intern(id)))
		return c
	}
	frame()
	if got, want := fill("outer"), ui.DarkTheme().Band; got != want {
		t.Errorf("outer tab bar %v, want the dark band %v", got, want)
	}
	if got, want := fill("inner"), ui.LightTheme().Band; got != want {
		t.Errorf("tab bar in the light Pane %v, want the light band %v", got, want)
	}
	if got := fill("go"); got != ui.RGB(materialColors[8]) {
		t.Errorf("button %v, want Material.Teal %v", got, ui.RGB(materialColors[8]))
	}
	if got := fill("red"); got != ui.RGB(0xff0000) {
		t.Errorf("a button with its own palette.highlight is %v, want red", got)
	}
	if _, err := evalString("root.dark = false", env); err != nil {
		t.Fatal(err)
	}
	frame()
	if got, want := fill("outer"), ui.LightTheme().Band; got != want {
		t.Errorf("after the switch the outer tab bar is %v, want the light band %v", got, want)
	}
	if v, err := evalString("Material.color(Material.Teal)", env); err != nil || str(v) != "#009688" {
		t.Errorf("Material.color(Material.Teal) = %v (%v)", v, err)
	}
}

// On a dark page the text and fields go dark, but Qt's Basic tab bar and
// menu bar keep their light bands, as Qt draws them on any page.
func TestDarkPageKeepsBasicBands(t *testing.T) {
	defer ui.SetTheme(ui.LightTheme())
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Rectangle {
    width: 400; height: 300; color: "#1e1f24"
    Column {
        TabBar { id: tabs; TabButton { text: "A" } }
        CheckBox { text: "Wrap" }
    }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	for i := 0; i < 4; i++ {
		u.Layout(400, 300)
	}
	if c, _ := ui.FillColor(d.doc.elems.get(d.doc.intern("tabs"))); c != ui.LightTheme().Band {
		t.Errorf("tab bar on a dark page is %v, want the light band %v", c, ui.LightTheme().Band)
	}
	if !d.doc.groundDark {
		t.Error("the page did not read as dark")
	}
}
