package md

import (
	"strings"
	"testing"
)

// Cases from the CommonMark spec (0.31) and the GitHub extensions, with
// the reference HTML (quotes escaped &quot;, destinations percent-encoded,
// as the reference renderer does). The whole spec is spec_test.go.
func TestHTML(t *testing.T) {
	for i, tc := range []struct{ in, want string }{
		// Headings.
		{"# foo", "<h1>foo</h1>\n"},
		{"###### foo", "<h6>foo</h6>\n"},
		{"####### foo", "<p>####### foo</p>\n"},
		{"#5 bolt", "<p>#5 bolt</p>\n"},
		{"## foo ##", "<h2>foo</h2>\n"},
		{"Foo *bar*\n=========", "<h1>Foo <em>bar</em></h1>\n"},
		{"Foo\n---", "<h2>Foo</h2>\n"},
		// Rules.
		{"***\n---\n___", "<hr />\n<hr />\n<hr />\n"},
		{"- - -", "<hr />\n"},
		// Paragraphs and breaks.
		{"aaa\nbbb\n\nccc", "<p>aaa\nbbb</p>\n<p>ccc</p>\n"},
		{"foo  \nbar", "<p>foo<br />\nbar</p>\n"},
		{"foo\\\nbar", "<p>foo<br />\nbar</p>\n"},
		// Code.
		{"    a simple\n      indented code block", "<pre><code>a simple\n  indented code block\n</code></pre>\n"},
		{"```\n<\n >\n```", "<pre><code>&lt;\n &gt;\n</code></pre>\n"},
		{"```ruby\ndef foo(x)\n  return 3\nend\n```", "<pre><code class=\"language-ruby\">def foo(x)\n  return 3\nend\n</code></pre>\n"},
		{"`foo`", "<p><code>foo</code></p>\n"},
		{"`` foo ` bar ``", "<p><code>foo ` bar</code></p>\n"},
		{"`foo   bar \nbaz`", "<p><code>foo   bar  baz</code></p>\n"},
		// Block quotes.
		{"> # Foo\n> bar\n> baz", "<blockquote>\n<h1>Foo</h1>\n<p>bar\nbaz</p>\n</blockquote>\n"},
		{"> bar\nbaz\n> foo", "<blockquote>\n<p>bar\nbaz\nfoo</p>\n</blockquote>\n"},
		// Lists.
		{"- foo\n- bar\n+ baz", "<ul>\n<li>foo</li>\n<li>bar</li>\n</ul>\n<ul>\n<li>baz</li>\n</ul>\n"},
		{"1. foo\n2. bar\n3) baz", "<ol>\n<li>foo</li>\n<li>bar</li>\n</ol>\n<ol start=\"3\">\n<li>baz</li>\n</ol>\n"},
		{"- foo\n\n- bar", "<ul>\n<li>\n<p>foo</p>\n</li>\n<li>\n<p>bar</p>\n</li>\n</ul>\n"},
		{"- foo\n  - bar\n    - baz", "<ul>\n<li>foo\n<ul>\n<li>bar\n<ul>\n<li>baz</li>\n</ul>\n</li>\n</ul>\n</li>\n</ul>\n"},
		{"1. a\n\n   b", "<ol>\n<li>\n<p>a</p>\n<p>b</p>\n</li>\n</ol>\n"},
		{"The number of windows in my house is\n14.  The number of doors is 6.", "<p>The number of windows in my house is\n14.  The number of doors is 6.</p>\n"},
		{"- [ ] open\n- [x] done", "<ul>\n<li><input type=\"checkbox\" disabled=\"\" /> open</li>\n<li><input type=\"checkbox\" checked=\"\" disabled=\"\" /> done</li>\n</ul>\n"},
		// Emphasis.
		{"*foo bar*", "<p><em>foo bar</em></p>\n"},
		{"a * foo bar*", "<p>a * foo bar*</p>\n"},
		{"foo*bar*", "<p>foo<em>bar</em></p>\n"},
		{"foo_bar_", "<p>foo_bar_</p>\n"},
		{"**foo bar**", "<p><strong>foo bar</strong></p>\n"},
		{"*foo **bar** baz*", "<p><em>foo <strong>bar</strong> baz</em></p>\n"},
		{"*foo**bar**baz*", "<p><em>foo<strong>bar</strong>baz</em></p>\n"},
		{"***strong emph***", "<p><em><strong>strong emph</strong></em></p>\n"},
		{"**foo*", "<p>*<em>foo</em></p>\n"},
		{"*foo**", "<p><em>foo</em>*</p>\n"},
		{"~~gone~~ stays", "<p><del>gone</del> stays</p>\n"},
		// Links and images.
		{"[link](/uri \"title\")", "<p><a href=\"/uri\" title=\"title\">link</a></p>\n"},
		{"[link](</my uri>)", "<p><a href=\"/my%20uri\">link</a></p>\n"},
		{"[link](foo(and(bar)))", "<p><a href=\"foo(and(bar))\">link</a></p>\n"},
		{"[foo]: /url \"title\"\n\n[foo]", "<p><a href=\"/url\" title=\"title\">foo</a></p>\n"},
		{"[Foo bar]\n\n[foo BAR]: /url", "<p><a href=\"/url\">Foo bar</a></p>\n"},
		{"[link *foo **bar** `#`*](/uri)", "<p><a href=\"/uri\">link <em>foo <strong>bar</strong> <code>#</code></em></a></p>\n"},
		{"[foo [bar](/uri)](/uri)", "<p>[foo <a href=\"/uri\">bar</a>](/uri)</p>\n"},
		{"![foo *bar*](train.jpg \"train & tracks\")", "<p><img src=\"train.jpg\" alt=\"foo bar\" title=\"train &amp; tracks\" /></p>\n"},
		{"<http://foo.bar.baz>", "<p><a href=\"http://foo.bar.baz\">http://foo.bar.baz</a></p>\n"},
		{"<foo@bar.example.com>", "<p><a href=\"mailto:foo@bar.example.com\">foo@bar.example.com</a></p>\n"},
		{"see https://example.com/a.", "<p>see <a href=\"https://example.com/a\">https://example.com/a</a>.</p>\n"},
		{"[not a link]", "<p>[not a link]</p>\n"},
		// Escapes and entities.
		{"\\*not emphasized*", "<p>*not emphasized*</p>\n"},
		{"&copy; &#35; &#x22;", "<p>© # &quot;</p>\n"},
		// HTML.
		{"<div>\n*hello*\n</div>", "<div>\n*hello*\n</div>\n"},
		{"foo <span>bar</span>", "<p>foo <span>bar</span></p>\n"},
		// Tables.
		{"| a | b |\n| --- | ---: |\n| 1 | 2 |", "<table>\n<thead>\n<tr>\n<th>a</th>\n<th align=\"right\">b</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>1</td>\n<td align=\"right\">2</td>\n</tr>\n</tbody>\n</table>\n"},
		{"| a |\n| - |\n| `x|y` |", "<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td><code>x|y</code></td>\n</tr>\n</tbody>\n</table>\n"},
	} {
		if got := HTML(Parse(tc.in)); got != tc.want {
			t.Errorf("case %d %q:\n got %q\nwant %q", i, tc.in, got, tc.want)
		}
	}
}

// The parser never fails and never loses text: whatever it cannot read as
// structure comes out as text.
func TestNeverLosesText(t *testing.T) {
	for _, in := range []string{"[", "]", "**", "`", "<", "&", "- ", "> ", "```", "|", "![", "[a](", "*_*_", "\\"} {
		doc := Parse(in)
		got := PlainText(doc)
		for _, c := range strings.TrimSpace(in) {
			// Markup characters may become markup; everything else stays.
			if !strings.ContainsRune(got+HTML(doc), c) && !strings.ContainsRune("*_~>-`", c) {
				t.Errorf("%q lost %q (plain text %q)", in, c, got)
			}
		}
	}
}
