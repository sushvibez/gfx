package gml

import (
	"testing"

	"github.com/sushvibez/gfx/md/mdui"
	"github.com/sushvibez/gfx/ui"
)

// Text with textFormat: Text.MarkdownText builds a Markdown page, styled
// for the page its colour reads on.
func TestMarkdownText(t *testing.T) {
	src := "Column {\n    Text { width: 300; textFormat: Text.MarkdownText; text: \"# Title\\n\\n- one\\n- two\" }\n}"
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	// A Markdown Text on its own really is a Markdown page: the enum used
	// to evaluate to 8 (Qt's value is 3) and every one built as plain text.
	solo, err := LoadString("Text { width: 300; textFormat: Text.MarkdownText; text: \"# T\" }", NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	if el := solo.View()(); func() bool { _, ok := el.(*mdui.Element); return !ok }() {
		t.Errorf("Text.MarkdownText built a %T, not a Markdown page", el)
	}
	dark := buildMarkdown("# x", ui.RGB(0x101010), 14, 300, nil)
	if _, ok := dark.(*mdui.Element); !ok {
		t.Fatalf("buildMarkdown made a %T", dark)
	}
}
