package md

import "testing"

// GitHub's footnotes, definition lists and heading anchors: Parse only.
func TestExtensions(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Here[^1] and again[^1].\n\n[^1]: The note.\n",
			"<p>Here<sup class=\"footnote-ref\"><a href=\"#fn-1\" id=\"fnref-1\" data-footnote-ref>1</a></sup> and again<sup class=\"footnote-ref\"><a href=\"#fn-1\" id=\"fnref-1-2\" data-footnote-ref>1</a></sup>.</p>\n" +
				"<section class=\"footnotes\" data-footnotes>\n<ol>\n<li id=\"fn-1\">\n<p>The note." +
				" <a href=\"#fnref-1\" class=\"footnote-backref\" data-footnote-backref data-footnote-backref-idx=\"1\" aria-label=\"Back to reference 1\">↩</a>" +
				" <a href=\"#fnref-1-2\" class=\"footnote-backref\" data-footnote-backref data-footnote-backref-idx=\"1\" aria-label=\"Back to reference 1\">↩</a></p>\n</li>\n</ol>\n</section>\n"},
		// Numbered in the order they are referred to; an unused one dropped;
		// a note's content may run on, indented.
		{"b[^b] a[^a]\n\n[^a]: A.\n[^b]: B\n    more.\n[^c]: never used\n",
			"<p>b<sup class=\"footnote-ref\"><a href=\"#fn-b\" id=\"fnref-b\" data-footnote-ref>1</a></sup> a<sup class=\"footnote-ref\"><a href=\"#fn-a\" id=\"fnref-a\" data-footnote-ref>2</a></sup></p>\n" +
				"<section class=\"footnotes\" data-footnotes>\n<ol>\n<li id=\"fn-b\">\n<p>B\nmore. <a href=\"#fnref-b\" class=\"footnote-backref\" data-footnote-backref data-footnote-backref-idx=\"1\" aria-label=\"Back to reference 1\">↩</a></p>\n</li>\n" +
				"<li id=\"fn-a\">\n<p>A. <a href=\"#fnref-a\" class=\"footnote-backref\" data-footnote-backref data-footnote-backref-idx=\"2\" aria-label=\"Back to reference 2\">↩</a></p>\n</li>\n</ol>\n</section>\n"},
		// Not defined: just text.
		{"x[^nope]\n", "<p>x[^nope]</p>\n"},
		// Definition lists.
		{"Apple\n: A fruit.\n: A company.\n\nGo\n: A language\n", "<dl>\n<dt>Apple</dt>\n<dd>A fruit.</dd>\n<dd>A company.</dd>\n</dl>\n<dl>\n<dt>Go</dt>\n<dd>A language</dd>\n</dl>\n"},
		{"Term\n: First para\n\n  Second para\n", "<dl>\n<dt>Term</dt>\n<dd>\n<p>First para</p>\n<p>Second para</p>\n</dd>\n</dl>\n"},
		{"Not a term\n:no space\n", "<p>Not a term\n:no space</p>\n"},
	} {
		if got := HTML(Parse(tc.in)); got != tc.want {
			t.Errorf("%q\n got  %q\n want %q", tc.in, got, tc.want)
		}
	}
	// Strict CommonMark has none of it.
	if got := HTML(ParseCommonMark("Term\n: def\n")); got != "<p>Term\n: def</p>\n" {
		t.Errorf("strict parser made a definition list: %q", got)
	}
}

func TestHeadingSlugs(t *testing.T) {
	doc := Parse("# Getting Started!\n## Getting started\n### Ünïcode & `code`\n")
	var ids []string
	Walk(doc, func(n *Node) bool {
		if n.Kind == Heading {
			ids = append(ids, n.ID)
		}
		return true
	})
	want := []string{"getting-started", "getting-started-1", "ünïcode--code"}
	for i := range want {
		if i >= len(ids) || ids[i] != want[i] {
			t.Fatalf("ids %q, want %q", ids, want)
		}
	}
}
