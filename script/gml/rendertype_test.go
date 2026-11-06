package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// Qt's Text.renderType, which we honour rather than accept and ignore:
// QtRendering is the distance field (crisp magnified, rotated, or at a
// size that slides), NativeRendering the hinted coverage atlas. The
// default is whatever the UI is set to, so a document that says nothing
// gets the app's choice.
func TestTextRenderType(t *testing.T) {
	env := NewEnv()
	// The enum has to resolve, because an unresolved identifier is not
	// an error here — the attribute is simply skipped, and a page would
	// silently keep the default.
	for name, want := range map[string]float64{"QtRendering": 0, "NativeRendering": 1} {
		v, err := evalSnippet("Text."+name, env)
		if err != nil {
			t.Fatalf("Text.%s: %v", name, err)
		}
		if f, err := toFloat64(v); err != nil || f != want {
			t.Errorf("Text.%s = %v, want %v", name, v, want)
		}
	}
	// And a document using it builds — both spellings, and the plain
	// number Qt also accepts.
	for _, src := range []string{
		`Text { text: "hi"; renderType: Text.QtRendering }`,
		`Text { text: "hi"; renderType: Text.NativeRendering }`,
		`Text { text: "hi"; renderType: 0 }`,
		`Text { text: "hi" }`,
	} {
		d, err := LoadString(src, NewEnv())
		if err != nil {
			t.Fatalf("LoadString(%s): %v", src, err)
		}
		u := ui.New(nil, nil, d.View())
		u.Layout(200, 60)
		if _, ok := d.View()().(*ui.TextEl); !ok {
			t.Errorf("%s did not build a Text", src)
		}
	}
}
