# Markdown in the framework

A parser that follows **CommonMark**, with the *GitHub* extensions, drawn by the
framework's own renderer. It handles `inline code`, ~~strikethrough~~, links such as
[the spec](https://spec.commonmark.org/), bare links like https://example.com,
and ***nested emphasis with a [link inside](https://example.com)***.

## Lists

- Tight lists keep their items close
- Nested lists change their bullet
  - a ring at the second level
    - a square at the third
- Back to the top

1. Ordered lists number themselves
2. And keep counting
3. Even past nine

- [x] Parser
- [x] Layout
- [ ] An editor

## Quotes and code

> A quote is set off by a bar, its text quieter than the body.
> It can hold **anything** a document can.

```go
func main() {
	doc := md.Parse(src)
	page := mdrender.Layout(doc, 600, mdrender.DarkStyle(15), fonts.Metrics())
	page.Draw(r, fonts, x, y)
}
```

## Pictures

![A wedge of five drones at dusk](sample.png)

Pictures load from files next to the document, from `http(s)` URLs in the background, or from `data:` URIs; one inside a line ![drone](icon.png) sits on its baseline, one wider than the page is scaled to fit it, and one that is missing ![shows its alt text](missing.png).

## Notes, terms and HTML

Footnotes are numbered as they are referred to[^cm], and each one links back[^why].
Raw HTML that Markdown writers use is drawn as what it means: <kbd>Ctrl</kbd>+<kbd>C</kbd> copies,
H<sub>2</sub>O and E = mc<sup>2</sup>, <b>bold</b> and <del>struck</del>; anything else is not drawn.

CommonMark
: The specification this parser passes all 652 examples of.

GFM
: GitHub's extensions: tables, task lists, strikethrough, bare links, footnotes.

## Tables

| Mission | Aircraft | Landed |
| :--- | :---: | ---: |
| Fast and low | 3 × Lance-J | 3 |
| Long leg | 4 × Ranger-ICE | 4 |
| River follow | 20 × Harrier-V | 2 |

---

That is a thematic break above. Line one ends with two spaces  
so this is a new line in the same paragraph.

[^cm]: The spec's own examples are the test suite: `testdata/spec.json`.
[^why]: So a reader who followed one can find their place again.
