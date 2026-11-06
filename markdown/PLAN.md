# markdown — Obsidian-class knowledge editor on the graphics framework

**Status: plan only.** Nothing here is implemented; no module yet. Like
This is an execute-later blueprint: feature parity is laid
out against **Obsidian** (the product surface) so that when the subproject
starts, scope is a selection problem, not a research problem.

Why it fits this framework: Obsidian is Electron — a web renderer around
local markdown files. We already have the renderer (SDF 2D, rich text,
scroll/list virtualization), the reactive model (`core.Property`), a
declarative document language (`.gml`), and first-class wasm. A native,
no-cgo, instant-startup Obsidian is exactly the kind of app the stack
should be able to carry — and building it forces the framework's biggest
missing primitive (a real multiline text editor) into existence, which
a CAD expression field, a game console and a future gml IDE
all want anyway.

Status markers: ✅ exists · 🔶 partial · ⬜ planned.

---

## 1. What the framework already has

- ✅ Rich text spans (`ui.Rich`), wrapped paragraphs (`ui.Paragraph`),
  fonts with CJK + color-emoji sequences, gradients, SVG, images.
- ✅ Scroll views, virtualized lists, grids, overlays, drag & drop,
  tabs/StackView (gml), pane-like layouts — the workspace chrome is
  mostly assembled from existing widgets.
- 🔶 `ui.TextArea` (2026-10-05) — multi-line editing on `ui.InputState`:
  word wrap, line breaks, Up/Down/Home/End by line, selection by keys and
  drag, clipboard, scroll to caret. Plain text over one string — no
  piece table, undo, or styled runs yet: the seed of the editor, not it.
- ✅ `ui.InputState` — single-line editing: caret, selection, word jumps,
  key handling. The seed of the multiline editor, not the editor.
- ✅ Reactive invalidation with whole-document frame memoization (only
  re-render on change) — right shape for an editor's render loop.
- ✅ `.gml` engine + writer (documents as executable declarative text) —
  candidate plugin/extension layer and canvas file format.
- ✅ Headless screenshots — golden-image tests for markdown rendering.
- 🔶 Keyboard: key events exist on all backends; no clipboard, no IME.
- ✅ `md/` (2026-10-07) — CommonMark + GFM parser, layout and drawing,
  a ui element: the reading half. Not incremental yet.
- ⬜ A real editor buffer (piece table, undo, styled runs), incremental
  re-parsing, file
  watching, full-text search index, platform clipboard.

## 2. Obsidian feature parity map

### 2.1 Markdown dialect
- ⬜ CommonMark + GFM: tables, strikethrough, task lists, autolinks,
  footnotes; setext/ATX headings, nested lists, blockquotes, HR.
- ⬜ Code blocks with syntax highlighting (pure-Go highlighter; start
  with a small language set: go/js/py/json/wgsl/gml).
- ⬜ Obsidian flavor: `[[wikilinks]]` (+aliases `[[note|shown]]`),
  `![[embeds]]` (note/heading/block/image), `==highlight==`,
  `%%comments%%`, callouts (`> [!note]`), tags (`#tag/nested`),
  block IDs (`^block-id`), YAML frontmatter (properties).
- ⬜ Math: LaTeX subset via a pure-Go layout (inline `$..$`, block
  `$$..$$`) — scope to a KaTeX-like common subset, not full TeX.
- ⬜ Diagrams: mermaid subset (flowchart, sequence) rendered to our
  canvas/SVG path pipeline — deliberately small, most-used shapes only.
- Parser decision to make at kickoff: **goldmark** (pure Go, CommonMark
  compliant, extensible — fastest path) vs hand-rolled incremental
  lexer in the `script/gml` style (needed eventually for live preview;
  goldmark can still back reading mode). Likely answer: goldmark first,
  incremental block-level reparse wrapper around it second.

### 2.2 Editor
- ⬜ **Text buffer**: piece table or gap buffer over lines; rune-safe;
  0-alloc steady-state typing; undo/redo as operation log with
  coalescing; deterministic (replayable) edits.
- ⬜ **Multiline editor widget**: wrapped soft lines, caret affinity,
  selection (mouse + shift-keys + word/line), multiple cursors (later),
  auto-indent, list/quote continuation on Enter, bracket/emphasis
  auto-pairing, drag-selection auto-scroll.
- ⬜ Clipboard on all four backends (X11 selections, Win32, web async
  clipboard API, Android) — lands in `app/`, shared by every widget.
- ⬜ IME/composition events (CJK input) — `app/` work; degrade
  gracefully where missing.
- ⬜ Three view modes like Obsidian: **source**, **live preview**
  (hybrid: the active line shows syntax, the rest renders), **reading**.
  Live preview is the hard one — requires the incremental parser and
  a span-to-source mapping.
- ⬜ Find/replace in note; vim mode (later; keymap layer over the
  buffer commands, which should be command-objects from day one).

### 2.3 Vault & files
- ⬜ Vault = local folder of `.md` + attachments; multiple vaults.
- ⬜ File explorer pane (create/rename/move/delete, folders, drag);
  attachments (paste image → file + embed link).
- ⬜ File watching (fsnotify-style; polling fallback on wasm/OPFS) —
  external edits reload; conflict-free because buffer diffs rebase.
- ⬜ Quick switcher (fuzzy open), recent files, bookmarks/starred.
- ⬜ Search: full-text index (pure Go, trigram or inverted index) with
  Obsidian operators (`path:`, `tag:`, `file:`, `line:`, regex);
  search-and-replace across vault (later).
- ⬜ File recovery snapshots (rolling local history, like Obsidian's
  File Recovery core plugin).
- Web target: vault via OPFS/IndexedDB + drag-in folder import; the
  whole app must stay wasm-clean (no cgo, no OS dialogs assumed).

### 2.4 Knowledge graph
- ⬜ Link resolution: wikilinks + markdown links, aliases from
  frontmatter, case/space-insensitive shortest-path resolution.
- ⬜ Backlinks pane (linked mentions) + **unlinked mentions**.
- ⬜ Outgoing links pane; broken-link detection; auto-rename updates
  links vault-wide.
- ⬜ Tag pane with counts/nesting; property (frontmatter) index.
- ⬜ Outline pane (heading tree, click-to-scroll, drag-to-reorganize
  later).
- ⬜ **Graph view**: force-directed layout of the whole vault + local
  graph per note — instanced quads/lines are exactly our renderer's
  fast path; should comfortably beat Electron at 10k+ nodes. Filters
  (tags/folders/orphans), depth control, animation.

### 2.5 Workspace & UX
- ⬜ Panes: unlimited splits (h/v), tabs per pane, pinned tabs, linked
  panes (scroll/selection sync), stacked tabs (later).
- ⬜ Left/right sidebars with dockable panes (explorer, search,
  backlinks, outline, tags, calendar-ish).
- ⬜ Command palette (all commands, fuzzy, hotkey display); global
  hotkey remapping; settings UI (gml-built).
- ⬜ Themes: light/dark + accent, all colors via a theme table —
  community-CSS parity is a non-goal, a clean theme struct is.
- ⬜ Workspaces (saved layouts); note composer (merge/split/refactor).

### 2.6 Core-plugin parity (selection)
- ⬜ Daily notes + calendar strip; periodic notes (later).
- ⬜ Templates (+ template variables `{{date}}`, `{{title}}`).
- ⬜ **Canvas** — infinite whiteboard of cards/notes/edges. Natural fit:
  a 2D camera over instanced cards. File format: `.gml` scene (out
  Obsidian-JSON import/export for interop) — per the
  gml-as-document-format decision.
- ⬜ Slides (H2-split presentation mode), word count, random note,
  audio recorder (uses `audio` module — record is new), slash commands.
- Out of scope permanently: Sync/Publish services (we can point at git),
  community plugin marketplace, Electron/CSS compatibility.

### 2.7 Extensibility (the gml angle)
- ⬜ Plugins as `.gml` documents + JS-subset expressions (the engine we
  already ship): custom panes, commands, renderers for fenced blocks
  (e.g. ```chart), template functions. Sandboxed by construction —
  no FS/net API unless we expose one.
- ⬜ Fenced-block renderer registry (mermaid/math/chart all use it) —
  the one extension point that multiplies.

---

## 3. Staged roadmap

Reuse-first: every stage ships something the framework itself keeps.

- **A. Reader** — goldmark + renderer to `ui` element tree (headings,
  lists, tables, code, images, callouts, wikilinks); vault explorer;
  reading mode; quick switcher; golden-image tests over a corpus
  (dogfood: render this repo's own `*.md` docs). *Deliverable: a fast
  native markdown viewer for the repo.*
- **B. Editor core** — text buffer + undo; multiline editor widget;
  clipboard in `app/` (all backends); source mode editing; autosave;
  find/replace. *Deliverable: `ui.TextEditor`, reusable everywhere.*
- **C. Live preview + workspace** — incremental parse, hybrid
  rendering, span↔source maps; splits/tabs/sidebars; command palette;
  settings; themes. *Deliverable: daily-drivable editor.*
- **D. Knowledge features** — link index, backlinks/unlinked mentions,
  tags, outline, vault search index, rename-refactor, templates, daily
  notes, graph view. *Deliverable: the Obsidian core loop.*
- **E. Long tail** — canvas (gml scenes), math, mermaid subset, syntax
  highlight breadth, vim mode, multiple cursors, slides, plugins-as-gml,
  Obsidian vault import (attachments, canvas JSON), Android/web polish.

## 4. Dependency notes

- Stage B's clipboard + IME work lives in `app/` and unblocks every
  other application's text needs (expression fields, a gml IDE).
- The incremental-parse + span-mapping machinery (stage C) is the same
  discipline as gml's dirty-tracking — consider sharing the invalidation
  design, not just the idea.
- Graph view (stage D) is a showcase for instanced rendering and a
  natural demo for `examples/`.
- SDF glyph atlas (`sdf/IDEAS.md`) directly benefits the editor (crisp
  zoomable text, bold/italic synthesis); if both projects activate,
  sequence SDF text first.
