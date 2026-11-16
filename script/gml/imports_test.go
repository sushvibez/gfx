package gml

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// builtText is every text a built tree shows, joined.
func builtText(el ui.Element) string {
	var sb strings.Builder
	var walk func(ui.Element)
	walk = func(e ui.Element) {
		if e == nil {
			return
		}
		if s, ok := ui.TextOf(e); ok {
			sb.WriteString(s)
			sb.WriteString("\n")
		}
		for _, c := range ui.Children(e) {
			walk(c)
		}
	}
	walk(el)
	return sb.String()
}

// An import that names neither one of Qt's modules nor a module found near
// the document is not silently dropped: the document lists it, and a
// strict (.gml) document does not build until it is fixed. A lenient
// (.qml) page builds as before — its C++ modules are never on disk — and
// stays quiet.
func TestUnresolvedImports(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) string {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// A module that IS there: a qmldir beside the document.
	write("widgets/qmldir", "module widgets\nBadge 1.0 Badge.qml\n")
	write("widgets/Badge.qml", "import QtQuick\nText { text: \"badge\" }\n")
	const page = `import QtQuick
import QtQuick.Controls 2.15
import Qt5Compat.GraphicalEffects
import Qt.labs.settings
import widgets
import backend
import app.models 1.0 as M

Column {
    Badge { }
    Text { text: "page built" }
}
`
	for _, name := range []string{"page.gml", "page.qml"} {
		d, err := Load(write(name, page), NewEnv())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, want := d.UnresolvedImports(), []string{"app.models", "backend"}; !slices.Equal(got, want) {
			t.Errorf("%s: unresolved imports %v, want %v", name, got, want)
		}
		var text string
		logged := captureStderr(t, func() {
			view := d.View()
			u := ui.New(nil, nil, func() ui.Element { return view() })
			u.Layout(300, 200)
			text = builtText(view())
		})
		if strings.HasSuffix(name, ".qml") {
			if !strings.Contains(text, "page built") || !strings.Contains(text, "badge") {
				t.Errorf("%s: the lenient page did not build: %q", name, text)
			}
			if strings.Contains(logged, "import") {
				t.Errorf("%s: a lenient page logged %q", name, logged)
			}
			continue
		}
		for _, want := range []string{"backend", "app.models", "qmldir"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the strict page's error does not mention %q: %q", name, want, text)
			}
		}
		if strings.Contains(text, "page built") {
			t.Errorf("%s: the strict page built with imports missing", name)
		}
	}
	// With nothing missing a strict page builds.
	d, err := Load(write("ok.gml", "import QtQuick\nimport widgets\nColumn { Badge { } }\n"), NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	if got := d.UnresolvedImports(); len(got) != 0 {
		t.Errorf("unresolved imports %v, want none", got)
	}
	if text := builtText(d.View()()); !strings.Contains(text, "badge") {
		t.Errorf("a page with every import found built %q", text)
	}
}
