package gml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// A delegate row is built by a CALLBACK the ui layer runs during layout —
// after the frame's build phase, and once per layout pass. That is
// outside the reach of beginFrame, which is where the per-frame repeat
// counter behind cache keys is reset (see build).
//
// Left to accumulate, the repeat suffix climbs for as long as the window
// is open: every row build asks for a key nothing has ever stored, and
// the subtree map grows for ever. It reached #86 on a seven-row list in
// sixty frames before enterRow started putting the counts back.
//
// So: once a page has settled, its subtree map stops growing. A page
// whose MODEL grows (the chat page's bot keeps talking) grows with it
// and then levels off, which is why this settles first and measures
// after.
func TestRowCacheDoesNotGrow(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, page := range []string{"navdisplay", "chatui", "indicators", "treeview", "lists"} {
		path := filepath.Join(home, "workspace", "qml-examples", "examples", page, "Main.qml")
		if _, err := os.Stat(path); err != nil {
			t.Skip("corpus not found")
		}
		d, err := Load(path, NewEnv())
		if err != nil {
			t.Errorf("%s: %v", page, err)
			continue
		}
		view := d.View()
		u := ui.New(nil, nil, func() ui.Element { return view() })
		layout := func(n int) {
			for i := 0; i < n; i++ {
				u.Layout(900, 700)
			}
		}
		layout(900) // settle, models included
		settled := d.doc.subtrees.count()
		layout(300)
		grown := d.doc.subtrees.count()
		t.Logf("%-12s subtree entries: %d settled, %d after another 300", page, settled, grown)
		if grown > settled {
			t.Errorf("%s: the subtree map grew from %d to %d over 300 settled frames — "+
				"a row is being filed under a new key every build", page, settled, grown)
		}
	}
}
