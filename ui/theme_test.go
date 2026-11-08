package ui

import (
	"testing"

	"github.com/sushvibez/gfx/core"
)

func TestTheme(t *testing.T) {
	defer SetTheme(DefaultTheme())
	var on core.Property[bool]
	on.Set(true)
	// The default theme is the look the controls had before themes.
	box := func() RGBA {
		c, _ := FillColor(Children(Checkbox(&on, "x"))[0])
		return c
	}
	if c := box(); c != RGB(0x3b5bdb) {
		t.Errorf("checked box %v, want the original accent", c)
	}
	if c, _ := FillColor(Button("Go", ButtonOpts{})); c != Accent {
		t.Errorf("button %v, want the original accent", c)
	}
	// A theme switch reaches every control built after it, and is a
	// property change (a cached tree rebuilds).
	rev := core.Revision()
	dark := DarkTheme()
	dark.Accent = RGB(0x12b886)
	SetTheme(dark)
	if core.Revision() == rev {
		t.Error("SetTheme did not change anything a cache would see")
	}
	if c := box(); c != dark.Accent {
		t.Errorf("checked box %v under the new theme, want %v", c, dark.Accent)
	}
	if c, _ := FillColor(TabBar(0, []Tab{{Label: "A"}}, nil)); c != dark.Band {
		t.Errorf("tab bar %v, want the dark band", c)
	}
	// A control's own theme wins over the current one.
	light := LightTheme()
	if c, _ := FillColor(Button("Go", ButtonOpts{Theme: &light})); c != light.Accent {
		t.Errorf("button with its own theme %v, want %v", c, light.Accent)
	}
	if !DarkTheme().Dark || LightTheme().Dark || DefaultTheme().Dark {
		t.Error("Dark flags")
	}
}

func TestThemeScope(t *testing.T) {
	var sc ThemeScope
	dark := DarkTheme()
	sc.Push(dark)
	inner := TabBar(0, []Tab{{Label: "A"}}, nil)
	sc.Pop()
	outer := TabBar(0, []Tab{{Label: "A"}}, nil)
	if c, _ := FillColor(inner); c != dark.Band {
		t.Errorf("a tab bar built in the scope is %v, want the dark band", c)
	}
	if c, _ := FillColor(outer); c != CurrentTheme().Band {
		t.Errorf("a tab bar built after Pop is %v, want the program's band", c)
	}
	// A slider keeps the scope's theme when it paints, outside the scope.
	var v core.Property[float32]
	sc.Push(dark)
	s := Slider(&v, 100)
	sc.Pop()
	if s.tm.Track != dark.Track {
		t.Error("a slider built in a scope does not keep the scope's theme")
	}
}
