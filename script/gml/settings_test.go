package gml

import (
	"testing"

	"github.com/sushvibez/gfx/core/settings"
	"github.com/sushvibez/gfx/ui"
)

const settingsSrc = `
import QtQuick 2.15
import QtCore
Item {
    id: root
    width: 300; height: 200
    property real doubled: prefs.zoom * 2
    Settings {
        id: prefs
        category: "view"
        property real zoom: 1.5
        property string theme: "light"
        property bool grid: true
        property alias boxWidth: box.width
    }
    Rectangle { id: box; width: 40; height: 20 }
}`

func loadSettingsDoc(t *testing.T, store settings.Store) (*Document, *Env, func()) {
	t.Helper()
	env := NewEnv()
	d, err := LoadString(settingsSrc, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	d.SetSettingsStore(store)
	u := ui.New(nil, nil, d.View())
	frame := func() { u.Layout(300, 200) }
	frame()
	return d, env, frame
}

func TestSettingsPersist(t *testing.T) {
	store := &settings.Memory{}
	store.Save("", map[string]any{"view/theme": "dark", "view/boxWidth": float64(90)})

	_, env, frame := loadSettingsDoc(t, store)
	frame()
	get := func(expr string) string {
		v, err := evalString(expr, env)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		return str(v)
	}
	if got := get("prefs.theme"); got != "dark" {
		t.Errorf("theme = %s, want the saved dark", got)
	}
	if got := get("prefs.zoom"); got != "1.5" {
		t.Errorf("zoom = %s, want the default 1.5 (nothing saved)", got)
	}
	if got := get("root.doubled"); got != "3" {
		t.Errorf("root.doubled = %s, want 3 (bound to prefs.zoom)", got)
	}
	if got := get("box.width"); got != "90" {
		t.Errorf("box.width = %s, want the saved 90 through the alias", got)
	}

	// Changes are written on the next frame, under their category.
	if _, err := evalString("prefs.zoom = 2.25", env); err != nil {
		t.Fatal(err)
	}
	if _, err := evalString("prefs.grid = false", env); err != nil {
		t.Fatal(err)
	}
	if _, err := evalString(`prefs.setValue("recent", ["a.gml", "b.gml"])`, env); err != nil {
		t.Fatal(err)
	}
	if v := store.Load("")["view/zoom"]; v != nil {
		t.Errorf("saved before the frame ended: %v", v)
	}
	frame()
	if got := get("box.width"); got != "90" {
		t.Errorf("box.width = %s after more frames, want the restored 90 to hold", got)
	}
	saved := store.Load("")
	if saved["view/zoom"] != 2.25 || saved["view/grid"] != false || saved["view/theme"] != "dark" {
		t.Errorf("saved = %v", saved)
	}
	if r, ok := saved["view/recent"].([]any); !ok || len(r) != 2 {
		t.Errorf("view/recent = %v, want the two files", saved["view/recent"])
	}
	if got := get(`prefs.value("recent", []).length`); got != "2" {
		t.Errorf("value(recent).length = %s, want 2", got)
	}
	if got := get(`prefs.value("missing", 7)`); got != "7" {
		t.Errorf("value(missing, 7) = %s, want the default", got)
	}

	if _, err := evalString("box.width = 120", env); err != nil {
		t.Fatal(err)
	}
	frame()
	if v := store.Load("")["view/boxWidth"]; v != 120.0 {
		t.Errorf("view/boxWidth = %v after box.width = 120, want 120 (the alias target's change)", v)
	}

	// A second run reads them back.
	_, env2, _ := loadSettingsDoc(t, store)
	if v, _ := evalString("prefs.zoom", env2); str(v) != "2.25" {
		t.Errorf("second run zoom = %v, want 2.25", v)
	}
	if v, _ := evalString("prefs.grid", env2); str(v) != "false" {
		t.Errorf("second run grid = %v, want false", v)
	}
}

// evalString evaluates one expression (an assignment included) in env.
func evalString(src string, env *Env) (any, error) {
	e, err := parseExprSrc(src)
	if err != nil {
		return nil, err
	}
	return e.eval(env)
}
