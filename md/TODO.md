# md — Markdown for the framework

Parser (`md`), layout and drawing (`md/mdrender`), a ui element
(`md/mdui`), a headless viewer (`md/cmd/mdview`). Used by `.gml`
`Text { textFormat: Text.MarkdownText }` and a game's mission
descriptions.

## Done (2026-10-07, 2026-10-09)
- CommonMark blocks: ATX/setext headings, paragraphs, block quotes (lazy
  continuation), bullet and ordered lists (tight/loose, start numbers),
  fenced and indented code, thematic breaks, HTML blocks, link reference
  definitions.
- Inlines: emphasis/strong by the delimiter-stack algorithm (flanking
  rules, rule of three), code spans, links and images (inline, full,
  collapsed, shortcut references), autolinks, raw HTML, escapes, entities,
  hard/soft breaks.
- GitHub extensions: tables (alignment, escaped pipes), task lists,
  strikethrough, bare links.
- **The whole CommonMark spec, 652 of 652** (0.31.2, `testdata/spec.json`,
  `TestCommonMarkSpec`, 2026-10-09) through `ParseCommonMark`, the strict
  parser — `Parse` keeps GitHub's extensions. Getting there: tabs kept in
  content (only the indentation is expanded), all of HTML5's named
  entities, `&quot;` and percent-encoded destinations, the spec's raw HTML
  grammar, list tightness from the gaps between blocks, definitions taken
  out before a setext underline, case-folded labels. Fuzzed 5M inputs
  through both parsers clean.
- Layout: word wrap with mixed faces on one baseline, headings, lists with
  depth-shaped bullets and numbers, task boxes, quotes, code panels, tables
  sized to content, link hit boxes.
- Images: `mdrender.Images` loads files relative to a base directory,
  http(s) URLs in the background (`OnLoad`, `Gen`, `Wait`), data: URIs and
  bytes the host adds; sizes come from the header, textures are made on
  first draw (capped at 2048 px). A picture is laid out at its natural size,
  shrunk to the column, standing on its line; a missing one shows its alt
  text. `mdui.Options.Images`, `mdview -base`.

- GitHub extensions, all of GitHub's own examples (`testdata/gfm-spec.txt`,
  `TestGFMExtensions`, 2026-10-09): tables, strikethrough, extended
  autolinks (www., http(s)://, ftp://, email, the domain and trailing
  punctuation rules), the tag filter in HTML. Plus footnotes (numbered in
  order of reference, unused ones dropped, multi-paragraph, GitHub's
  HTML), definition lists (`Term` / `: definition`) and a GitHub slug on
  every heading (`Node.ID`). Strict `ParseCommonMark` has none of them.
- Layout of all of that: footnote numbers raised and linked, the notes
  under a short rule with a way back (↑), definition lists, and anchors —
  `Page.Anchor("#slug")`, `#fn-x`, `#fnref-x` — for links within a page.
  Inline HTML is interpreted where Markdown writers use it (b/strong,
  i/em, code/kbd, s/del, sub/sup, small, br, a, img) and other tags are
  not drawn; an HTML block is drawn as that, centred when it says
  align="center" (a README's logo and badges), scripts and styles left
  out, <pre> as code.
- `mdui.View` (2026-10-09): a page with state — drag to select, Ctrl+C
  copies, Ctrl+A selects all, a click follows a link, a "#…" link asks
  the host to scroll (`OnScroll`). `mdrender.Page` has `Hit`, `Text` and
  `Rects` for it. New in `ui`: `DivEl.OnPointer`, a press-move-release
  handler told where the pointer is.
- `.gml`: `Text.MarkdownText` was the wrong number (8; Qt's is 3), so no
  Markdown text in a .gml file ever rendered as Markdown — it does now,
  with pictures relative to the document and `onLinkActivated` (with
  `link`) on a click. a game's mission descriptions show pictures
  from beside the mission store.

## Open
- [ ] Pictures: SVG, animated GIF frames, `<img width=…>` sizing, a
      placeholder box while a URL loads.
- [ ] Selection on a plain `mdui.Markdown` element (it is `View` that has
      it) and in `.gml` Markdown text.
- [ ] The editor is its own project: `markdown/PLAN.md`.
