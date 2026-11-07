package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

func TestSystemPalette(t *testing.T) {
	src := `
import QtQuick 2.15
Rectangle {
    id: root
    width: 200; height: 100
    property bool on: true
    color: pal.window
    SystemPalette { id: pal }
    SystemPalette { id: grey; colorGroup: SystemPalette.Disabled }
    SystemPalette { id: live; colorGroup: root.on ? SystemPalette.Active : SystemPalette.Disabled }
    Text { id: label; text: "hi"; color: live.text }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(200, 100) }
	frame()
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	for expr, want := range map[string]string{
		"pal.window":      "#f0f0f0",
		"pal.highlight":   "#308cc6",
		"pal.button":      "#e0e0e0",
		"pal.mid":         "#b8b8b8",
		"grey.text":       "#bebebe",
		"grey.window":     "#f0f0f0",
		"grey.colorGroup": "1",
		"live.text":       "#1a1a1a",
		"root.color":      "#f0f0f0",
		"pal.highlight.r > 0.18 && pal.highlight.r < 0.2": "true",
	} {
		if got := get(expr); got != want {
			t.Errorf("%s = %s, want %s", expr, got, want)
		}
	}
	// colorGroup is a live binding.
	if _, err := evalString("root.on = false", env); err != nil {
		t.Fatal(err)
	}
	frame()
	if got := get("live.text"); got != "#bebebe" {
		t.Errorf("live.text = %s after disabling, want #bebebe", got)
	}
	if got := get("label.color"); got != "#bebebe" {
		t.Errorf("label.color = %s after disabling, want #bebebe", got)
	}
}
