package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// A padded ScrollView reads its own laid-out size to tell its children how
// wide `parent` is, and on the first frame there is no layout to read yet:
// that build panicked on a nil element (examples/qml/gpui/scrollable.gml
// could not be opened at all).
func TestPaddedScrollViewFirstFrame(t *testing.T) {
	d, err := LoadString(`Rectangle {
    ScrollView {
        padding: 16; width: 200; height: 100
        Text { text: "inside" }
    }
}`, NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	view := d.View()
	u := ui.New(nil, nil, func() ui.Element { return view() })
	for i := 0; i < 3; i++ {
		u.Layout(300, 200)
	}
}
