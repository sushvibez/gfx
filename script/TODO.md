# TODO — the .gml engine

`script/gml`: the QML-compatible markup engine (lexer, parser, JS
expression evaluator, element builder) and `script/cmd/gfx`, the WGSL
codegen. The rest of the framework's list is the repo-root
[`TODO.md`](../TODO.md).

Kept current by hand: when something here changes state, this file
changes in the same commit. Detail lives where the work is; this page
says what is open, what it is waiting on, and where to look. Dates are
when the item was last touched.

## Now

### QML parity review — 1 of 65 pages signed off (2026-09-14)

All 65 pages *render*, with zero warnings, since 2026-08-26. What is
open is **visual and interaction parity**, judged by eye, one page at a
time: `scripts/qml-trio.sh <page>` opens the page three times at once —
real Qt, our .gml engine, and a Go port written straight against the ui
builder — and nothing moves to the next page without the user's
go-ahead.

- Machine-readable status: `gml/check/REVIEW-STATUS.tsv` (page, gml, go).
- Findings log: `gml/check/QML-EXAMPLES.md`, item 88.
- The parity board artifact needs republishing as a new artifact (the
  old link is dead); generated from the TSV by the scratchpad's
  `refresh_board.py`.

Open on the current page (**buttons**, round 8): confirm against the Qt
window that clicking a second tool button while a menu is open closes
the menu without opening the other — modal popup behaviour, on both our
sides.

#### The 65 pages, in review order

What each one actually exercises, so the order can be re-cut by what is
being worked on rather than by what happens to come next. `✅` signed
off, `👀` in review, `▢` pending; **go** marks a page whose Go port
exists (`examples/qmlgo/<page>`).

**Controls** — the Qt Quick Controls set, each page a grid of cards.

| page | what it exercises | |
| --- | --- | --- |
| buttons | six card kinds: standard, states, icon, tool-button menus, custom styles, and four styled buttons from a component file (one of them animated) | 👀 go |
| sliders | horizontal, vertical, a Dial, a glow demo, volume actions | ▢ go |
| rangesliders | two-handle sliders, labelled and vertical; neither handle may pass the other | ▢ |
| switches | ten toggle shapes — switch, checkbox, radio, segmented, pill, day/night, large knob, icon, coloured track, a smart-home panel | ▢ |
| combobox | basic, editable, model-backed, interactive | ▢ |
| tabbar | basic, icon, dynamically added tabs, interactive | 👀 |
| toolbar | basic, action, context, interactive | ▢ |
| menubar | menu bar, checkable items, context menu, interactive | 👀 |
| popups | ten: dialog, popup, menu, tooltip, snackbar, toasts, alerts, bottom sheet, command palette, floating action button | 👀 |
| drawers | four edges plus a contained drawer | 👀 |
| scrollview | basic, custom scrollbar, Flickable, interactive | ▢ |
| splitview | horizontal, vertical, nested, interactive | 👀 |
| swipeview | basic, carousel, page indicator, interactive | ▢ |
| layouts | ten: Row/Column, Grid, Flow, StackLayout, responsive breakpoints, holy grail, masonry, collapsible sidebar, draggable split, nested dashboard | ▢ |
| lists | ten: ListView, GridView, sections, carousel, kanban, timeline, accordion, swipe-to-action, chat bubbles, dynamic | ▢ |
| gridview | basic, dynamic, photo grid, interactive | ▢ |
| indicators | twelve gauges: progress bar, busy, dial, battery, signal, speedometer, tachometer, thermometer, VU meter, step progress, circular rings, system monitor | ▢ |
| textinputs | eleven: text field, text area, search, PIN, tags, char limit, editable labels, star rating, colour picker, spin/combo, a form builder | ▢ |
| date | a calendar and a Tumbler date picker | 👀 |
| filedialogs | file, folder, save and an interactive dialog | ▢ |
| flickable | basic, snapping, pinch-zoom, interactive | ▢ |
| pathview | circular, arc, cover flow, interactive | ▢ |
| states | states, transitions, a state machine, interactive | ▢ |
| loaderex | Loader: basic, from a Component, dynamic creation, interactive | ▢ |
| treeview | file system, organisation chart, interactive | 👀 |
| tableview | basic, editable cells, sort/filter | 👀 |
| settings | preferences, key/value, recent files, an info card | ▢ |

**Graphics and animation** — the pages that exercise the renderer more
than the control set.

| page | what it exercises | |
| --- | --- | --- |
| animations | thirteen: easing curves, Behavior/spring, sequential and parallel, states/transitions, staggered list, orbital system, Newton's cradle, Lissajous, sine bars, matrix rain, particle fountain, breathing circles, 3D flip cards | ▢ |
| chatui | bubbles, typing indicator, emoji picker, an interactive bot | ▢ |
| toasts | a toast host with an API, a queue and a playground | ▢ |
| images | fill modes, BorderImage, masking, interactive | ▢ |
| canvas | ten Canvas2D drawings: clock, radar sweep, kaleidoscope, fractal tree, spirograph, audio visualiser, drawing pad, gradients, paths | ▢ |
| shapes | twelve QtQuick.Shapes: Bézier, arcs, SVG paths, fill rules, gradients, gear train, DNA helix, mandala, rose curve, liquid blob, oscilloscope | ▢ |
| transforms | eleven: 2D and 3D rotation, matrix shear, card flip, 3D carousel, parallax, elastic spring, wave grid, neon glow, graphical effects | ▢ |
| visualeffects | Qt5Compat.GraphicalEffects, through our stub module | ▢ |
| frostglass | a blurred-backdrop glass card | ▢ |
| lenstabs | a lens circle and rect effect over a tab strip | ▢ |
| lensphoto | the same lens over a photograph | ▢ |
| portal | a portal canvas with a reflection pass | ▢ |
| diorama | QtQuick3D — modelled, no 3D backend behind it | ▢ |
| multiplane | a layered parallax scene | ▢ |
| spritestack | stacked sprite slices | ▢ |
| particles | QtQuick.Particles: emitter, trail, affectors, interactive | ▢ |
| shaders | ShaderEffect: blur, colour, glow/shadow, interactive | ▢ |
| cards2d5 | a 2.5D depth card | ▢ |

**Instrument panels** — dense custom drawing, the pages closest to what
the framework is for.

| page | what it exercises | |
| --- | --- | --- |
| hud | a head-up display canvas with a control panel | ▢ |
| pfd | primary flight display: artificial horizon, heading, speed/altitude tapes, VSI and turn | ▢ |
| ecam | engine gauges, fuel synoptic, system status, warning panel | ▢ |
| navdisplay | moving map, flight plan, compass rose, mode/range — state shared from the root down four cards | ▢ |
| aircraftmap | a blueprint viewer with a marker sidebar (the page whose `Component.onCompleted` reads a Flickable's width) | ▢ |
| graphs | ten charts: bar, multi-series, candlestick, donut, radar, scatter, heatmap, scrolling waveform, system monitor, vibration sensor | ▢ |
| charts3d | QtGraphs bars, scatter and surface — modelled | ▢ |
| graphics3d | QtQuick3D — modelled | ▢ |
| customitem | QQuickPaintedItem stand-ins: gauge, painted clock, waveform, interactive draw | ▢ |
| multimedia | QtMultimedia audio, video and camera — modelled, no backend | ▢ |
| maps | QtLocation map with compass overlay, controls and info panel — modelled | ▢ |
| pdfreader | QtQuick.Pdf — modelled | ▢ |

**C++-backed** — pages whose data comes from the app's registered C++
types. The bare `qml` runtime cannot instantiate those either, so both
sides render the chrome with the data areas empty; this is where a page
is read on our side only.

| page | what it exercises | |
| --- | --- | --- |
| database | CRUD table, query explorer, data dashboard | ▢ |
| threads | a pipeline with flow, control, records and thread info | ▢ |
| websocket | connection, message log, send | ▢ |
| ethernet | connection, hex view, message log, send | ▢ |
| asynccpp | cancel, concurrent run, parallel map, progress | ▢ |
| theorycpp | a chapter panel, content panel and code blocks | ▢ |
| qmlcppbridge | property binding, method calls, signals, interactive | ▢ |
| network | REST, XHR, JSON parsing, interactive | ▢ |

### gml frame cost — the last named step is done (2026-09-25)

`gml/check/QML-EXAMPLES.md` item 89. Yardstick:
`BenchmarkCorpusButtonsFrame` (here) against `BenchmarkButtonsFrame`
(`examples/qmlgo/buttons`), one frame of the buttons page with its pulse
animation running.

**509 → 147 µs** on buttons; the Go port is 31 µs. The pages built out
of delegate rows moved much further — navdisplay 1087 → 92 µs, chatui
2913 → 131, indicators 3520 → 524 — and
`BenchmarkCorpusNavDisplayFrame` and friends now measure them.

- ✅ Steps 1–5 of the integer-key plan: interned keys, columns, subtree
  patching (on by default; `GFX_NO_PATCH=1` off, `GFX_PATCH_VERIFY=1|all`
  verifier).
- ✅ **The use site is a slot** — the step this was waiting on, and it
  cost two lines. A component use site's build resolves the component,
  merges the use-site attributes into a derived node, declares the
  instance's properties and binds its scope; on the buttons page that
  was **58 % of a frame**, run twice over (the card, and the styled
  button inside it) every frame the animation ticked, to arrive back at
  the elements it already had. Registering the instance build as one of
  the use site's slots makes it patchable, and it patches because the
  instance's own subtree patches in place and comes back as the *same*
  element — which the use site already finished (layout hints, wrappers,
  registration), so none of that has to be redone. **3 fresh rebuilds a
  frame → 1**, which is the animated leaf and nothing else. 194 → 148 µs.
  The fallback is unchanged: when the instance does come back as a new
  element the patch bails out and the use site rebuilds, because the
  finish step still is not re-applicable.
- ✅ Verified, not assumed: all 65 corpus pages rendered before and
  after — **63 byte-identical**, and the two that differ (customitem,
  scrollview) differ between two runs of the *same* binary, so they are
  animation timing. Zero warnings either way, same exit codes, and
  `GFX_PATCH_VERIFY=all` reports exactly the same set of artifacts as
  before the change.
- ✗ AST→closure compilation — dropped: evaluation is ~3 % of a frame.
- ✅ **A delegate row is cacheable now** (2026-09-25), which is what
  the untracked read-sets were really about. A model row is bound into
  the delegate's scope as plain identifiers — `index`, `model`, every
  role by name — and plain values are exactly what a read-set cannot
  see. The engine's answer was to give up on the whole interior of a
  view: `build` skipped the subtree cache outright while `rowDepth > 0`,
  so a ListView, GridView, Repeater or PathView rebuilt every element of
  every row every frame, however little had changed.

  The fix is the one `listModel.ver` already used: put a token in front
  of the data. Each row carries a `core.Property` holding a hash of its
  contents and its index, and the row's names bind as Refs that read the
  token first (`rowtoken.go`). It is all-or-nothing per row — a row
  holding something the hash cannot follow exactly is bound the old way
  and stays uncacheable, because a hash that might miss a change would
  serve a stale row.

  | | before | after |
  | --- | --- | --- |
  | navdisplay | 1087 µs, 91 rebuilds | **113 µs, 0** |
  | chatui | 2913 µs, 196 | **769 µs, 38** |
  | indicators | 3520 µs, 228 | **1075 µs, 34** |
  | buttons | 139 µs, 1 | 142 µs, 1 |

  Allocations follow: indicators 1086 kB a frame → 301 kB, chatui 745 →
  244. buttons pays about **2 %** for the hashing and gains nothing —
  it has almost no rows — which is the honest cost of the trade.

  Three things had to be fixed with it, each its own bug:
  - **The repeat counter grew without bound.** A row is built by a
    callback the ui layer runs during layout, after the build phase and
    once per layout pass, where `beginFrame`'s reset cannot reach it.
    The suffix behind a cache key climbed past `#80` on a seven-row list
    in sixty frames, every build missed a key nothing had stored, and
    the subtree map grew for as long as the window was open. `enterRow`
    puts the counts back now, so every build of a row numbers its
    subtrees the same way and finds them. `TestRowCacheDoesNotGrow` is
    the guard: without it navdisplay reaches 81 572 entries and climbs.
  - **A view snapshotted its row scope afresh every build**, and a row's
    cache entry guards on env IDENTITY — so every row in a list was
    invalidated by its own list rebuilding. Memoized per view now.
  - **A styled control could be rebuilt without its scope in hand.** The
    control's id is only in the registry while its own build is in
    flight; a patch redoing one slot inside it arrives after that. The
    Tumbler was the one that showed it — the rebuilt wheel found no
    control, made its own private `currentIndex`, and the wheel took one
    drag and then ignored every later one. It falls back to the object's
    durable home (`doc.tplObjs`) now.

  Verified: all 65 corpus pages rendered before and after, zero warnings
  either way, `GFX_PATCH_VERIFY=all` reporting exactly the baseline set
  of artifacts, and the two pages that changed both checked by eye —
  chatui's two-line outgoing bubble is now aligned and sized like the
  one above it, which it was not before, and treeview's file sizes
  changed because that page lists this repository and `TODO.md` had
  just been rewritten.

  `GFX_NO_ROW_CACHE=1` puts rows back outside the cache, for bisecting a
  suspected stale row.
- ✅ **The rest of the untracked reads** (2026-09-25). Six causes, each
  its own small thing, and between them they took the last of the
  cache misses off the corpus:

  | page | before | after |
  | --- | --- | --- |
  | chatui | 740 µs, 31 rebuilds | **131 µs, 0** |
  | indicators | 1036 µs, 34 | **524 µs, 5** |
  | navdisplay | 101 µs, 0 | **92 µs, 0** |
  | sliders | 12 rebuilds | **1** |
  | shapes | 19 | **9** |
  | buttons | 142 µs, 1 | 147 µs, 1 |

  Allocations: chatui 245 kB a frame → 42, indicators 301 → 139.

  - **A JS local is derived.** `var r = cx - Style.resize(4)` in a
    Canvas handler was read back as an injected value, so every subtree
    using it was uncacheable — 28 reads a frame on one page. A local is
    computed inside the very computation being tracked, from inputs
    already recorded as they were read; reading it back adds nothing.
  - **A shadowed binding kept its value but lost its derived mark.** An
    item nested inside another item restored the outer `opacity` and
    `visible` bindings without their marks, and every read of them from
    then on poisoned the reader. This one was worth 27 rebuilds a frame
    on indicators by itself.
  - **The memoized scope snapshot copied the frozen builtins into its
    own vars**, where nothing marks them derived — so after the first
    memo hit every `Math.`, `Animation.` and `Easing.` in the document
    poisoned whatever read it. They are reachable through the kept
    parent; refreshing them was never needed.
  - **A model read now takes its version property.** `listModel.ver` is
    stamped on every mutation, which is exactly a tracked read, and
    `count` / `length` take it too.
  - **An item’s own attributes are derived** where a sibling attribute
    reads them back (`cellHeight: cellWidth`), for the same reason a
    local is.
  - **A row’s `ListView` / `GridView` / `PathView` attached object is a
    Ref**, resolved on read, so `ListView.isCurrentItem` records the
    dependency on the view’s currentIndex in the reader’s read-set
    rather than being computed into a plain map beforehand.

  One thing was tried and taken out again: carrying derived marks
  through `withCtx`, so a patch re-establishes an ancestor’s marks
  exactly. It changed no page’s rebuild count and cost **a fifth of the
  buttons frame**, because it runs for every name every ancestor bound
  on every slot a patch redoes. Recorded at the call site so it is not
  tried again without a measurement.

  What is left is not untracked reads. indicators rebuilds 5 subtrees a
  frame and shapes 9, all of them `ownStale` — their own read-sets are
  genuinely stale, which is an animating page doing the work it should.
- ✅ **A row default compared by formatting** (2026-10-10). The per-
  instance live default (`if str(raw.get()) != str(v)`) formatted both
  sides with `%v`, which on a `var` property holding an object sorted
  the map's keys and printed every member, every row, every frame.
  `sameValue` compares scalars by value, objects by identity then
  `reflect.DeepEqual`. navdisplay 88 → 65 µs, 553 → 385 allocs; the
  other pages are unchanged (it was navdisplay's map-valued property).
- ▢ **A row that will hit the cache still declares its whole scope**
  (2026-10-10, measured, not done). On indicators (520 µs, 5 ownStale
  rebuilds a frame) `enterRow` → `declareRowScope` is 15 % of the frame
  and `walkRowNodesParent` another 9 %: every row of a Repeater whose
  container is rebuilding walks its delegate, seeds its elemObjects,
  re-evaluates its property defaults and binds them, and only then
  `build` finds the row's subtree in the cache. The safe skip is the
  subtree cache's own mechanism one level up: track declareRowScope's
  reads into a per-row read-set (keyed by the row prefix), and on a
  later frame, when that set is not stale, the row token is unchanged
  and the delegate's entry would hit, bind only the token, the prefix
  and the handler-scope entry. It must be the read-set and not a
  "would hit" peek, because the live default's re-evaluation is what
  stamps the raw ref that makes the row stale — skip it on a peek and
  a row keeps a stale default forever. Eighteen `enterRow` call sites,
  one change in `enterRow` itself; verify with the 65-page before/after
  render and `GFX_PATCH_VERIFY=all`.
- ✓ **A cache hit replays every registration below it** (2026-11-08).
  `replayRegs` was 17 % of a settled indicators frame, three quarters
  of it `stampFocus` re-fitting focus ids and reports the reused
  elements already carried; every change to those (a new activeFocus
  ref, a focus request) invalidates the cache, so a replay now stamps
  only while a request is pending. indicators 484 → 357 µs, chatui
  168 → 139, buttons 102 → 94, navdisplay 41 → 40.
  ▢ The walk itself (5.6 % now, one `elems.set` per element under a
  hit) goes away with a persistent registry: keep `elems` across frames
  with a per-key frame stamp, stamp only the entries built or patched
  this frame, and let a hit mark its entry live (O(1)); a lookup accepts
  a key whose own stamp is current or whose owning entry (each
  registration remembers it) is reachable from a live entry through
  `subs` that were not replaced — which needs `subs` to drop a replaced
  child when a patch rebuilds it, so a stale element under a patched
  parent is not found. Verify with the 65-page before/after render and
  `GFX_PATCH_VERIFY=all`; the risk is a geometry read finding an element
  no longer in the tree.
- ▢ **buttons is spread thin** (2026-10-10): 141 µs against the Go
  port's 31. The profile has no single owner — patch machinery 59 %
  (`withCtx` re-binding every ancestor's names into the flat
  `env.vars` map for the one patched slot is 15 %, map assign/access
  another 23 %), ui layout 21 %, GC 7 %. The structural fix is a
  layered scope (a chain of frames) instead of binding into one flat
  map and undoing, which is a refactor of `Env`, not a tuning.
- ▢ **`width` / `height` read where the item is not registered** stay
  untracked on purpose (`derivedGeom` in shadowSelfGeometry): a size
  evaluated at declare time, or off an element that has never been laid
  out, has no geometry token to hang a dependency on, and guessing gave
  a layer served from cache at the wrong place. The remaining reads on
  shapes and sliders are these.
- ▢ Template delegate placement for the remaining control kinds
  (`templatepopup.go` sites) as slots. No measurable target left on
  buttons; worth re-measuring against the pages above.

## Later

### gml app chrome
A whole application's chrome (tool rails, panels, menus) runs in .gml elsewhere; the markdown editor's is not
ported. File components are single-instance (doc-global props), and
there is no per-file hot reload.

### Full QML compatibility
The staged A–E roadmap for running real `.qml` unmodified. The corpus
above is the evidence; the roadmap is the plan.

## Rules that apply here
- Bounded runs only: `scripts/bounded.sh`, `scripts/qml-perf.sh`,
  foreground, short `-frames`. Two unfenced runs took the desktop down.
- Gates before a commit: `scripts/check.sh`, the warning sweep prints 0,
  and the corpus renders unchanged.
