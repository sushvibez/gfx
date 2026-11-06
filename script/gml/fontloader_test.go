package gml

import (
	"os"
	"testing"
)

// TestFontLoader: both declaration forms load a real face, expose .name,
// and register the family for font.family lookups.
func TestFontLoader(t *testing.T) {
	const face = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
	if _, err := os.Stat(face); err != nil {
		t.Skip("DejaVuSans not installed")
	}
	src := `
Column {
    FontLoader { id: byChild; source: "` + face + `" }
    property FontLoader byProp: FontLoader { source: "` + face + `" }
    property string famA: byChild.name
    property string famB: byProp.name
    Text { text: famA; font.family: famA; color: "#ffffff" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	famA := str(d.doc.props["famA"].get())
	famB := str(d.doc.props["famB"].get())
	if famA != "DejaVu Sans" || famB != "DejaVu Sans" {
		t.Fatalf("family = %q / %q, want DejaVu Sans", famA, famB)
	}
}
