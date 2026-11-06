Test data from the specifications the parser is checked against:

- `spec.json` — the CommonMark specification's examples, version 0.31.2,
  from https://spec.commonmark.org/0.31.2/spec.json (© John MacFarlane,
  CC BY-SA 4.0). `spec_test.go` runs all 652 through `ParseCommonMark`.
- `gfm-spec.txt` — GitHub Flavored Markdown's spec, from
  https://github.com/github/cmark-gfm/blob/master/test/spec.txt (CC BY-SA
  4.0). `gfm_test.go` runs its extension examples (tables,
  strikethrough, autolinks, the tag filter) through `Parse`.
