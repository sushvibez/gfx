# QML-Examples-Collection coverage

Corpus: https://github.com/JesusRamosMembrive/QML-Examples-Collection —
a real Qt Quick application (~400 .qml files: custom Controls style,
qmldir modules, a Style singleton driving every color/size). We run its
65 `examples/*/Main.qml` pages unmodified.

**Status (2026-08-26): all 65 pages render with ZERO warnings.** Every
element, attribute, handler and expression in the corpus is either
implemented or accepted, and the four Qt modules the framework has no
backend for (QtMultimedia, QtLocation, QtQuick3D, QtQuick.Pdf) are modeled
as far as they can be without one — see items 25-28 below for exactly what
each one does and does not do. Warnings went 503 → 0 over this work; the
remaining gap to Qt is visual parity on some pages (see the side-by-side
renders), not missing features.

Rerun (clone the repo to ~/workspace/qml-examples):

```bash
cd examples
go run ./qml -file ~/workspace/qml-examples/examples/buttons/Main.qml \
    -w 900 -h 700 -screenshot /tmp/out.png
```

## Side-by-side parity renders

`scripts/qml-compare.sh` renders every page twice — real Qt (offscreen
platform, `grabToImage`, hosted like the app's Dashboard Loader with
`fullSize` set, plus a stub Qt5Compat.GraphicalEffects module so pages
load in kits without it) and our engine — and composes labeled pairs
plus a contact sheet into `build/qml-compare/`:

```bash
QT_QML=~/Qt/6.10.1/gcc_64/bin/qml scripts/qml-compare.sh          # all
scripts/qml-compare.sh buttons sliders                            # some
```

Representative half-scale pairs are committed in `qml-examples/`
(buttons, sliders, chatui, ethernet, indicators, textinputs, tabbar).
Refresh them from `build/qml-compare/pairs/` when parity moves —
downscale 2x, e.g. `Image.open(src).resize((w // 2, h // 2))`. Regrabbed
2026-08-29, after the parity pass in items 31-38; both sides render with
the app's own Controls style (see item 12).

Only 37 of the 65 Qt references carry content: the rest are pages built
on the app's C++ types, which the bare `qml` runtime cannot instantiate,
so it renders them empty. Those pages can only be read on our side.

## Automated parity scores (2026-11-08)

`scripts/qml-compare.sh` now runs in WSL (Qt 6.10.1 at `~/Qt`, installed
with aqtinstall; the corpus at `~/workspace/qml-examples`). Each page's
score is the mean absolute pixel difference between Qt's render and ours
(0–255 per channel, after resizing to the same size): a few points is
font rasterisation and antialiasing, double digits is a visible
difference. Pages whose reference is blank (built on the app's C++
types, or needing a GPU display) cannot be scored.

| score | pages |
| --- | --- |
| ≤ 1.0 | splitview 0.5, states 0.6, filedialogs 0.7, toolbar 0.7, gridview 0.7, flickable 0.8, swipeview 0.8, loaderex 0.9, shapes 0.9, network 0.9, menubar 0.9, tabbar 1.0 |
| 1.0–2.2 | pathview 1.1, scrollview 1.1, popups 1.2, rangesliders 1.3, hud 1.3, sliders 1.4, toasts 1.4, drawers 1.5, layouts 1.5, textinputs 1.6, combobox 1.6, indicators 1.7, ecam 1.7, buttons 1.7, canvas 1.8, navdisplay 1.8, lists 1.9, pfd 2.0, date 2.1 (before the fix below), chatui 2.2 |
| no reference | asynccpp, charts3d, customitem, database, ethernet, graphs, maps, multimedia, pdfreader, qmlcppbridge, settings, tableview, theorycpp, threads, treeview, websocket; multiplane (no render of ours) |

Found and fixed by it: tabbar and menubar had scored 10.5 and 9.6 — a dark
page's theme had darkened Qt's light Basic bands — now 1.0 and 0.9; date's
MonthGrid started on the 1st when the month began on a Sunday, where Qt
always shows some of the previous month. Open: on drawers, a button whose
`contentItem: Text` sets no size renders its label larger and bolder than
Qt's default font (Qt does not pass a control's font to a plain Text).

## Status (2026-07)

**65 / 65 pages render.** No banners, crashes or timeouts. Pages using
the app's C++-registered types (DatabaseManager, WebSocketClient, ...)
render their full chrome with the C++-backed data areas empty — the
same degradation Qt shows without the native plugin.

Engine features this corpus drove (all landed):
- Multi-line JS expressions: continuation across newlines after a binary
  operator, before one, and through member chains (`"a" +` \ `+ "b"` \
  `.method()`), plus newline-tolerant ternaries and call arguments.
- `function(...)` expression values (signal-handler style) and ES6
  default parameters (`function mesh(name, format = ".obj")`).
- Block bindings: `text: { if (...) return "x"; ... }`.
- Inline components: `component Foo: Item { ... }` (registered like
  `Component { id }` and instantiable as an element type).
- Bitwise `| & ^` (`Qt.AlignHCenter | Qt.AlignVCenter`).
- Forward references in property defaults (declare-then-retry).
- Element-valued property declarations parse (value kept null v1).
- **QML modules**: `import <name>` resolves a qmldir near the document
  (`<dir>`, `imports/<dir>`, `styles/<dir>` up the ancestor chain);
  `singleton Type` files load once per document and expose properties +
  functions as a shared object (`Style.bgColor`, `Style.resize(x)`);
  plain `Type 1.0 Type.qml` entries register as file components.
- Root-level `anchors.fill: parent` (means: fill the window).
- **Controls styling**: `import <style>` modules shadow the builtin
  Button and Label with the style's QML (QML import shadowing), backed
  by a minimal QtQuick.Templates layer — namespaced types (`T.Button`)
  compose their background/contentItem delegates in an anchor
  container, with the control contract (text, flat, enabled, palette,
  ...) seeded into the file's id scope per instantiation (declared
  properties re-evaluate per instance; use-site ids alias the control
  scope). Includes block-binding property defaults, color component
  access ("#rrggbb".r), import `as` aliases, dotted type names, Flow.
  Interaction states (pressed/hovered tints) are static in v1.
- Lenient mode for `.qml` documents: any element whose build fails —
  at any depth, including anchored children, delegates and attr-valued
  elements — logs and becomes an empty placeholder (Qt warns-and-
  renders; authored `.gml` stays strict). Build errors also log to
  stderr alongside the banner.
- Element-valued attributes stopped crashing: inline `model:
  ListModel { ... }` resolves (ComboBox included, honoring textRole),
  singular `states: State { ... }` works, and every attrFloat/Bool/Color
  helper rejects element values with an error instead of a nil deref.
- Off-target scissor rects clamp (render): a clip fully outside the
  render target becomes an empty in-bounds scissor, not a GPU
  validation failure (ListView content far below the fold).

## Remaining failures

- FIXED 2026-07: all former failure classes. The five "timeout" pages
  were crashes (element-valued attribute nil derefs, an off-target
  scissor rect); the eleven "banner" pages were unknown-element hard
  failures (C++ types, QtQuick3D View3D, MultiEffect, bare self-
  geometry identifiers) now skipped as placeholders by lenient mode.
- Fidelity gaps in "OK" pages: Controls styles from
  `import qmlsnippetsstyle` are ignored (builtin controls render
  instead — buttons are blue, not the theme's flat style), FontLoader
  (custom fonts null → default font), `availableWidth/Height`
  approximate to width/height.
- FIXED 2026-07: GridLayout cell sizing + card backgrounds —
  `Layout.preferredWidth/Height` (and minimum sizes) now apply as fixed
  sizes after build, so the corpus' universal card grid lays out and the
  card Rectangles paint. See the committed pairs for before/after.

## Go-registered types (qmlRegisterType equivalent)

The corpus' C++-backed types can now be implemented in Go:
`gml.RegisterType(name, factory)` — properties are live core.Property
bindings (gml.BindBool/BindString/BindFloat), methods invoke through the
element's id, and signals emitted from Go (any goroutine — emissions
queue and drain on the UI thread per frame) run the element's on<Signal>
handlers with arguments bound (function-expression and arrow forms, AND
now Connections' named-function form — see below). Runtime ListModel
mutation (append/insert/remove/clear/set/get) makes signal-driven logs
live. examples/qml/native.go implements six corpus backends:
WebSocketClient (simulated echo server), EthernetController (simulated
STANAG-4586 link — full ConnectionCard/SendMessageCard now live, ports
+ counters bound to the Go side), ThreadPipeline (simulated 3-stage
generator/filter/collector worker on a goroutine ticker — PipelineFlow/
PipelineControl cards live), TheoryParser (chapters/topics tree +
getExplanationHtml/getCodeSections, driving theorycpp's ContentPanel),
FileSystemTreeModel (os.ReadDir-backed listDir/parentDir), and
DatabaseManager (in-memory employee table — queryEmployees/
addEmployee). examples/qml/gobackend.gml is a minimal self-driving
demo; FileSystemTreeModel and DatabaseManager are proven via standalone
examples/qml/filesystem.gml and examples/qml/database.gml (ListView/
Repeater demos) since their real corpus pages (treeview, database/
tableview) bind these same Go types to TableView/TreeView, which aren't
implemented controls yet — the backend works, the card just won't
render on the corpus page itself until that control lands. Also landed
en route:
- `new Type()` expressions; Qt.formatTime/formatDateTime.
- implicitWidth/Height accepted on boxes and as geometry members.
- Connections { target: x; function onSig(...) {...} } — the named-
  function form now carries real parameters through to the handler
  (previously silently dropped), and `target` resolves through a dotted
  chain's trailing id (`target: root.pipeline` where `pipeline` is a
  sibling id), not just a bare identifier.
- Bitwise shift operators (<<, >>) and compound assignment (|=, &=,
  ^=), prefix ++i/--i in for-loop increment clauses.
- Bare self-geometry identifiers (`radius: width / 2`): a node's own
  width:/height: attribute values are now visible to its OTHER
  attributes as plain identifiers — the first corpus item from the
  "not a C++ gap" bucket to close.
- Stray `anchors.*` attributes (set on a child of a non-anchor
  container, e.g. inside a Row) are tolerated as a no-op instead of
  rejecting the element.
- `model:` accepts a plain JS array directly (object elements expose
  their keys as roles, primitives via `modelData`), not just count/
  ListModel.
- Fixed: `modelData` is now always available for array-model rows, not
  only primitive ones — an object row previously exposed just its own
  keys as roles, so a delegate written as `modelData.foo` (common in
  the corpus, e.g. theorycpp's topic Repeater) rather than the bare
  `foo` role failed to resolve. `resolveModel` (model.go) now merges
  `modelData: <the row object itself>` into every object row (QML
  semantics — `modelData` and the per-key roles coexist).
- `Number.prototype.toLocaleString()` (comma-grouped formatting).
- Fixed `resolveModel` (model.go): `model: someVar` where `someVar` is
  a declared `property var x: []` failed with "model wants a count or
  a ListModel" — `resolveAny` deliberately returns the raw `Ref` for a
  bare identifier (so `resolveRef`-style callers can get the Ref
  itself), but `resolveModel` wasn't unwrapping it before its
  `*listModel`/`[]any` type-switch. `model: obj.someVar` (member
  expression) already worked since that eval path dereferences Refs
  itself — only the bare-identifier form was broken. General fix, not
  corpus-specific (surfaced by database.gml's `model: rows`).
- TextField accepts `clip`, `textFormat`, `selectedTextColor`,
  `selectionColor`, `onTextEdited`, `onEditingFinished`, `onAccepted`
  as no-ops; Flickable/ScrollView accepts `boundsBehavior`,
  `ScrollBar.vertical`, `ScrollBar.horizontal` as no-ops (theorycpp's
  ContentPanel: a rich-text TextEdit inside a ScrollView).

## Live hover/press state + cursorShape (33-page impact)

Any box (Rectangle/Item/MouseArea/...) now has real, persistent
containsMouse/hovered/pressed state, and `cursorShape:` actually
changes the pointer — a grep of the corpus found 33 pages reading
`containsMouse`/`.hovered`/`.pressed` and 30 using `cursorShape`,
previously either erroring (cursorShape: unhandled attribute, so the
whole MouseArea was skipped by lenient mode) or silently wrong
(containsMouse/pressed simply didn't exist as members).
- `ui.DivEl` gained `OnHoverChanged`/`OnPressChanged` callbacks fired
  from `paint()` every frame with the element's live hit-tested hover/
  press state. Since the element tree is rebuilt from scratch each
  frame (GPUI-style), the callback can't hold state on the transient
  `*DivEl` itself — the gml side backs it with a persistent
  `doc.boolProp` Ref (same mechanism as Controls state exposure) and
  the Ref does the "did this actually change" dedup (`core.Property.
  store` no-ops on an unchanged value) before firing onEntered/onExited/
  onPressed/onReleased.
- `boxAttrs` (build.go) wires this for every box via a new
  `wireHoverPress` helper: exposes `id.containsMouse`, `id.hovered`,
  `id.pressed` (MouseArea and plain-box vocabulary overlap on purpose —
  QML controls also expose `.hovered`/`.pressed`), and fires
  onEntered/onExited/onPressed/onReleased handlers on the real edge.
  `cursorShape: Qt.PointingHandCursor` (and a few other Qt::CursorShape
  values, enums.go) maps to `d.Cursor(...)`; unmapped shapes (the
  engine only has Default/Pointer/Text) fall back to the default arrow.
- `templates.go`'s `buildTemplateControl` wires the same mechanism for
  the QtQuick.Templates layer (imported Controls styles), replacing the
  static `pressed`/`hovered` false the control contract seeded — a
  style's background/contentItem delegates that tint on `root.pressed`/
  `root.hovered` now actually animate instead of always rendering the
  unpressed/unhovered look.
- `ui.DivEl.hit()` previously only intercepted a hit for `d.click != nil
  || d.focusable`, so a MouseArea with only `hoverEnabled`/
  `cursorShape`/`onEntered` (no `onClicked`) was invisible to hit-
  testing — hover state would update (paint() traverses unconditionally)
  but `Cursor()` (which hit-tests) could never find it. Fixed: `hit()`
  also intercepts when `hoverFn`/`pressFn` is set or a non-default
  cursor is set.
- Fixed a real bug this surfaced: `loadSource`'s (and `Live.reload`'s)
  `defer d.doc.retryPendingProps()` ran a *final* pass — logging and
  discarding anything still unresolved — immediately after
  `declareProps`, before the document's first `build()` ever runs. A
  forward reference to a control's own live state (`ma.containsMouse`,
  and previously `msgIdSpin.value`-style refs too, coincidentally) can
  only resolve once that control has actually been built, so this
  always failed and permanently dropped the default — starving
  `Document.View()`'s own post-build retry (which checks
  `len(pendingProps) > 0`) of anything to do, since the list was
  already drained to nil. Split into `retryPendingPropsSoft` (resolves
  what it can, leaves the rest queued, no logging) used by the two
  pre-build call sites, and `retryPendingProps` (soft pass + final
  log-and-discard) used only by `View()`'s post-build retry and the
  singleton loader (module.go — no build ever follows a singleton's
  property declaration, so its retry is correctly final).

## Controls state exposure

CheckBox/Switch/RadioButton/Slider/SpinBox/ComboBox now always have
live, persistent state of their own — a real QML control's intrinsic
Q_PROPERTY, whether or not the app binds it externally:
- `boolBinding`/`floatBinding`/`intBinding` (build.go) fall back to
  `doc.boolProp`/`floatProp` — `intProp`'s existing bool/real
  counterparts — when the attribute is a plain literal (`checked:
  false`) rather than a bound property, and expose the result as
  `id.attr` via `exposeIDProp` so other bindings (`msgIdSpin.value`,
  `fieldCheck.checked`) read it. This made the ethernet page's dynamic
  field-editing form (checkboxes + SpinBoxes generated from
  `fieldDefinitions`) actually interactive and readable.
- Fixed a real ordering bug this surfaced: `buildFileComponent` was
  calling `doc.retryPendingProps()` immediately after `declareProps`,
  i.e. BEFORE the component's own children (the sibling element a
  forward reference points at, e.g. `readonly property int listenPort:
  listenPortSpin.value`) had been built — so the retry always failed
  and permanently dropped the pending default. That premature call is
  gone; `Document.View()` now retries once, synchronously, right after
  the whole tree's first build.
- Added: Switch/ComboBox onToggled/onActivated signal handlers,
  Slider.from/to/stepSize (accepted; Slider is 0..1 internally, ranged
  display is still a TODO), scale/opacity/visible accepted as no-ops
  on CheckBox/Switch/RadioButton/SpinBox, bare width/height/flow
  accepted on GridLayout.
- EthernetController (Go backend) gained `getFieldDefinitions()`,
  completing the SendMessageCard's dynamic form end to end.

Ethernet is now the corpus' best-fidelity Go-backend page: every card
(Connection, Send Message with its live field checklist, Protocol Log,
Hex Dump) renders and is interactive. Only MultiEffect (unimplemented)
is skipped on that page.

## Per-instance delegate scope (LANDED)

The long-deferred architectural gap is closed: a Repeater/ListView/
GridView/PathView delegate is now a real per-row component instance,
matching QML's semantics, instead of one shared doc-global template.
theorycpp's ChapterPanel accordion (the motivating failure —
`chapterDelegate.expanded`, `chapterMouse.containsMouse`,
`property var filteredTopics: { ...modelData... }`) now renders and
expands per chapter. How it works (rowscope.go):
- `declareProps` no longer descends into a delegate host's subtree
  (its ids/properties were never document-global in QML; ListModel
  children remain document data). Instead `enterRow` runs per row at
  build time: every state key gets a `<viewKey>#<row>/` prefix
  (`doc.key(n)`, threaded through hover/press refs, boolProp/floatProp/
  intProp, behaviors, elems geometry, list/grid scroll state,
  StackView/SwipeView state — all the elemKey call sites), and a
  two-phase per-row declaration pass binds (1) per-row elemObjects for
  every id with hover/press state pre-seeded, so an EARLIER sibling's
  binding can forward-reference a LATER MouseArea, then (2) per-row
  property refs, with `required property` declarations aliasing the
  row's model context (modelData/index/roles) and everything else
  holding real per-row state whose default evaluates once per row with
  the row in scope. Row state persists in docState maps across frames
  and hot reloads, exactly like document-level declared properties.
- Handlers built inside a delegate fire frames later, long after the
  build-time env row bindings were restored — `doc.handlerRunner`
  snapshots the active row-scope stack (nested delegates included) and
  re-binds it around the handler's statements, so
  `onClicked: picked = modelData.name` works per row.
- View attached properties (`delegate.ListView.isCurrentItem`,
  `GridView.isCurrentItem`) are static-false stubs; ListView/GridView
  `currentIndex` is a persistent settable ref (delegates assign it) and
  `count`/`cellWidth`/`cellHeight` are exposed on the view's id.
- Also landed en route (all found by re-running the corpus against the
  new scoping): JS `switch` statements (case/default, fall-through —
  SectionsCard's category colors), `\uXXXX` string escapes and
  single-quoted strings (lexer), `Qt.color()`, `modelData` merged into
  object-array rows, and a batch of accepted-no-op attributes
  (ListView highlight/section.*/verticalLayoutDirection, GridView
  highlight/currentIndex, MouseArea onPositionChanged/onDoubleClicked/
  onPressAndHold/drag.*, Repeater delegate:/width/height).
- ListModel runtime mutations (append/insert/remove/clear/set) bump
  the revision (`core.BumpRevision`) so mutation-only changes — rows
  are plain data outside the property system — invalidate the frame
  memo and re-render.

## TableView + TreeView (LANDED)

The two data-view controls exist now (tableview.go / treeview.go):
- **TableModel/TableModelColumn** (Qt.labs.qmlmodels): declarative
  columns + `rows:` array, registered like ListModel; runtime
  appendRow/removeRow/setRow/getRow bump the revision.
- **TableView**: one delegate instance per cell (row-major), injecting
  row/column/display; fixed column widths from `columnWidthProvider`
  (a JS function, called per column); selectionModel/editTriggers
  accepted as no-ops (selection rendering + editing are TODO).
- **HorizontalHeaderView**: delegate per column of its `syncView`,
  at the synced table's widths — resolves through a declared-TableView
  node registry so header-before-table declaration order works.
- **TreeView**: nested tree data ({roles..., children: []}, or an
  object exposing that as `.tree` — how the Go backends serve it)
  flattened to visible rows like Qt's TreeModelAdaptor, injecting
  treeView/isTreeNode/expanded/hasChildren/depth/row/column/current/
  selected. Expand state persists per node path (boolProp); the view
  id exposes toggleExpanded/expand/collapse/index plus a
  selectionModel stub (setCurrentIndex selects), so the corpus'
  delegate TapHandlers work unmodified.
- **TapHandler/HoverHandler**: Input Handlers hoist onto their parent
  box (like the existing bare-MouseArea hoist) with onTapped→click
  mapping; DivEl hover/press callbacks now chain instead of replace.
- Go backends: FileSystemTreeModel gained `tree` (real os.ReadDir
  hierarchy, capped) + `totalNodes`; new OrganizationTreeModel (static
  org chart with name/title/department roles).
- treeview page: all three cards render (file system tree with real
  directories, org chart with department badges, dynamic tree).
  tableview page: BasicTableCard renders fully — headers, 10 rows,
  alternating colors (the Qt reference renders BLACK here: it needs
  the C++ plugin offscreen). Sort&Filter/Editable cards are gated on
  EmployeeModel/EmployeeProxyModel backends (already on the backlog).
- General JS fixes surfaced by these pages: `a || b`/`a && b` now
  return the VALUE (JS semantics — `widths[col] || 120` was yielding
  `true`); toBool treats null as falsy and objects/arrays as truthy;
  out-of-range array/string indexing yields undefined instead of an
  error; file components' `function` declarations now register (and
  attach to the root id, so `root.deptColor(...)` resolves) — the
  load-time declareFuncs never saw those separate trees; module.go's
  singleton scratch is a fully-initialized docState (a bare literal
  had nil registries that newer declareProps writes crashed on).

PdfDocument still needs an actual PDF rendering library (out of scope
for "register a type" — skipping degrades exactly like Qt without the
plugin).

## Next steps (impact order)

1. DONE: live containsMouse/hovered/pressed + cursorShape.
2. DONE: per-instance delegate scope — theorycpp's accordion, lists'
   contact/color/sections cards, every modelData-reading handler.
3. DONE: TableView/TreeView controls + TapHandler (see above).
4. DONE: EmployeeModel/EmployeeProxyModel Go backends + object-backed
   TableView. tableSource interface (tableview.go): TableModel, or any
   object (NativeObject/elemObject) with props "table" ([][]display
   values — proxies serve the sorted/filtered VIEW here) + "headers".
   Backends share an employeeTable store (examples/qml/native.go);
   proxy: filterText is a BindString whose Changed re-derives, toggleSort
   flips column/order, sortColumn/currentSortOrder live. Also landed en
   route: DelegateChooser/DelegateChoice (per-cell column/row criteria,
   criterion-less fallback), TableView.* attached attrs stripped
   (editDelegate no-op), CheckBox onToggled (ui.Checkbox onToggle
   callbacks), Number/String/Boolean/parseInt/parseFloat globals,
   TableView scroll grows when no explicit height. Both corpus cards
   render with live data (cell editing landed later — item 14).
5. DONE: Selection rendering. ListView/GridView: currentIndex attr
   seeds the persistent ref; delegates read LIVE ListView.isCurrentItem
   (bare AND dl.-qualified — rowscope binds the attached map over the
   enum namespaces per row); highlight: component layers behind the
   current row/cell (AnchorMode + Anchored Fill:"parent"). TableView:
   selectionModel (named or inline ItemSelectionModel) enables
   tap-selects-row; delegates get live selected/current; the selection
   object exposes hasSelection/selectedIndexes[i].row/clearSelection().
   Row selection drops when the model shrinks under it.
6. DONE: Ranged Slider — ui.SliderEl.Range(from,to) (property holds
   ranged values, track shows the fraction; UI drag remaps), plus
   Slider onMoved (fires on user changes only, `value` bound in scope).
7. DONE: FontLoader — both forms (child element with id; `property
   FontLoader x: FontLoader{}` — the parser now KEEPS element-valued
   property defaults as elemExpr, materialized by declareProps;
   unmodeled element types still hold null). Loads via the qrc-tree
   search (absolute "/assets/..." resolve against ancestor dirs +
   imports/, now shared with images/icons), registers the face
   process-globally (render.RegisterFontBytes; NamedFontStyled falls
   back to it), name from the sfnt name table (render.FontFamilyName).
   DONE: Button/ToolButton icon.source + icon.name (SVG tinted via
   icon.color, raster via image cell; icon left of label; zero sizes
   from pre-layout Style.resize guarded). Qt.resolvedUrl added;
   palette.* accepted. **SVG viewBox fix (render/svg.go): a declared
   viewBox now defines the coordinate space AND origin (offX/offY) —
   Google-Material icons (viewBox="0 -960 960 960") previously drew
   giant and displaced whenever width/height attrs were also present.**
8. DONE: bare self-geometry identifiers — `radius: width / 2` shadows
   the element's previous-frame bounds into scope (in-place bind with
   restore, so subtree-cache env identity is preserved), zero on frame
   one, settling via the geometry signature.

9. DONE: soft-failure sweep (2026-08-24). All 65 pages rendered, then
   every logged "element skipped" was triaged by frequency: **568 -> 372
   unique warnings**, no page lost. What landed:
   - **Controls bound to a document property worked at all**: a declared
     property is wrapped in `trackedRef` (imperative-write tracking), and
     CheckBox/Slider type-asserted the bare `propRef[T]` — so
     `value: root.blurValue` dropped the whole Slider (30 sites). `propOf`
     now digs to the concrete ref and reports the property whose binding a
     user drag breaks (the widget writes the core.Property directly, so it
     marks the break in its interaction callback).
   - Control state is exposed as `id.value` / `id.checked` in BOTH cases
     (bound or literal), and is seeded at DECLARE time — per document and
     per component instance — so `Label { text: "A: " + sl.value }` above
     the Slider resolves on frame one. `Slider { }` with no value, and
     `value: someIntProp` (mirrored through the slider's own real state,
     written back on drag), both build now.
   - **Per-instance property bindings are live**: a file component's
     declared properties re-evaluate every build unless imperatively
     assigned (trackedRef per instance key), and a use-site binding is
     evaluated in the USE SITE's scope — `AnalogClock { active:
     root.fullSize }` means the enclosing document's root even though the
     component file has its own `id: root` (18 sites). A use-site id also
     aliases the instance's scope object, so `ctl.pitch` reads that
     instance. Forward-referencing defaults retry once the scope is built.
   - `parent.<prop>`: the `parent` scope object now carries the container's
     declared properties, not just its geometry.
   - **QtQuick.Shapes** (shape.go): `Shape` / `ShapePath` with
     PathMove/Line/Quad/Cubic/Arc (SVG endpoint-parameterized elliptical
     arcs)/AngleArc/Svg/Polyline, strokeColor/strokeWidth/fillColor/
     capStyle, and fillGradient (Linear; Radial/Conical keep their end
     colors along the item diagonal). Shapes paint unclipped (as in Qt);
     a Canvas now clips to its item, which is what Qt's backing texture
     does — and stops an overrunning path bleeding over the UI.
   - Canvas context2D: full transform stack (translate/rotate/scale/
     transform/setTransform/resetTransform, points and stroke widths mapped
     through it), reset(), textBaseline, lineCap/lineJoin, globalAlpha,
     ellipse(), Qt's roundedRect(), clip()/setLineDash()/shadow* as
     documented no-ops. Plus `requestPaint()` (bare and qualified),
     String.padStart/padEnd, Text `opacity`, container `orientation`
     (SplitView), negative computed sizes clamped at zero, and the
     remaining Input Handlers (Wheel/Drag/Pinch/Point) accepted as
     non-visual attachments.

10. DONE: settling + second triage round (same day). **568 -> 340
   unique warnings.** Landed:
   - **Headless screenshots now settle.** `shell.Screenshot` rendered ONE
     frame, but a binding that reads its own item's geometry only sees
     real bounds after the frame that laid them out — so geometry-driven
     markup shot blank or half-placed. It now re-renders while property
     revisions keep moving (cap 8), which is what an interactive window
     does anyway. 35 of the 65 pages changed for the better.
   - Bare self-geometry works in **property defaults** too, not just
     attributes (`property real bSize: Math.min(width, height - 20)`) —
     shadowSelfGeometry is now shared by buildFresh, declareProps,
     rebindProps and the per-instance scope pass. Negative computed sizes
     clamp at zero. shapes/BezierCurvesCard renders correctly now.
   - **A forward-referencing property default froze its binding forever**:
     retryPendingPropsSoft wrote the resolved value through the TRACKED
     ref, which marks the binding broken (that is what an imperative
     assignment does). It writes through the raw ref now, so the binding
     keeps following its inputs.
   - `state` is live item state: a persistent string property, readable
     AND writable as `id.state` (`stateBox.state = "expanded"`), with
     `state: <expr>` as its binding until an assignment breaks it.
     Transitions accept Color/Property/Smoothed/Spring animations and
     grouping nodes instead of NumberAnimation only.
   - Text fields expose their live state from DECLARE time (id object
     carries the ui.InputState), so `field.text`, `field.length` and
     `field.activeFocus` resolve from siblings and property defaults —
     and `activeFocus` is now real: ui tracks focus on InputState
     (`HasFocus`, maintained through Context.setFocus).
   - JS/parse: **regex literals** in value position (`validator:
     /[0-9]*/`, with flags), `/* block comments */`, and an assignment
     whose value starts on the next line (`root.summary =` \ `a + b`).
     These three unblocked whole file components (PinInput,
     FormBuilderCard, TextFieldCard).
   - `SplitView.*` attached properties share the `Layout.*` path
     (preferredWidth/fillWidth/... actually applied); GridView's own
     attributes are readable bare (`cellHeight: cellWidth`); Canvas
     gained createRadialGradient; geometry/focus change handlers
     (onXChanged, onWidthChanged, ...) are accepted no-ops; Slider takes
     implicitWidth; TextField takes height/implicit sizes.

11. DONE: third round (**340 -> 326 warnings**):
   - **Group-property blocks** — `anchors { fill: parent }`, `font { bold:
     true }` — fold into dotted attributes at PARSE time (a child element
     whose type name starts lowercase, which no QML type does), so the
     parent's anchor pass sees them like the flat form.
   - Conditional anchors: `anchors.left: sent ? undefined : parent.left`
     picks a branch, and undefined/null means "this edge is not anchored"
     rather than an error.
   - Model and view state readable through ids: `listModel.count`,
     and a Flickable/ScrollView id carries its scroll state
     (`contentX/contentY/contentWidth/contentHeight`, `visibleArea.
     yPosition|heightRatio|xPosition|widthRatio`, `atYBeginning/atYEnd`).
     Scroll state is keyed per instance now (row prefix), like list state.
   - `id.visible` is published from the resolved value each build, so a
     sibling can bind against it (`anchors.leftMargin: avatar.visible ?
     6 : 0`).
   - Flickable's flick-physics attributes and DragHandler axis limits
     (xAxis/yAxis minimum/maximum) are accepted no-ops.

12. DONE: **Controls styling** — the long-standing #1 item (**329 -> 233
   real warnings**). The missing piece was the corpus's actual styling
   mechanism: `qtquickcontrols2.conf` sets `Style=qmlsnippetsstyle`,
   which restyles EVERY Controls type in every file. We read that conf
   now (walking up from the document dir), while a module a FILE imports
   shadows builtin types only inside that file — real QML import scoping
   (before, any loaded module shadowed types everywhere, so a page's
   style import restyled the component files it instantiated).
   - Styled kinds: Button, Label, CheckBox, Switch, RadioButton,
     ToolButton. The Templates layer composes indicator and handle
     alongside background/contentItem, a delegate that positions itself
     nowhere fills the control (Qt resizes those), and delegates are
     declared in a per-instantiation scope so cross-delegate geometry
     (`width: background.width / 2`) resolves. Slider/SpinBox/ComboBox/
     TextField/Dial/Tumbler keep the builtins — their styles need drag
     and editing routing the composed box cannot do yet.
   - Contract: a page-bound contract value keeps its Ref, so a styled
     CheckBox writes `checked` back; checked/value fall back to
     per-instance persistent state; position/visualPosition derive from
     value/from/to; icon.* and RangeSlider first/second resolve;
     Qt.Checked/PartiallyChecked/Unchecked exist.
   - A container's `background:` delegate paints now (ToolBar, Pane,
     Frame, ... take its color/radius/border/opacity) — this is what
     makes the corpus's toolbars dark instead of light grey.
   - Attached properties we ignore are stripped centrally (ToolTip.*,
     Accessible.*, ScrollBar.*, ...) instead of listed per builder;
     ComboBox exposes currentText/displayText/count.
   - **scripts/qml-compare.sh now renders the reference with the app's
     own style** (QT_QUICK_CONTROLS_STYLE from the conf — whose CRLF
     endings Qt otherwise takes as part of the style name). The
     committed refs before this were Basic-style, i.e. a misleading
     ground truth; re-grab the pairs after this change.

13. DONE: styled Slider + a real Dial.
   - `ui.SliderEl.Bare()` paints nothing but still handles press and drag,
     so a styled Slider draws through its groove/handle delegates with a
     bare slider overlaid for the interaction — the first
     interaction-heavy kind routed into a composed control. Horizontal
     and vertical sliders now match the reference.
   - The builtin Dial was a placeholder ring that ignored `value`. It
     draws for real now: a -140°..140° track, the value as a progress
     arc, and a handle dot (rotary dragging still not modeled). Shadowing
     Dial with the corpus style was tried and reverted — the style's
     readonly geometry properties (`_arcRadius`, `_progressAngle`)
     evaluate once per instantiation, before its canvas has real bounds,
     so it draws a bare ring with no ticks or readout.

14. DONE: **TableView cell editing** (`TableView.editDelegate`), the
   other long-standing item. A cell's edit delegate builds over the cell
   when that cell is the edit target, with QML's editing contract: the
   cell delegate sees `editing` (so its display Label hides), the editor
   gets a writable `edit` property holding the pending value, and
   `TableView.commit()` writes it into the model and closes the editor
   (`TableView.closeEditor()` just closes). Declarative TableModels take
   the write (new tableWriter interface); object-backed models don't —
   their rows live behind a Go backend's own API. QML's default trigger is
   a double tap; with single clicks only, a click on the cell that is
   already current opens the editor — the same two-step gesture. An edit
   target past the end of a shrunken model closes itself.
   Test: TestTableEditDelegate.

15. DONE: styled TextField/TextArea + **per-axis Layout.fillWidth /
   fillHeight**, which turned out to be the bigger fix.
   - A styled text field composes its background delegate and overlays the
     real input element (transparent, so the style's frame shows): the
     style draws the frame, the input owns text, caret, selection and keys,
     and `activeFocus`/`hovered` reach the delegate so its focus border
     animates. The markup's `text:` seeds the field once.
   - **Layout.fillWidth/fillHeight were conflated into one "grow" flag**,
     so `Layout.fillWidth` inside a ColumnLayout grew the item along the
     COLUMN — splitting the column between its rows and squashing them
     (which is why styled text fields first rendered as slivers). Now the
     hint lands per axis: grow on the enclosing layout's main axis, drop
     the fixed size on the cross axis so the layout's stretch pass fills
     it. The direction comes from the box that is actually building the
     children (`DivEl.IsRow`), so `SplitView { orientation: ... }` is
     right too; a grid's cells fill both axes, since a grid sizes them
     itself. 61 of 65 pages changed; layouts, splitview and textinputs now
     track the reference closely. Test: TestLayoutFillAxes.

16. DONE: styled ComboBox, with its popup. The style's background,
   indicator and contentItem compose as usual; the control resolves its
   own model (array / ListModel / count) and publishes
   displayText/currentText/currentValue/count for the delegates and the
   page, and a click opens a floating list of the model — the style's own
   `delegate` per row when it declares one (with modelData/index/
   highlighted in scope), otherwise a plain labeled row. Choosing a row
   sets currentIndex, clicking outside dismisses. The builtin ComboBox
   gained the same read-only properties, currentValue included (a row's
   valueRole field). Test: TestComboBoxModelValues.
   En route: an element-valued attribute may name a NAMESPACED type
   (`delegate: T.ItemDelegate { ... }`) — the parser only accepted a bare
   one, which failed the whole style file; `font: root.font` (a whole-font
   object) is read for the parts we model; and a text delegate that fills
   its control gets its leftPadding/vertical centering applied by the
   composition, since a text leaf cannot position itself in a rect it was
   handed.

17. DONE: the JS/QML syntax behind most remaining component failures
   (**221 -> 179 real warnings**). 32 of the corpus's warnings were parse
   errors, from only four causes; three are fixed and the fourth is
   isolated:
   - `var l = [], t = [], p = []` — several declarators in one statement
     (23 sites, each killing a whole component file). They desugar to a
     seqStmt, which runs them in the CURRENT scope so the names stay
     visible.
   - a property whose value starts on the NEXT line (`readonly property
     color c:` then the ternary underneath) — 6 sites.
   - `typeof x === "object"` (plus unary `+`/`~`): typeof evaluates its
     operand itself, returning "undefined" for an unbound name instead of
     failing, like JS.
   - QML 6 type annotations on functions: `function pad2(n: int) : string`.
   - assignment-as-expression: `a = b`, `a += b` and `a++` are
     EXPRESSIONS in JS, not statements — `{ id: root.nextId++ }` inside an
     object literal, `1 - (t2 -= 1.5/d1) * 2` inside arithmetic. They
     parse as an updateExpr (assignment binds loosest, right associative;
     postfix yields the pre-write value) and the statement form is just an
     expression statement now. The formatter prints them, and
     `var a = 1, b = 2` too.
   **Every .qml file in the corpus now parses — zero parse errors across
   the 65 pages.** Tests: TestJSParserGaps, TestAssignmentExpressions.

18. DONE: **the 3D chart types** — Bars3D, Scatter3D and Surface3D, drawn
    as an isometric projection on the 2D renderer (`charts3d.go`): a floor
    grid, extruded bars shaded per face and depth-sorted back to front,
    depth-ordered scatter dots, and the surface as a row/column wireframe.
    `cameraPreset` picks the projection's handedness; themes, shadows,
    orbit/zoom and selection are accepted and not modeled.
    `ItemModelBarDataProxy` / `ItemModelScatterDataProxy` /
    `ItemModelSurfaceDataProxy` resolve their `itemModel` through the same
    path views use, mapping rowRole/columnRole (categorical, indexed in
    first-seen order) and valueRole / xPosRole / yPosRole / zPosRole. The
    QtGraphs enum namespaces (`Graphs3D.CameraPreset.*`,
    `Graphs3D.ShadowQuality.*`, `GraphsTheme.*`, the series mesh and
    drawMode flags) now evaluate.

    Three general gaps this closed:

    - A proxy's model is read at PAINT time, not build time: these pages
      fill their ListModels from `Component.onCompleted` and rebuild them
      from handlers (the resolution slider), so a value captured at build
      time is always one generation stale.
    - **A ListModel declared inside a file component lost every row a
      handler appended.** Re-declaring the component re-created the
      `listModel` object, so `m.append()` from `onCompleted` went to an
      instance the next frame threw away — the model read as empty
      forever. Models are now retained per `rowPrefix + id`, and declared
      `ListElement` rows re-seed only when the declarations themselves
      changed (`sameRows`), so runtime rows survive a rebuild and a hot
      reload still picks up edited data.
    - `Math` was missing `log` (and `exp`, `log2`, `log10`, `atan2`,
      `hypot`, `sign`, the hyperbolics, `cbrt`, `trunc` and the `LN2`-style
      constants). One missing function killed a whole page: the scatter
      page's Box-Muller `randNormal` failed, so both its series stayed
      empty.

    Corpus warnings: 87 → 82 unique. Tests: TestChart3DProxies,
    TestListModelSurvivesRebuild.

19. DONE: **the date page** — Tumbler, MonthGrid and DayOfWeekRow, and the
    two scope bugs behind them (`tumbler.go`, `calendar.go`).

    Tumbler draws the visible window directly (visibleItemCount slots
    centred on currentIndex, wrapping when `wrap` says so) instead of
    modelling the flickable wheel; selection is a click or the arrow keys,
    not a spin. Its delegates get the attached `Tumbler.displacement` they
    scale font size, colour and opacity by, and an integer model gives the
    delegate the index as its modelData. currentIndex is ordinary control
    state (seedControlState), so `selectedDay: dayTumbler.currentIndex + 1`
    resolves before the wheel builds and `setDay()` writes it back.

    MonthGrid generates six weeks of day cells from the Sunday of the week
    containing the 1st — adjacent-month days carry their own month, and the
    cell for today carries `model.today` — and DayOfWeekRow the seven
    weekday names. The locale is accepted, not modeled (weeks start Sunday,
    names are English).

    Two general bugs this closed, both scope leaks out of a component file:

    - **A file component's functions closed over the pre-instance scope.**
      A setter called later (`picker.goToToday()` from the calendar's
      signal, or the component's own `Component.onCompleted`) wrote the
      doc-global slot, not the instance's, so the write vanished — the
      picker sat on index 0 forever. Functions are now re-closed over a
      snapshot of the instance scope (`declareFuncsScoped`).
    - **A use-site attribute was evaluated inside the component's scope.**
      Only declared-property overrides got the use-site scope; a plain
      attribute did not — so `Label { text: "" + root.selectedYear }`
      resolved `root` to the STYLE file's own `id: root` and the binding
      silently produced nothing. Every styled control with an expression
      for text/colour/size was blank. Plain values now freeze in the
      use-site scope (`useSiteAttr`); object-valued attributes
      (`anchors.fill: parent`) and unresolvable ones keep their expression
      for the instance scope, as before.

    `parent.<prop>` also reads a delegate's `required property` filled from
    the model row, not only its Refs — DayOfWeekRow's delegate reads
    `parent.shortName`.

    Corpus warnings: 82 → 71 unique. The date page renders with none.
    Tests: TestTumblerWindow, TestTumblerState, TestMonthCells,
    TestDayOfWeekCells, TestUseSiteAttrFreeze.

20. DONE: forward references in component/delegate property defaults stopped
    warning falsely. A default naming an element declared LATER in the
    document (`HudCanvas { pitch: controls.pitch }` with the controls panel
    below it) cannot resolve on the first frame but resolves on the next —
    the instance scope re-evaluates it every frame, and the pages rendered
    correctly all along. Those defaults now queue for the document's retry
    pass and only report after failing three frames running, so a real typo
    still surfaces. A child's default can also read the item it sits in
    (`property real prog: parent.progress`): `parent` is bound during the
    declaration walk, over the parent whose own properties were declared
    one step earlier. Corpus warnings: 71 → 56 unique.

21. DONE: five general gaps behind the swipeview, indicators, diorama and
    switches pages (56 → 42 unique warnings):

    - **Nested function declarations.** `function drawPine(x, y) { ... }`
      inside another function's body — JS's function statement, which these
      canvas painters use to factor a repeated shape — parsed as nothing.
      It now binds a userFunc in the scope it runs in (`funcDeclStmt`), and
      the formatter prints it.
    - **A Repeater inside a navigation container** (SwipeView /
      StackLayout / StackView) expands into one page per model row, built in
      that row's scope. Five wizard steps declared as a Repeater used to
      collapse to one page.
    - **PageIndicator** is a real control: one delegate (or a built-in dot)
      per page with `index` in scope, `interactive` dots that page the view,
      and count/currentIndex on its id. SwipeView and friends now expose
      `count` and `depth` too, which is what an indicator binds to.
    - **`parent.<attr>`** reads a plain visual attribute of the item a child
      sits in (`radius: parent.radius`) by evaluating the parent's markup —
      geometry still comes from the live layout, which is why the untracked
      geometry read-set is unchanged — and **`someId.parent.<prop>`** works
      too: every id'd element records where it sits, document-globally and
      inside a component instance.
    - A Repeater's id exposes its `count`.

    Tests: TestNestedFunctionDeclaration, TestParentAttrAndParentOfID,
    TestSwipeViewRepeaterPages, TestPageIndicatorDots.

22. DONE: the Flickable / state / ComboBox contracts (42 → 33 unique
    warnings):

    - **A Flickable's scroll properties in its own scope**, bare, the way
      QML reads them (`property int currentPage: Math.round(contentX /
      width)` declared on the Flickable itself), plus the rest of the
      contract on its id: atX/atY begin/end, originX/originY, and the
      "is it moving" family (movingVertically & co, which read false —
      scrolling is immediate here rather than flicked).
    - **contentX / contentY are assignable** (`flick.contentX = 0`), and
      ListView has its imperative view API: positionViewAtEnd /
      positionViewAtBeginning / positionViewAtIndex (a scroll-offset write,
      since rows are uniform height here), with forceLayout,
      returnToBounds and friends as no-ops. They resolve bare inside the
      view's own handlers too, which is where QML calls them.
    - **Qt 6's qualified PropertyChanges** — `PropertyChanges { morphRect.
      width: 100; morphRect.radius: 0 }`, no `target:` line — including
      several targets in one block. Group properties (font., anchors.) and
      attached ones are not mistaken for targets. An item's own states may
      target the item itself, which is the usual shape for a single
      morphing rectangle, and that merge now happens before its attributes
      are applied.
    - **A ComboBox's read-only properties are computed on demand** from its
      model, roles and index, so a card that sizes or colours a preview
      from `sizeCombo.currentValue` declared ABOVE the box resolves on the
      first frame — with the real type, which a placeholder value could not
      give (a colour role is not a number).
    - The item's own `visible` reads bare (`opacity: visible ? 1 : 0`).

    Tests: TestFlickableScrollContract, TestQualifiedPropertyChanges,
    TestComboBoxDerivedBeforeBuild.

23. DONE: the long tail — 33 → 18 unique warnings, and 47 of the 65 pages
    now render with none:

    - **A host-registered object declared inside a component file owns its
      id.** The per-instance scope was forking a plain element object over
      it, so every settings card's `SettingsManager { id: settings }` lost
      its methods (`settings.allKeys()` from its own onCompleted).
    - **JS truthiness for engine objects.** An element id, a model, a
      function is an OBJECT, and an object is truthy — `visible: someItem`
      failed the binding and skipped the element instead.
    - **toString() / valueOf() answer on every value**, not only numbers and
      strings (`getValue(key, "").toString()` where the stored value is a
      bool or missing).
    - **Qt.callLater(fn, args…)** defers to the posted queue, which drains at
      the top of the next frame — "after everything settles", which is what
      the callers want (syncing a highlight to geometry layout just made).
    - **Qt::Edge** (a Drawer's `edge: Qt.LeftEdge`), and **Qt.rgba clamps a
      NaN component to 0** as Qt does — a component computed from widths
      before the first layout produced a "bad color" and skipped the panel.
    - Enter on a text field is its accept however it is spelled
      (`Keys.onReturnPressed`, `Keys.onEnterPressed`); a MouseArea's
      `onContainsMouseChanged` runs on both hover transitions; a view's
      `onAtYEndChanged` / `onAtYBeginningChanged` run when the edge flag
      actually flips (the infinite-scroll hook), tracked frame to frame.
    - **BorderImage** renders as its stretched image (the 9-patch insets and
      tile modes are accepted, not modeled), AnimatedImage as its first
      frame; **PathAttribute / PathPercent** are accepted path segments;
      ShaderEffectSource accepts transform lists; DragHandler's
      onTranslationChanged is an accepted no-op.

    Tests: TestNativeIDInsideComponent, TestObjectTruthinessAndToString,
    TestQtCallLater.

Remaining warnings are the genuinely deep ones: QtMultimedia
(MediaPlayer/VideoOutput/CaptureSession/MediaDevices — no decoder without
cgo), QtQuick3D's View3D, QtLocation's Map plus QtPositioning, the PDF
pair, and three on the database page.

24. DONE: try/catch/throw and the item scope in handlers — 18 → 12 unique
    warnings, 53 of 65 pages clean:

    - **try / catch / finally and throw** were missing from the JS layer, so
      any handler that guarded a parse or a request died at the keyword. A
      runtime fault reaches the catch clause as an Error-shaped object
      (`e.message`), which is what these pages read.
    - **A handler resolves bare names against its own element first** (QML's
      item scope), whether or not that element has an id: a ComboBox reading
      `currentValue` in its onCompleted, a TextEdit passing `text` to a
      parse function. Control state binds live, derived values through a
      read-only ref that reads when the handler runs.
    - **`field.text = "..."` is assignable** through the field's id, as are
      `flick.contentX/Y`.
    - **`model: null`** (a var property the backend has not filled) shows
      nothing instead of failing the view.
    - A C++ type exposes the same value as a property on one page and an
      invokable on another (QSqlQueryModel's `rowCount` vs
      `columnCount()`), so a **no-argument call on a plain value yields the
      value**.
    - A native object's initial property that names an element declared
      LATER is retried on later frames instead of being reported on the
      first one.

    Tests: TestTryCatchFinally, TestPropertyOrInvokable, TestNullModelIsEmpty.

The 12 warnings left are all missing Qt modules: QtMultimedia
(MediaPlayer / VideoOutput / CaptureSession / MediaDevices — no decoder
without cgo), QtQuick3D's View3D, QtLocation's Map with QtPositioning, and
the PDF pair.

25. DONE: **QtQuick3D's View3D** — a 3D scene declared in markup, rendered
    into an offscreen target the 2D layout embeds like any other element
    (`qtquick3d.go`). PerspectiveCamera / OrthographicCamera (position,
    eulerRotation, fieldOfView, clip planes), DirectionalLight (direction,
    colour, ambient, brightness), Model with the built-in meshes (#Cube,
    #Sphere, #Cylinder, #Cone, #Plane — carrying the scale that makes Qt's
    100-unit primitives measure the same here), DefaultMaterial /
    PrincipledMaterial colour, and Node nesting whose transforms compose.
    SceneEnvironment contributes its clearColor; shadows, IBL, instancing
    and mesh FILES are not modeled (a mesh file stands in as a cube).

    The scene is re-read from markup every frame, so bindings are live, and
    a transform component an animation targets is an engine property the
    ordinary animation machinery writes — `NumberAnimation on
    eulerRotation.y` works because the animator resolves that exact name.

    The host wires it once: `gml.SetView3DGPU(device, queue, r2, format)` in
    Init and `gml.EncodeView3D(enc)` from the Encode3D hook. The qml example
    installs both only when the document declares a View3D
    (`live.DeclaresType`), since the 3D hook also turns off the 2D MSAA
    path. Without a GPU context a View3D lays out as an empty box of the
    right size, so tests and null hosts are unaffected.

    Also added: `Qt.vector2d/3d/4d`, `Qt.size/point/rect` value types, and
    `Renderer3D.ConeMesh` / `PlaneMesh` primitives.

    Corpus warnings: 12 → 10 unique. Tests: TestView3DScene,
    TestView3DAnimatedTransform.

26. DONE: **QtMultimedia's object model** (`media.go`) — MediaPlayer with its
    AudioOutput, VideoOutput, and the capture side (CaptureSession / Camera /
    ImageCapture / MediaDevices), plus the MediaPlayer enum namespace.

    There is no decoder in this build (no cgo, so no ffmpeg) and nothing
    fetches media over the network, so the player is honest about it:
    assigning a source moves it to InvalidMedia, play() leaves it stopped,
    position/duration stay 0, and MediaDevices reports no hardware. That is
    what Qt does without the multimedia plugin — and it is the branch these
    pages are written for, so the whole multimedia page now renders with its
    own fallbacks: "Could not load media", "No camera detected", "0
    camera(s)". The transport controls, seek sliders and time labels bind to
    a real state machine (position is writable, so a seek slider works).

    Wiring MediaPlayer to the audio package's WAV playback, and adding
    ogg/mp3 decoding, is the step that would make these play for real.

    Corpus warnings: 10 → 4 unique. Tests: TestMediaPlayerContract,
    TestMediaDevicesEmpty.

The 4 warnings left are QtLocation's Map with QtPositioning (needs a tile
source) and the PDF pair (needs a PDF renderer).

27. DONE: **QtLocation's Map** and QtPositioning (`map.go`). Nothing here
    fetches map imagery, so a Map draws no basemap — but it does the part the
    overlays depend on: a real Web-Mercator projection around `center` at
    `zoomLevel`. MapPolyline / MapPolygon (a path of coordinates, stroked or
    filled), MapQuickItem (its sourceItem placed at a coordinate, offset by
    anchorPoint), MapItemView (a Repeater for overlays), MapCircle (a radius
    in METRES, converted through the Mercator scale at that latitude), and
    plain items declared inside the map. center and zoomLevel are engine
    state — `map.center = QtPositioning.coordinate(lat, lon)` and
    `map.zoomLevel += 1` work from handlers, clamped to the map's limits —
    and toCoordinate / fromCoordinate / pan invert the projection.
    QtPositioning.coordinate builds the value type; Plugin and
    PluginParameter are accepted and ignored.

    So the flight-simulation page draws its route, its waypoint markers and
    the moving aircraft in the right places relative to each other and to the
    viewport, with the map's own overlay panels on top.

    One general fix behind it: a delegate's per-instance scope now covers
    element-valued ATTRIBUTES too (`sourceItem: Column { Label { id:
    wpLabel } }`), not only children, so an id declared there resolves.

    Corpus warnings: 4 → 2 unique (both the PDF pair). Tests:
    TestMapProjection, TestMapStateAndItems, TestGeoDistance.

28. DONE: **QtQuick.Pdf's object model** (`pdf.go`) — PdfDocument plus the
    viewer items (PdfMultiPageView / PdfPageView / PdfScrollablePageView) and
    the PdfDocument enum namespace. There is no PDF renderer here
    (rasterizing PDF means fonts, shading and the whole imaging model), so a
    document given a source reports Error with a plain errorString and the
    viewer lays out its area without pages — the branch the page already
    handles, showing its drop target and status line. Its toolbar, page
    counter and library list all bind to a real object, and the viewer's
    page navigation (goToPage / goToNextPage / renderScale) is real state.

    **With this the corpus renders clean: 0 warnings across all 65 pages.**

    One thing worth recording, because it bit three of these backends in a
    row: state a QML page reads MUST be an engine property, not a plain Go
    field. Per-subtree dirty tracking proves a subtree fresh from its
    read-set, so a plain field written by a handler is invisible — the
    subtree is reused and the page never updates (a media player's
    playbackState, a map's centre, a PDF's status). MediaPlayer, Map and
    PdfDocument all hold their state in doc.floatProp/strProp/boolProp refs
    now, which makes reads tracked and writes invalidating for free.

    Tests: TestPdfDocumentStatus.

29. DONE (visual parity, first pass): four fixes that change how a lot of
    pages look.

    - **An Item is not a layout.** QML puts every child of an Item /
      Rectangle at (0,0) unless it is anchored or given x/y; gml's own
      dialect treats a box as a flex container (that is what `justify:`,
      `items:` and `spacing:` are for, and the gpui parity suite is written
      that way). So the QML rule now applies to **.qml documents**, where
      matching Qt is the point, and .gml keeps its flex boxes. The diorama
      page went from five stacked strips to one composed scene.
    - **A plain attribute is assignable through an element's id** —
      `box.width = 120` from a handler, which QML allows and pages use —
      recorded as an override that wins over the markup on every later
      build. And a **`property alias` now forwards a use-site value into the
      aliased element** (`property alias canvasWidth: canvas.width`), which
      is how that page sizes each layer. Two things had to be right for it:
      the alias's def must NOT be replaced by the use-site override (it IS
      the target expression), and an override has to drop the cached trees,
      since no read-set knows about it.
    - **Multi-stop canvas gradients paint every stop.** A sky ramped
      purple → pink → orange → gold used only the first and last colours
      before; `render.FillPathRamp` splits the mesh at each stop so all of
      them land exactly. **Radial gradients are radial**:
      `render.FillRadialRing` fans concentric bands around the gradient's
      own circle (colour by DISTANCE, which a linear ramp cannot express).
    - **CSS functional colours parse**: `rgba(255,200,80,0.40)`,
      `rgb(50%,0%,100%)`, `hsl()`/`hsla()`. There are 256 of them across 88
      corpus files, and every one used to fall through to the hex parser and
      come back OPAQUE BLACK — every soft glow and scrim on every canvas
      page.

    Tests: TestQMLItemChildrenOverlap, TestAttributeOverrideThroughID,
    TestAliasPropertyWrite, ui.TestParseFunctionalColors.

30. DONE: **item transforms** — `rotation`, `scale` and the `transform:`
    list of Translate / Rotation / Scale objects, on any item. Painting only,
    as in QML: a rotated item keeps its unrotated layout box, so rotating a
    card does not disturb its siblings, and a transform list applies IN
    ORDER (the first entry acts on the item, the next on that result) —
    which is what puts a spinner's dots in a ring: translate out from the
    centre, then rotate about it.

    The renderer grew a transform stack (`render/transform.go`): every
    vertex-based primitive (paths, text, images, lines) goes through toNDC,
    so it transforms exactly. The SDF quad is different — its shader
    rebuilds a rounded rect from an axis-aligned pixel rect — so a quad
    carries its own rotation angle instead: the transform decomposes into
    (centre, scale, rotation), the vertex shader rotates the corners about
    the centre and the fragment shader rotates its sample point back, so
    corner radii, borders and gradients all follow the rotation. Skew is not
    modeled for quads; none of QML's transform types produce it.

    Hit-testing maps the pointer back through the inverse, so a rotated
    button is clickable where it looks.

    Tests: render.TestAffine2Basics, render.TestTransformStack,
    ui.TestTransformComposition, ui.TestRotateScaleAboutCentre,
    gml.TestItemTransforms.

31. DONE: **MenuBar and Menu** (`menu.go`) — a MenuBar is a row of titles
    that drops the clicked menu under it; a Menu builds its own popup list
    (Action / MenuItem entries, separators, checkable ticks, disabled
    entries, submenu titles), opened by popup()/open() on its id or by a
    `visible:` binding. Floating children of an anchor container keep their
    x/y too: those position the panel in window coordinates, and stripping
    them into an anchor slot had put every popup at the origin.
    Test: gml.TestMenuBarOpensAndTriggers.

32. DONE: **Controls containers size to their content** — a ToolBar (or
    Pane, Frame, GroupBox, Page, Control, MenuBar) whose content fills it by
    anchor collapsed to nothing, because an anchor container sizes only from
    its own width/height. Qt puts a control's declared children in its
    contentItem and adopts their implicit size, so those containers measure
    their anchored content (`ui.DivEl.AnchorFit`).

33. DONE: **RangeSlider** (`rangeslider.go`, `ui.RangeSliderEl`) — one
    track, two handles that cannot cross, `first`/`second` published with
    value/position/visualPosition, vertical orientation, and a drag that
    grabs the handle nearest the press.

34. DONE: **font fallback chain** — UI fonts routinely skip the symbols QML
    pages use for icons (⌂ ↻ ▾ ✉ ▸), which Qt draws through its own font
    substitution. `Font.SetFallback` takes a list now (CJK first, then a
    symbol face — DejaVu Sans where present), and astral characters (📁,
    which JS can only spell as a surrogate pair) lex to one rune.

35. DONE: **a component instance is a real item** — three fixes, all
    visible on the drawers page (its panels sat open across the card):
    a component whose ROOT anchors itself is anchored at the use site
    (`anchorAttrsOf`), an id'd view finds its element under the instance's
    prefix (so `root.width` inside a component is not 0), and
    `default property alias contentData: panel.data` puts the use site's
    children in the element the alias names.

36. DONE: **layout fixes the pairs showed** — Layout.rowSpan (a grid with
    a row-spanning cell lays out on a real track grid), items in a
    RowLayout/ColumnLayout compress instead of overflowing, StackLayout /
    SwipeView pages fill their container, TabBar tabs share the bar and
    elide, and a Tumbler's slots share its height and clip.

37. DONE: **implicit sizes and self-scope** — Text/Paragraph report the
    content size they measured (QML implicitWidth), which is what markup
    sizes boxes from; an item's x/y read in its PARENT's coordinates; a
    bare `width`/`height`/`opacity` in a child's own binding is its own;
    and a control publishes pressed/down/hovered/checked on its id at
    declare time, so `text: btn.down ? ... : ...` resolves on frame one.

38. DONE: **drawing correctness** — ear clipping no longer stalls on
    collinear runs (a canvas roundedRect filled as four corner slivers),
    PathArc takes the SVG centre-sign the right way round (every rounded
    corner tied a loop), an 8-digit colour is #AARRGGBB as QML spells it
    (Qt.rgba() emits that too), `opacity: 0` means invisible rather than
    "unset" at all three layers, and textFormat is classified as the enum
    it is rather than string-matched.

39. DONE: **blur / frosted glass**, the last of the "needs an offscreen
    layer" gaps. `render.Layer2D` renders part of the UI into its own
    texture (its pass submits separately, since the renderer refills one set
    of vertex buffers per flush), `ui.Layer(state, child).Blur(px)`
    composites it back through a 13-tap kernel applied twice — once in the
    layer's own small texture, once while compositing — with each tap drawn
    over the last at its share of the weight so far, which leaves their
    weighted average rather than a ring of offset copies.

    In markup that is `layer.enabled` + `layer.effect: MultiEffect {
    blurEnabled }`, Qt5Compat's GaussianBlur / FastBlur / RecursiveBlur, and
    the same effects used standalone over a captured source — which is how
    the frostglass and lenstabs pages build their glass (a card over a
    blurred copy of its backdrop). `opacity` reads back off an id too, since
    `layer.enabled: root.opacity > 0` is how those pages gate it.
    Demo: `examples/frost` (live radius slider), smoke-tested by check.sh.
    Tests: render.TestBlurTapAlphasAverage, render.TestBlurDownscaleRange.

40. DONE: **fillRule** — `ShapePath.fillRule` (OddEvenFill, the default,
    and WindingFill), canvas `fill("evenodd")` and SVG's fill-rule /
    clip-rule. render gained `FillRule` plus rule-taking fill entry points;
    a simple outline still ear-clips, while one that nests or crosses itself
    goes to the scanline, which sums crossing directions for non-zero and
    splits its slabs at every crossing Y (without that, even-odd covered
    three times the right area). The ShapePath enum namespace did not exist
    at all, so `fillRule:` silently fell back to the default.

    Same page, same session: a path that closes by repeating its start point
    lost BOTH copies of that vertex to the collinear filter (each judged
    against the other), so the corpus star filled as a spike. Duplicates go
    first now, the wrap-around included.

41. DONE: **dashed strokes** — `render.DashPath` cuts a path into its
    on-runs and strokes those, so the pattern lives in the geometry (caps,
    joins and widths keep working; the dashes are measured along the
    outline, so they stay put when the shape animates). Wired to canvas
    setLineDash / getLineDash / lineDashOffset (saved and restored with the
    rest of the context state), ShapePath.strokeStyle (SolidLine, DashLine,
    DotLine, DashDotLine, DashDotDotLine with Qt's stock patterns) plus
    dashPattern / dashOffset in pen widths, and SVG stroke-dasharray /
    stroke-dashoffset.

    Three pages now match their own captions: the HUD's pitch ladder is
    dashed below the horizon, the bezier card's control guides are dashed,
    and the canvas card's red rectangle is the dashed outline Qt draws.
    Tests: render.TestDash* (even/odd patterns, offset phase, closed
    outlines, zero runs, "no pattern is solid"), TestSVGStrokeDashArray.

42. DONE: **styled SpinBox and Dial** — the last two controls on their
    builtins, and the general bug that had kept the Dial style from ever
    working: a style file's declared properties were keyed by NAME, so every
    instantiation shared one slot and the last one built decided what all of
    them drew (five dials painted the last dial's arc, while their handles,
    which read the control's own `position`, sat correctly). Declared
    properties carry the instance prefix now.

    SpinBox needed the rest of its contract: `up.indicator` /
    `down.indicator` as delegate slots, each with a click that steps `value`
    by `stepSize` clamped to the range; `up`/`down` sub-objects whose
    `pressed` the style colours by; textFromValue / valueFromText; and the
    control's padding insetting its contentItem, which centres its text when
    it asks to.

    Three general fixes came with them: `font: root.font` parses on a text
    input (and its retained state is keyed per instance, or two spin boxes
    share one text); `color` on a text item is the TEXT colour in a .qml
    document as QML defines it, and TextInput/TextEdit — the raw text items
    — draw no frame or fill; and on the first frame an item's `width`/
    `height` fall back to what the markup states, with a styled control's
    delegates seeing its implicit size as `parent`, without which
    `x: parent.width - width` put the stepper outside the control where it
    was clipped and unclickable until a second frame settled it.

    Tests: gml.TestStyledControlPropertiesArePerInstance,
    TestStyledSpinBoxIndicatorsStep — both stand up a real style module
    (qtquickcontrols2.conf + qmldir + T.* files) in a temp dir.

43. DONE: **styled Tumbler** — the last control on its builtin. Its style
    states its size through its parts, computes its own geometry from the
    content rect, hands the wheel to a TumblerView and hangs the selection
    band off the background, so: a control's implicit size comes from its
    background's and content's (implicitBackgroundWidth and friends); its
    contract is readable by BARE name inside its own bindings (QML's item
    scope), with that scope in place for the declaration pass, since
    `readonly property real __delegateHeight: availableHeight /
    visibleItemCount` computes from it; availableWidth/Height are the
    content rect; TumblerView is replaced by our own wheel, turning the
    control's own currentIndex; and the style's plain children lay out in
    the control's box.

    Two disconnects had to close for the date picker to work. The page's
    view of a styled control and the control's own scope kept SEPARATE
    state — the page wrote `yearTumbler.currentIndex = 26` into the ref its
    bindings and handlers closed over while the wheel turned another — so a
    styled control adopts the use-site element's existing refs and shares
    back the ones it creates. And a delegate the PAGE handed in is built in
    the PAGE's scope: both files call their root `root`, so building it
    inside the style's scope silently resolved `root.pad2(...)` against the
    control. (The last `delegate:` wins now too, as attributes do
    everywhere else.)

    `new Date(...)` also took only the no-arg and epoch-ms forms, so the
    `new Date(y, m + 1, 0).getDate()` idiom returned today's day-of-month
    instead of the month's length and the day wheel had one row. It takes
    JS's argument forms now, months 0-based, out-of-range parts normalized.
    Tests: gml.TestStyledTumblerUsesPageDelegateAndIndex,
    TestJSDateConstructorForms.

44. DONE: **dragging spins a tumbler**. ui gained a general
    press-drag-release gesture — `DivEl.OnDrag(fn(dx, dy, phase))`, which
    reports movement continuously where OnSwipe reports one flip per
    gesture; a press picks the deepest drag region under it, and a press
    that travelled more than a few pixels does not also click (releasing a
    spun wheel must not pick the row under the finger). `UI.Drag` drives the
    same path for tests and tools.

    The wheel follows the pointer a row at a time from the item that was
    centred when the press landed: half a row does nothing, letting go
    leaves it snapped, and a wheel that does not wrap stops at its ends.
    That is a detented wheel; item 45 gave it momentum.
    Test: gml.TestStyledTumblerDragSpins.

45. DONE: **kinetic flicking**. A press that is still moving when it is
    released carries on. `UI` samples pointer velocity once per frame
    (smoothed, last sample dominant, so a slow drag that ends in a flick
    reads as fast) and hands it to the region under the finger through
    `DivEl.OnFling(fn(vx, vy))` when the release is above a small floor.
    Motion decelerates at Qt's 1500 px/s², so a flick travels v^2/2a and
    lasts v/a — a constant-deceleration coast is exactly an ease-out
    quadratic, which is how the tumbler animates it.

    - **Flickable / ScrollView pan and coast**: `ScrollEl.Flickable()`
      registers the scroll area as a drag region — drag to pan against the
      pointer, release to fling, press to catch one still moving. It stops
      dead at the ends rather than overshooting (Qt bounces; we do not),
      and asks for the next frame only while it runs.
    - **A flicked tumbler spins through the coasted rows** and settles on
      an item, tweening a continuous wheel position. While that tween runs
      it owns the wheel; once it stops, currentIndex is the truth again
      (`core.TweenRunning`, new), so a page that writes the index during a
      spin is not fought by a stale animation.

    Tests: ui.TestScrollFlingDecelerates, TestScrollFlingStopsAtTheEnd,
    TestScrollDragFollowsPointer, gml.TestStyledTumblerFlingCarriesOn.

46. DONE: **a PathView carousel turns**. Its slot offset is a real number
    now, not an item count: a drag rotates it continuously against the
    pointer (one item per item's-spacing along the path — vertical drags
    for a tall path, horizontal for a wide one), a release settles on the
    nearest item, and a flick coasts on at the shared deceleration first.

    currentIndex stopped being display-only. It is derived from the offset,
    seeded and turned by the page's own `currentIndex:` binding, published
    on the view's id (so `view.model.get(view.currentIndex)` labels track a
    turn, and delegates' `PathView.isCurrentItem` follows), and adopted
    back when the page writes it — told apart from our own publishing by
    remembering what we published last build, since otherwise the carousel
    reads its own stale index as a page write and snaps back.
    `incrementCurrentIndex()` / `decrementCurrentIndex()` turn the short way
    round over QML's 300ms highlightMoveDuration; `positionViewAtIndex()`
    jumps.

    Tests: TestPathViewDragRotates, TestPathViewFlingAndIndexWrite,
    TestPathViewIncrementCurrentIndex.

47. DONE: **per-page layout polish**, read off the side-by-side pairs. Each
    of these was one page's cosmetic complaint and an engine bug underneath:

    - **Qt.lighter / Qt.darker wrote #RRGGBBAA where QML reads #AARRGGBB.**
      A lightened teal came back with the source's zero red channel as its
      alpha, so it was fully transparent: every hovered, pressed and
      highlighted control lost its background, page-wide. The single
      highest-value fix of the pass.
    - **Layout.preferredWidth/Height is the size, not a suggestion.** Qt
      hands the item its preferred size and lets the row overflow; ours
      shrank items to fit, cutting the labels out of a row of buttons. It
      is a floor as well as a size now. (An implicit-size floor was tried
      first and was wrong the other way: it pinned a vertical Slider at the
      style's implicit 270px when the page asked for 150.)
    - **A GridView guessed its column count** from a 300px placeholder
      whenever it had no width of its own, so a view filling its parent
      showed three columns where four fit. ui gained flex-wrap
      (`DivEl.Wrap()`) — lines break at the width the row is given, stack
      by their tallest child, and measure height-for-width — and a vertical
      scroll offers its viewport width to the content it measures, so the
      wrapping row inside one breaks at the view's width. A GridView is now
      that wrapping row, so the columns fall out of the real width on the
      first frame. QML's **Flow** wraps by the same mechanism (it had been a
      plain row that ran off the edge).
    - **QtQuick.Layouts space their items 5px apart** by default, as Qt's
      do; an icon and its label in a bare RowLayout were touching.
    - **A styled control does not clip its own content**: Qt clips at the
      card. "Airplane Mode" read "Airplane M".
    - **A styled RangeSlider** was the last control still drawing its
      builtin look — see the next item.
    - **A TabButton's own font.pixelSize** decides how much of its label
      survives in a shared bar.

60. DONE: **interaction parity — the same script played by both engines**
    (ui, ui/shell, script/gml, examples/qml, scripts). A screenshot only
    proves a page looks right as it OPENS. `scripts/qml-interact.sh NAME`
    plays one script of clicks, drags, keys and wheels against real Qt and
    against us, capturing a PNG per step and pairing them:

        scripts/qml-interact.sh tabbar     # ref|ours per step, + RMS

    Qt's side is a QtTest case generated from the script (`qmltestrunner`
    synthesises real mouse/key events offscreen and grabs the window);
    ours is `examples/qml -script` driving `ui.UI` inside a
    `shell.ScreenshotScript` settle loop, so a step's animation lands
    before its capture. The scripts live in `check/interact/` and are the
    test cases — both engines replay the same file, so a pair always
    compares one gesture, not two that drifted apart.

    A click there is a press AND a release through the real event path
    (`UI.Mouse`), not `UI.Click`, which calls the handler directly: hover
    and pressed state, and click-on-release, are exactly what static
    screenshots cannot see.

    Ten pages in, THIRTEEN real bugs — none of them visible in a static
    render. Every page it was pointed at had one:

    - **A TabBar moved its highlight and nothing followed.** The bar owned
      its `currentIndex` under one key (`|currentIndex`) while every
      binding read the slot its id published under another
      (`.currentIndex`, from the declare pass) — two forked properties.
      The canonical `StackLayout { currentIndex: bar.currentIndex }` never
      changed page. A control now takes its state from the ref its id
      already publishes (`doc.idPropRef`).
    - **An open ComboBox dropdown was a 4px sliver.** A nested `T.*`
      control resolved the Templates alias only from its own node, and the
      alias lives on the FILE's root — so a style's `delegate: T.ItemDelegate`,
      built later when the popup opens, fell through as an unknown dotted
      type and measured nothing.
    - **Every popup row read `map[modelData:Apple]`.** The combo popup
      wrapped model ROWS in another row.
    - **A tri-state "Select All" whose children were all checked rendered
      empty.** `checked` and `checkState` were unlinked; QML keeps them in
      step (Qt.Unchecked 0 / PartiallyChecked 1 / Checked 2).
    - **Not one Dialog in the corpus could ever appear.** Our popups only
      showed while a bound property said so, and QML opens a dialog by
      CALLING it (`infoDialog.open()`). Popups now own their open state and
      publish open/close/accept/reject with Qt's signals, and a Dialog
      draws its title header and standard-button box.
    - **A password field showed its password.** `echoMode:
      TextInput.Password` never evaluated (no TextInput enum namespace);
      `validator:` was accepted and ignored, so a numeric field held
      "12x9"; and a styled field's text never reached the page, because the
      declare pass gave the id one InputState while the control typed into
      another — so `"Name: " + nameField.text` stayed empty on every form.
    - **Every slider drag also panned the page.** A press started BOTH the
      slider and the flickable under it; in QML the control accepts the
      press, so the Flickable never sees it. Also: `stepSize`/`snapMode`
      ignored (the stepped slider reported 19 for a step of 10),
      `orientation: Qt.Vertical` ignored (it read the pointer's X across a
      narrow track), and `enabled: false` disabled nothing.
    - **A list tap selected nothing.** A delegate's handler runs frames
      later and could not see the row's own ids, so the canonical
      `view.currentIndex = delegate.index` resolved to nothing; and state
      published on an id reached only one of its two objects inside a file
      component, so the grid's selection never moved either.
    - **The wheel always scrolled the page.** Scroll regions registered
      after their content laid out, which put the OUTERMOST last — and the
      wheel walks the list backwards to find the innermost region under the
      pointer.
    - **Every menu opened at the window's corner**, positioned from the
      title's offset within its bar (an overlay's x/y are window
      coordinates), and could only be closed by picking something.
    - **A held button never reported itself pressed**: the styled control
      moved a pressed ref of its own while the page's `pressBtn.down`
      binding read the one seeded on its id. The state is also reported as
      the tree LAYS OUT now, not while painting, so it exists for anything
      that lays a tree out.
    - **A component's own functions stopped working** once the build that
      declared them finished: the binding aliasing the use-site id to the
      instance is undone when the scope unwinds, so `leftDrawer.open()`
      failed as `no member "open"` from the very button written to call it.
    - **`on<Prop>Changed` never ran per instance.** Wired once on the
      document-global ref, a component reacting to its own property
      (`onOpenedChanged: position = opened ? 1 : 0`) never ran for the
      instance whose property moved — so a drawer set `opened` and stayed
      shut. It is wired in the instance scope now, at the END of it, so the
      handler can see the instance's other properties.

    Engine-wide fixes the same pages forced:

    - **Escape closes the topmost dismissable overlay** (Qt's popups all
      take it; ours left an open dropdown stuck over the page).
    - **A method published on an id has to reach every object that id
      names.** Inside a file component the declare pass binds the id bare
      and the build pass runs under the instance prefix.
    - **A popup with no background of its own takes its skin from how dark
      the page is.** Its content is written for the PAGE's palette — a dark
      app's dialog holds light text — so a light card made that text
      invisible.
    - **An unstyled ItemDelegate is a list ROW**, not a button: no fill of
      its own, its label leading at Qt's 12px inset, 46px tall for a 16px
      label (measured from the corpus). Built as a button, a drawer's menu
      came out as a stack of pills.
    - Tooling: `ui.TextOf` reads what a built element says (the counterpart
      of `FillColor`), and `ui.Children` descends anchored subtrees,
      overlays and modals instead of stopping at them. Both cost real time
      to debug without.

    A second batch, driven the same way — rangesliders, splitview,
    swipeview, scrollview, indicators, toolbar, animations, transforms,
    pathview — found five more:

    - **A SplitView's handle was decoration**: the panes kept whatever the
      markup asked for however hard it was pulled. Each handle now owns the
      offset it has been dragged, clamped at both panes' minimums, which is
      where Qt stops too.
    - **RangeSlider ignored stepSize/snapMode** (its stepped pair reported
      56—70 where Qt snapped to 60—70), and **a Dial had no interaction at
      all** — clicking one moved Qt's and left ours where it was. ui gained
      the rotary counterpart of the bare slider: a region that maps the
      pointer's ANGLE about its centre over Qt's -140°..+140° sweep.
    - **A PathView turned from anywhere in its box.** Qt's `dragMargin` is
      0 by default, so the carousel turns only when the press lands ON an
      item; a press in the middle of a circular path does nothing. The
      items carry the drag now, and `dragMargin` widens the reach.
    - **A handler could not reach its surroundings.** `parent` is bound
      only while an element's own attributes build, so
      `onClicked: parent.parent.originMode = 1` — how QML pokes the card an
      element sits in — failed as "unknown identifier parent" and a page of
      origin buttons did nothing. Handlers carry what `parent` meant where
      they were written, and `parent.parent` walks up.
    - **Animations did not run.** Four things: QML plays them by CALLING
      them (`easingAnim.restart()`, which every Play button on that page
      does) and there was no imperative API; a document's Timers and
      animations advanced on the WALL clock, which barely moves between two
      headless frames; animating item geometry (`property: "x"`) wrote an
      override the parent's anchor pass never read; and only four easing
      curves existed, so four differently-eased animations moved as one
      (core has QEasingCurve's whole set now).

    Two harness lessons from that batch, both encoded:

    - **A scripted step advances a FIXED slice of animation time** (700ms,
      the same wait the generated QtTest case uses), so a running animation
      is sampled at the same moment on both sides. Settling "until nothing
      moves" caught ours at the end of a tween and Qt's mid-flight.
    - **A page whose animations never settle is captured at the same 2.5s
      the reference is** — which brought spritestack from Δ12.5 to Δ8.1,
      better than it had ever been.
    - And the pairs script now says when a step left the REFERENCE
      unchanged: a flat Δ down the column otherwise reads as agreement when
      it is really two engines both doing nothing, which is how three
      "clean" sweeps turned out to have missed every control they aimed at.

88. IN PROGRESS: **the manual review pass — three live windows per page,
    and a Go port as the control** (scripts, examples/qmlgo, app, core, ui).
    Reviewed so far: buttons (1/65). Corpus unchanged at 96.5.

    A screenshot cannot show hover, press, a menu opening or an animation
    running, and a .gml page proves nothing about whether a widget exists
    in the FRAMEWORK or only in the markup engine. So each page is now
    opened three times at once — `scripts/qml-trio.sh <page>` places real
    Qt, our gml engine and a hand-written Go port side by side on one
    screen and samples each one's CPU, RSS and GPU memory — and looked at
    by hand. The ports live in examples/qmlgo/<page> on a shared
    examples/qmlgo/corpus package that restates the corpus' own QML style
    (colours, Style.resize, Quicksand, icons, the card and page frames).
    script/gml/check/REVIEW-STATUS.tsv tracks page/gml/go, and the parity
    board leads with it.

    Two harness lessons before the first page could even be seen: every
    corpus Main.qml is an Item at `opacity: fullSize ? 1.0 : 0.0`, so
    loaded raw it is perfectly transparent (Qt showed a white window) —
    the live host sets fullSize the way the app's Dashboard does; and our
    engine resolves `import utils` by walking UP for an imports/ root, so
    the harness' copy of a page under build/ renders BLACK, every Style.*
    an unknown identifier. Ours loads the page from the corpus, Qt gets
    the copy with the compiled .qsb shaders.

    What the first page turned up, all of it invisible to a screenshot:

    - **A live gml page FROZE the moment it stopped taking input.** The
      frame loop renders on damage, and a gml animator is invisible to
      it: the document's clock only advances while it is being rendered,
      and the revision it bumps is bumped DURING that render, so the next
      frame is skipped and the clock never advances again. The pulsing
      ring on the buttons page never pulsed. `core.SetExternalAnimation`
      (and `core.ExternalAnimating`, which the shell now checks) is how a
      driver outside core's tweens says it is moving.
    - **The cost of that animation, and what four fixes bought.** One
      pulsing ring, live at 900x700, cost Qt 3.9% of a core against our
      Go port's 43.6% and the gml page's 104.2%: both of ours redraw the
      whole window every frame where Qt repaints a rectangle. Profiled,
      fixed in the order the profile named them, and re-measured:
        - **An SVG re-triangulated its every path on every frame** — ear
          clipping three toolbar icons was **79% of a gml frame**. A
          shape's fill is now ear-clipped ONCE, in the document's own
          coordinates, and the draw transforms the triangles (an affine
          map of a triangulation is the triangulation of the affine map);
          the no-rotation case has the transform inline rather than
          through a per-vertex closure. Per-frame CPU on the buttons
          page: 75ms -> 13ms.
        - **Text was re-shaped every frame.** The cmap lookup is memoized
          per face (sfnt walks format-4 segments per call, behind its own
          per-call closure) and MeasureText per (face, string) — a
          retained-mode page measures the same strings twice a frame, to
          lay out and to paint. The Go port's frame: 5.8ms -> 2.6ms CPU.
        - **Animation-driven frames are RATE-CAPPED** (60fps, GFX_MAX_FPS)
          — a 240Hz monitor was asking for four times the work, and the
          whole window is redrawn either way. Input, damage and resizes
          still render at once.
        - Result: the Go port 43.6% -> **11.4%** (Qt 5.5%). The gml page
          renders 36 -> 58fps at 122%: its per-frame work fell by a
          third, and what is left is the REBUILD, not the drawing.
    - **Round three, measured under a fence.** Two unbounded runs took
      the desktop down (a 5.5GB cache experiment; a full check.sh in
      swap), so every measurement now runs in a user cgroup
      (scripts/bounded.sh: MemoryMax, no swap, CPUQuota) and
      scripts/qml-perf.sh reports fps and per-frame uploads. What that
      found: the buttons page uploaded **6.3MB of vertices a frame** —
      195k triangle vertices, 194k of them six 24px toolbar icons.
      Path.CubicTo's fixed 16-160 step suits pixel coordinates; an SVG
      flattens in viewBox units and a 960-unit icon became thousands of
      points. SVG paths now carry a flatness (a thousandth of the
      document's extent) and subdivide adaptively: 2.7k vertices, 220KB,
      corpus and cad canary pixel-identical. gml 112% -> 74% of a core.
      Also found: the 12ms a frame the profile charged to WriteBuffer is
      the driver's vsync WAIT (both engines pay it, one at 12% CPU), and
      after a desktop session restart the FIFO swapchain stalled every
      frame to the driver's 1s timeout — immediate presentation was fine,
      headless was fine — so the shell now watches acquire+present and
      falls back to immediate after three stalls.
    - **Round four: the subtree cache finally HITS.** Geometry reads now go
      through per-element GEOMETRY TOKENS (a property carrying a hash of
      the item's rect, stamped by beginFrame when it moves; keyed by the
      element registry key, because the `parent` object doing the reading
      is made per build); plain values exposed on an id (`dot.opacity`, a
      control's contract) carry document-keyed tokens too; the node
      identity check is gone; and a subtree built twice in one frame under
      one key (a style's content container, as delegate and again with
      the page's children in it) gets numbered slots. Then the untracked
      reads had to be found one by one — GFX_UNTRACKED_TRACE counts them
      by call site and by identifier — and every one was a constant
      mis-marked as mutable: the builtin namespaces (`Math`, `Qt`,
      `Text.AlignHCenter`), a function's own parameters (`Style.resize(v)`
      poisoned every subtree that called it), an item's own width/height,
      a control's contract geometry, an image's sourceSize. Env scopes
      carry a `derived` mark for those now. What must NOT be derived was
      learnt from the tests: a layout's preferred size the PARENT computed
      (a layer cached at (80,50), belonged at (-70,-25)), a contract state
      placeholder that a Ref later replaces (a held button never read as
      pressed), a particle system (an empty box when cached), and any
      element a parent MUTATES after build() (the SplitView sizing its
      panes — the IDE card's editor column came back a third shorter, and
      panes now build fresh). Bisected with GFX_CACHE_SKIP=<key> against
      GFX_NO_SUBTREE_CACHE, both permanent knobs.
      Buttons page: **234 fresh subtree builds a frame -> 33**, 20 hits;
      live CPU 74% -> 60%. Corpus unchanged (date's +4 is the reference
      being three calendar days old; the picker shows the 12th), cad canary
      pixel-identical, gates green. What is left, profiled: the 33 that
      rebuild are the pulsing ring's ANCESTOR CHAIN (a child's change makes
      every parent rebuild, since a parent's element embeds the child's —
      element-level splicing would end that), plus layout (anchorLayout
      alone is a fifth of a frame) and painting the whole tree. That last
      one is the damage-driven redraw, which is next.
    - **Round five: damage-driven repaint.** A frame used to repaint every
      pixel whatever had changed; Qt repaints the ring's rectangle. Now,
      after layout, `ui` walks the tree once (ui/damage.go): every element
      is keyed by its PATH from the root and compared with last frame's
      entry — the same pointer at the same rect, or a rebuilt one whose
      PAINT SIGNATURE (a hash of what its paint reads: colours, radius,
      borders, shadows, text, font, transform...) matches. Path keys and
      signatures are what make it work for both front ends: a gml page
      rebuilds the animated child's whole ancestor chain and a Go view
      rebuilds everything, so pointer identity alone found the window
      changed every frame. The damage is the union of the old and new
      paint bounds of what changed (or is dirty: a canvas, a flick, a
      blinking caret, a playing GIF, a re-rendered layer); painting culls
      clean subtrees outside it; the shell opens the frame with the clear
      colour over just that rectangle, draws into a pass that LOADS the
      persistent MSAA target (the resolve still writes the whole swapchain
      image), and every scissor is clamped to it. Input, overlays, a
      drag, a focus ring, a resize, an expose and the first frame force a
      full repaint. Verified live: a grab with damage on is pixel-identical
      to one with GFX_NO_DAMAGE=1.
      Measured on the pulsing ring: the damaged area is **1.3% of the
      window** a frame (a first version inflated a transformed box by its
      diagonal and damaged 26%; the exact transformed bounding box fixed
      that), uploads 220KB -> 31KB a frame. Tests: TestDamageCullsClean
      Subtrees, TestDamageFirstAndForcedFramesAreFull.
    - **What the CPU turned out to be, once pixels were cheap.** The
      damage work did not move the CPU numbers at all (gml 42%, Go 12%),
      and steady-state profiles (GFX_CPUPROFILE_SKIP, which starts the
      profile after the start-up frames — font atlases cost more than a
      hundred frames and a short profile is all start-up) said why:
        - A frame built and laid out the tree TWICE: the shell called
          Layout for the layer pass and Frame built again. Frame paints
          the tree Layout produced now. gml 60% -> 42% of a core.
        - The subtree cache's read-set replay was QUADRATIC: a cache hit
          copied the child's deps into the parent's set through a linear
          dedupe scan — half of every gml frame in slices.Contains.
          Read-sets now hold their nested sets by reference and Stale()
          is memoized per property revision (nothing a set depends on
          can change without a write, and every write bumps it), so the
          whole tree is checked once a frame however many lookups ask.
        - A styled control's shadow object was rebuilt every build: a
          fresh defaults map, four nested maps, seventy stamped writes,
          each formatted through fmt to stamp its token (a sixth of a
          frame in Sprintf). The object is kept per use site now
          (doc.tplObjs), scalars that did not change are not written,
          and maps are hashed by content instead of formatted.
        - The Go port's ONE glow layer re-rendered every frame: the
          layer's skip test watched the global property revision, and
          the port's status label was writing itself back twice a frame.
          A layer now re-renders when its content's paint signature
          changes (ui.SubtreeSig) — which also stops every frosted panel
          on a gml page re-rendering whenever any property anywhere
          moves — and the port's bug is fixed.
      Result, live at 900x700 with the ring pulsing: **gml 112% -> ~35%,
      Go 44% -> ~16%, Qt ~5%** (the fenced sampler's runs vary by ±5).
      Present mode is not the lever: immediate presentation measured the
      same as FIFO and mailbox crashes this driver. What remains in gml is
      the long tail of rebuilding the animated child's ancestor chain
      (33 fresh subtrees a frame: template scope seeding, row scopes,
      shadowing), plus layout, plus the damage walk itself (~9%); ending
      the chain rebuild means splicing a cached child into a fresh parent
      without re-running the parent's build, which is the next structural
      step. A Canvas no longer paints before its document has completed
      (Qt runs onCompleted before the first frame; ours completes on the
      second build, and one build per frame put the first paint ahead of
      the arrays it indexes). Corpus unchanged apart from date (+4.1: the
      Qt reference is three calendar days old), cad canary pixel-identical,
      gates green, warning sweep 0.
    - **Round six: the last untracked reads, and a number that means
      something.** With GFX_UNTRACKED_TRACE the remaining "stale for ever"
      subtrees were the buttons page's whole right-hand column: every
      ancestor of a closed Menu, because a closed menu is never laid out
      and its `contentHeight` read went through an UNREGISTERED element,
      which had been made untracked. It is a token again (an absent
      element hashes as absent and stamps the frame it appears; the real
      bug that rule had been covering was the SplitView mutation, fixed
      separately), keyed by the registry key beginFrame resolves through
      idKeys — keyed by the bare id it hashed a key nothing registers
      under and never stamped (an effect capture served at x=0 for ever).
      Scroll offsets and content sizes got tokens too (one per
      ScrollState, stamped once a frame), and a value bound as DERIVED
      now carries the token its reader must record, because the bind
      happens ahead of a property binding's read-set (`page:
      Math.round(contentX / 200)` never re-evaluated otherwise). Result:
      **0 untracked subtrees; 17 fresh, 16 served a frame** — the fresh
      ones are exactly the pulsing ring's ancestor chain.
      Also this round: a handler's scope snapshot keeps the builtins as a
      live FROZEN parent instead of copying a hundred names per handler
      per build (the document env is still copied — an instance's ids are
      bound into it around its build and a handler captured in between
      has to keep what it saw); element keys are cached on the node
      instead of Sprintf'd; the damage walk's hash mix is one multiply
      per word (it was six percent of a frame byte-by-byte).
      **The number that means something**: live CPU% is mostly the
      driver. A MaxFrames run now reports its own rusage at exit, and two
      run lengths cancel start-up: **gml 0.94 ms of CPU per frame, the Go
      port 0.25 ms** — six and one and a half percent of a core at 60fps.
      The live sampler's 32% / 17% is the rest: NVIDIA's Vulkan present
      and acquire spin in-process (a capped profile is 29% of a core with
      our code at less than half of it). Immediate presentation measured
      the same and mailbox crashes the driver, so that part is not ours
      to fix here. Corpus unchanged, cad canary pixel-identical, gates
      green, sweep 0.
    - **Round seven: what the live percentage actually measures.** The
      shell now prints per-phase wall time per frame (GFX_FPS: layout,
      layers, paint, flush, present, wait). At 60fps the gml buttons page
      spends 3.3ms a frame in layout+build and sleeps 12; uncapped, the
      SAME build takes 0.6ms. Varying the cap: 3.05ms at 30fps, 3.5 at
      60, 2.0 at 120, 1.3 at 240, 0.6 uncapped — and the Go port's layout
      goes 0.68 -> 0.07. That is the CPU, not the code: this i7-8700K
      runs the `powersave` governor, the core drops between 12ms sleeps,
      and every frame runs cold and slow. The live "30%" is mostly clock
      ramp; Qt pays the same physics on a frame that is ten times
      smaller. So the deterministic metric is the one to steer by:
      **scripts/qml-cpuframe.sh** — two uncapped run lengths, the app's
      own rusage at exit, start-up cancelled: **gml 0.71ms of CPU per
      frame, the Go port 0.20ms** (was 0.94 / 0.25 at the start of the
      round; 60fps needs 16.7). scripts/qml-cpusample.sh is the live
      number, kept because it is what a user's `top` shows.
      Allocation was the lever this round (a differential heap profile,
      1800 frames minus 600): **560KB -> 315KB allocated per frame.** A
      cache hit copied its element registrations into every enclosing
      build's list (held by reference now, replayed recursively); the
      element registry was regrown from empty every frame (two maps
      rotate and clear); a handler-scope snapshot grew its map entry by
      entry through five reallocations (sized up front; the builtins are
      a frozen shared parent, but the document env is still copied — an
      instance's ids are bound into it around its build and a handler
      captured in between must keep what it saw); element keys are cached
      per node and prefix. What allocates now is diffuse: the snapshot
      copies (20%), the per-element scope shadowing, node copies.
      Corpus unchanged, cad canary pixel-identical, gates green, sweep 0.
    - **A template's contentItem ignored horizontalAlignment.** The
      wrapper that centres a filling Text vertically left it at the
      leading edge, so every specialized button's label sat against the
      pill's left edge where Qt centres it (the reviewer's first catch on
      the live windows). It follows AlignHCenter/AlignRight now; buttons
      2.2 -> 2.1, sliders 1.9 -> 1.8, textinputs 3.1 -> 3.0, corpus 96.3.
    - **Why the gml page still costs 15x what Qt's does.** Instrumented:
      **234 subtree builds per frame, zero reuse.** Three reasons, now
      each measured. (a) `setAttrOverride` called `invalidateCache()`,
      so an animator writing one attribute a frame emptied the whole
      subtree cache every frame — fixed: each override carries a
      tracking token (a Property read while a subtree builds, written
      when the override changes), and the cache now retains its 234
      entries. (b) Every node handed to `build()` is a freshly derived
      pointer (overrides merged, sizes stripped, handlers consumed, a
      component's `inst := *comp`), so the identity check never passes.
      (c) With the check relaxed, 50 of 234 hit and **158 are stale for
      ever**: they read layout geometry, which is an untracked read.
      Geometry needs the same token treatment as (a) — a per-element
      stamp written when its rect changes — and then (b) can go. Until
      then the cache is also gated on `doc.rowPrefix`, which a styled
      control or a file component sets for its whole subtree. On a corpus page dressed in styled controls that is
      everything. Gating on the row scope instead — the thing a read-set
      genuinely cannot see — was tried and MEASURED: worse (37fps, 5.5GB
      resident), because a template instantiation seeds a fresh env scope
      per build, so every entry misses on `e.env == env` and the cache
      grows without ever hitting. Caching inside components needs a
      stable per-use-site scope first; that is the next piece of work,
      and it is the one that decides whether an animated .gml page can
      cost what Qt's does.
    - **A resize exposed undefined pixels**, and the obvious fix was
      worse. X11 windows here carry no background (deliberately, to avoid
      a repaint flash), so the strip a WM-driven resize opens keeps
      whatever was in it — black, or a see-through hole under a
      compositor — until our next frame lands. Giving the window its own
      clear colour (`app.Window.SetBackground`, kept but NOT used) makes
      the X server clear the WHOLE window on every ConfigureNotify, and
      an interactive resize is thirty of those a second: the window then
      reads as BLANK while it is dragged. Undefined slivers at the
      growing edge are the lesser evil, and the real fix is a repaint
      fast enough not to be seen — which is what the frame-cost work
      above is for.
    - **The scrollbar disagreed with Qt three ways.** Qt's ScrollView
      makes its bar active while the view moves AND while the view is
      hovered; ours showed it only while moving (gml) or permanently (a
      plain ui.Scroll, which is what the Go port got). AutoHideBar now
      covers hover, and the corpus page frame uses it.
    - **A Go port needs Qt's LINE metrics.** Qt lays text out on the OS/2
      winAscent/winHeight box; the face's own hhea box is shorter and
      sits higher, so every centred button label was three pixels high.
      gml turns this on for the whole process (script/gml/doc.go) and a
      port has to as well.
    - **Three gaps in ui the port could not talk around**: an ImageEl
      could not be colorized (`icon.color` on a control), a GridEl could
      not be told to fill its parent's cross axis
      (`GridLayout { Layout.fillWidth: true }`, in every corpus page),
      and an unsized Img did not take its source's size. All three are
      now in ui, and a Flow (a wrapping row) is `Div().Row().Wrap()`.
      Button, ToolButton, Menu and the corpus' specialized
      buttons remain local helpers in examples/qmlgo/corpus — ui has no
      controls of its own, and whether it should is a decision this pass
      is collecting evidence for.
    - **One Qt behaviour worth writing down**: `Layout.minimumWidth` on a
      plain Item is 0, so a card in a GridLayout shrinks to its share and
      its contents run off the edge — Qt's own render cuts "Flat Butto|n"
      at the card's right edge. A port that lets the card size to its
      content pushes the second column off the window instead.
    - **Round eight: the interactions the reviewer asked about, and the
      frame floor.** "Tool buttons — menu should open on click; colour
      change on the middle-right card on click — check both go and gml."
      Both were broken, differently on each side, and the same scripted
      probe now runs against both (shell.RunScript, moved out of
      examples/qml into ui/shell; every Go port takes `-script` and
      `-w/-h`): press Success, release, click File, click Settings.
      **gml, pressed colour:** the pre-seeded hover/press refs on an id
      object were keyed by the BARE id, and every instance of a style
      file has `id: root`, so two styled Buttons side by side shared one
      `root.pressed` — the held one wrote true, its neighbour wrote false
      a moment later, and the style's `__bgColor` never darkened (a single
      styled button worked, which is why no test caught it). Keyed by the
      element key now, like wireHoverPress's. Tests:
      TestStyledButtonDerivedColourFollowsThePress. **gml, the menu:**
      three things stacked. A builtin Button never built its children, so
      `fileMenu.open()` found no member; a styled control stripped the
      x/y off every child as anchors, so `Menu { y: btn.height }` lost its
      y and floated centred; and a popup's x/y are in its PARENT's frame
      (QML) where ours read them as window coordinates — the origin is
      added now (popupOrigin), skipping the popup's own scope, which a
      styled control had already installed as `parent` (read as its own
      parent, the menu walked down the window by 40px a frame). The
      styled panel was also pinned to the style's implicitHeight
      expression, which cannot see a ListView's contentHeight — 8px tall,
      rows spilling below; a Menu is content-sized like a Dialog now.
      Tests: TestMenuInsideItsButtonOpensUnderIt,
      TestStyledMenuInsideStyledToolButtonOpensUnderIt. **Go port, the
      menu:** the overlay read the box of the element built THIS frame,
      which has none until layout, so it returned nil every frame; it
      reads last frame's boxes now (snapshot at the top of view). The
      second click, on Settings while File is open, closes the menu and
      opens nothing on either side — a Menu is a modal popup here, the
      press is consumed; to be confirmed against the Qt window.
      **The frame floor (b):** a frame with empty damage is skipped BEFORE
      the swapchain acquire, not after it (skippedFrames on the GFX_FPS
      line); DivEl and TextEl memoize measure within a frame (475 -> 194
      measures per frame on buttons; layout ms barely moved, 0.51 -> 0.46
      at 60fps, so measure was not the cost); the GFX_FPS line carries
      GPU call counts (draws, bind groups, pipelines, scissors, vertex
      buffers, encoders, passes) — five draws and nine bind groups a
      frame, so the ~0.45ms of Present+Finish+Submit is fixed driver cost,
      not our call volume. Corpus 100.4 -> 100.3, cad canary identical,
      sweep 0, gates green.

89. IN PROGRESS: **the gml frame floor — stable scopes instead of
    rebuilt ones.** A deterministic benchmark first
    (BenchmarkCorpusButtonsFrame: build + layout of the real buttons page
    with its pulse running, no present, no vsync sleep — the live
    number at 240fps varied 1.7–2.4ms for the SAME binary, which is the
    clock ramp, not the code). Profiled, evaluating gml expressions was
    ~3% of the frame: an AST-to-closure compiler was not the lever. The
    17 subtrees rebuilt per frame spent their time re-making scope —
    snapshots, shadowed names, derived node copies — and in the GC that
    followed. This round, all on the buttons page, 509 -> 276 µs per
    frame (-46%), 180 -> 52 KB allocated per frame, 1318 -> 705 allocs:
    - **Scope snapshots are memoized per site** (docState.scopeSnapshot):
      every rebuild of a component instance copied the whole scope chain
      for each animator, timer, frame animation, canvas, alias and the
      component's functions. What a scope binds is persistent (id
      objects, property refs live in the document's maps), so a copy
      holds until a declaration pass creates something new
      (scopeGeneration: object count, ref count, invalidations); only
      the values a build recomputes — `parent`, the item's own geometry
      and opacity/visible, a control's derived contract — are copied
      again on a hit. Inside a delegate row the snapshot stays fresh
      (row values are bound per row and not tracked here).
    - **Self-geometry shadowing is skipped for nodes that never read it**
      (selfreads.go): the parser marks every node whose subtree — attrs,
      handlers, property defaults, functions, element-valued attrs, and
      an `on <prop>` animation's target — never mentions a bare
      width/height/opacity/visible or Flickable state. That removed a
      fifth of the frame's allocation AND four of the seventeen rebuilds:
      the shadow recorded a dependency on the item's own geometry token
      for items that never read it, so a moved neighbour rebuilt them.
    - Smaller: the merged override node is kept on the node and its
      literal refreshed in place (an animator writes the same attribute
      every frame); stripAnchorAttrs copies once per node and returns
      the node itself when nothing is stripped (both caches keyed by the
      attrs slice they came from, so a derived copy never serves a stale
      one — the first version did, and a `SequentialAnimation on opacity`
      lost its override); `parent` objects are one per key, re-pointed
      per frame; the undo lists of shadowControlScope/shadowSelfGeometry
      are pooled; the view-attached namespace map is two shared
      constants; an animator's start/stop methods are built once; a
      handler's captured `parent` no longer allocates a map.
    Gates green, sweep 0, cad canary identical, corpus 100.3 -> 100.2.
    - **Second pass, 276 -> 244 µs, 52 -> 41 KB per frame.** A closure-free
      user function (`Style.resize(x)` is an identity function the corpus
      calls for nearly every size) runs in a POOLED call scope — the parser
      walk decides once per function whether an arrow function or nested
      declaration inside could keep the scope alive. wireHoverPress wires
      nothing for an id-less box with no hover/press handlers (nothing can
      read its state), and finds a box's two refs by one lookup instead of
      two string concatenations. exposeIDProp uses the node's cached key
      rather than building `prefix+id`. stripConsumedHandlers returns the
      node when it has no signals or properties. The whole-document
      geometry signature is computed only when the tree could be served
      (observables unchanged) — an animating page changes an observable
      every frame and paid for the fold regardless; the first quiet frame
      after a change rebuilds once more to record it. Corpus 100.4 (the
      animated pages' screenshot timing moves it ±0.2).
    - **Where it stands, on the same footing.** The Go port has the same
      benchmark now (examples/qmlgo/buttons BenchmarkButtonsFrame: view
      rebuild + layout, pulse advancing, no font or icons on either side):
      **Go 31 µs, gml 244 µs per frame** (gml was 509). What the 13
      rebuilds are (GFX_BUILD_TRACE): the CHAIN from the root to the
      pulsing ring — root, ScrollView, ColumnLayout, GridLayout, the
      SpecializedButtonsCard instance, its ColumnLayout, the Flow, the
      PulseButton instance, its wrapper, the ring. Only the ring changed.
      Each ancestor rebuilds because its read-set adopts its children's,
      and each rebuild costs ~15 µs of scope machinery (about forty
      percent of the gml time is string-keyed map work: element keys,
      registries, env lookups). Rebuilding the ring ALONE — patching it
      into a kept parent — needs the ring's scope to exist outside its
      parents' builds, which today's bind-into-the-document-env-and-undo
      design does not give: the persistent per-instance Env (110
      direct env.vars sites to move) is the prerequisite, and the two
      together are what would close the remaining gap.
    - **Integer element keys, steps 1–3 (of the five-step plan agreed
      with the reviewer: intern beside the string, kill hand-built keys,
      migrate the hot maps; columns and the persistent instance scope
      are 4 and 5).** keyid.go: a keyID is the element key interned once
      per document and cached on the node for its prefix; `here(id)` /
      `idIn(prefix, id)` find `prefix+id` through a two-level cache, so
      the 28 sites that concatenated them no longer do. Filed by id now:
      the element registry (elems/prevElems, registrations, idKeys), the
      id objects (elemObjs), geometry tokens, attribute overrides and
      their tokens, the subtree cache and its repeat counter, the
      hover/press refs, parent objects; elemObject.key, parentKey and an
      animation step's owner are ids. The string stays reachable through
      keyStr for tracing and for the state still filed by string
      (synthRefs' composite keys, tplObjs, scope snapshots). Every step
      compiler-checked, semantics unchanged (equal strings ⇔ equal ids):
      244 -> 222 µs per frame; check.sh green, sweep 0, cad canary
      identical, corpus 100.7 (scrollview alone moved 1.9 -> 2.2 and
      back to 1.8/1.9 on re-render: its scrollbar fade, not the change).
    - **Step 4, columns.** The same eleven stores are slices indexed by
      keyID (`col`/`ccol` in keyid.go: a read past the end is the zero
      value, a write grows, presence is the non-zero slot). Ids are
      interned in order of first sight, so the columns are dense; the
      registry rotation is a swap and a clear of the older slice, and
      the per-frame walks (beginFrame re-pointing every id object at its
      element, stampTokens over the geometry tokens, the geometry
      signature) index instead of hashing. 222 -> 207 µs per frame.
    - **Step 5, subtree patching (landed OPT-IN, GFX_PATCH=1).** The
      mechanism the whole plan was for: a cached parent whose OWN reads
      are fresh while a child's are stale keeps its element and redoes
      only that child in place. core.ReadSet gained OwnStale (own deps,
      nested sets aside — TrackReads no longer copies a child's deps up,
      only links the set), ReplaceSub and EachSub; ui gained ReplaceChild
      (in-place swap under any container). In gml every child placement
      that a parent can redo alone — buildChildren, anchored children,
      a styled control's delegates and style children — runs through
      docState.slot: its reads are one nested set, and the slot records
      the entry it produced, the element it landed as, how to redo it,
      and the build CONTEXT it ran in (buildCtx: row prefix, use site,
      current file, layout flags, `parent`, and the ancestors' scope
      bindings — declareRowScope, shadowControlScope, shadowSelfGeometry
      and a style root's id now push what they bind onto doc.bindStack,
      plus the names an ancestor re-pointed after binding). patch()
      re-establishes that context (refreshing captured objects' elements
      to last frame's), rebuilds the stale slots under TrackReads, swaps
      the elements, replaces the nested sets and the repeat counter is
      undone if it fails. registerElem no longer RECORDS a capture copy
      for replay (a cache hit re-registered the copy under the source's
      key: TestEffectCaptureKeepsTheSourceGeometry). Three tests that
      detected "a rebuild" by root-pointer identity count fresh builds
      instead; TestSubtreePatchKeepsAncestors covers the feature.
      On buttons with GFX_PATCH=1: **13 -> 3 rebuilds a frame, 207 -> 186
      µs, 40 -> 23 KB**; the three left are the animated ring and the two
      component USE SITES above it — a use site's element is its
      instance's, finished by the use site's own build (layout hints,
      wrappers, registration), so it cannot be swapped underneath (tried:
      a ProgressBar came back 200 wide where Layout.fillWidth had zeroed
      it). Why opt-in: GFX_PATCH_VERIFY=1 rebuilds every patched frame
      from scratch and diffs the trees (patchverify.go); buttons,
      indicators, frostglass and 49 others verify clean, but thirteen do
      not yet — a ListView rebuilt with no rows (aircraftmap, lists,
      settings), a Paragraph keeping a stale imposed width (asynccpp,
      multiplane, network, spritestack), a size left from the layout
      pass (drawers, lenstabs, portal), a text bound to a model count
      left empty (tableview, toasts) — and frostglass/portal move on the
      rank (2.0 -> 3.4, 1.9 -> 2.7) under patching. Default off, the
      corpus is unchanged (100.2, sweep 0, cad canary identical, gates
      green) and the benchmark is 204 µs. Next: work the thirteen down
      with the verifier, then flip the default.
    - **Step 5b: patching is ON by default.** The verifier got a baseline
      mode (GFX_PATCH_VERIFY=all: verify every frame with patching off),
      which showed eleven of the thirteen "mismatches" were the
      verifier's own — a from-scratch build mid-frame has no list rows,
      no imposed paragraph width, no model-count text — the same with or
      without patching. The two real ones, and the frostglass/portal rank
      moves, came down to three causes, all fixed:
      (1) **a component instance's cache entry never depended on the
      use-site values baked into it as literals** (`Canvas1 { width:
      parent.width }` evaluated at 0 on frame one was served at 0 for
      ever once nothing else in the instance moved — a pre-existing
      cache bug that the full rebuilds had been hiding; asynccpp,
      drawers, multiplane, network, portal and spritestack stopped
      mismatching the moment it was fixed). Every instance now carries
      an instance token (docState.instToken) stamped with a hash of the
      use-site literals and read by its build.
      (2) **doc.layoutGrid, doc.state, doc.curBehaviors and doc.curKey
      were not part of the captured build context**, so a slot rebuilt
      while a GridLayout ancestor happened to be rebuilding fresh kept
      its stated width where the layout should have zeroed it (lenstabs'
      slider; frostglass on a different frame). The context is also
      captured BEFORE the placement runs, not after.
      (3) **a placement that built an effect's capture copy is not a
      slot**: the copy reads the source's geometry through the source's
      tokens but is laid out inside the effect, and a patched copy sat
      where an earlier frame put it (frostglass' glass card).
      Gate, all fenced: every corpus page renders pixel-identical with
      GFX_NO_PATCH=1 and without (customitem and scrollview differ
      between two runs of the SAME mode — animation timing), the verifier
      finds nothing beyond its baseline on any page, check.sh green on
      linux/windows/js, sweep 0, cad canary identical, corpus 100.6.
      buttons: 13 -> 3 rebuilds a frame, 204 -> 191 µs, 40 -> 23 KB.

87. DONE: **the frosted pages blur through a MIP CHAIN, the way Qt's
    MultiEffect does** (render, ui). Corpus sum 96.5 across 49 comparable
    pages (96.2 before); the blur ITSELF is twice as close to Qt as it was.

    Three rounds (items 78, 83, 86) measured the same gap and each one
    ended at the same conclusion: what differs from Qt is not the blur's
    extent but the SHAPE of its falloff, and no constant fixes that.
    `Layer2D.BlurChain` is the renderer change those items were waiting
    for — the layer is downsampled through a chain of half-size targets
    and the levels are summed back, which is how MultiEffect makes a blur.

    Measured on a probe page written for it (a 200px magenta circle,
    `MultiEffect { blur: 1.0; blurMax: N }`, profile sampled every 10px
    across the middle, RMS against the Qt 6.10 render of the same file):

    | blurMax | before | after |
    |---------|--------|-------|
    | 60      | 5.6    | 2.1   |
    | 32      | 5.7    | 3.9   |
    | 16      | 11.3   | 5.4   |

    What it took, in the order the probe found it:

    - **A chain of plain downsamples is not a blur.** The first version
      halved the layer five times and summed the levels: a 32px square
      came back square (profile `0 0 8 157 254 254 ... 246 99 0`), because
      the linear sampler only ever gives a 2x2 box and upscaling that is a
      soft EDGE, not a falloff. Each level is a full 13-tap convolution of
      the level above it now, done in the SAME pass as the halving.
    - **The tap ring is sized so the chain's reach is the radius asked
      for.** The levels' radii double, so their sum is `2^levels - 1`
      texels; dividing the radius by that makes the reach continuous. A
      fixed ring quantized the blur to powers of two — at blurMax 32 our
      falloff ran 20px wider than Qt's while matching it at 60.
    - **Additive passes into an 8-bit target lose 2%.** A solid white
      layer came back at 250/255 — a dimming right across every frosted
      panel — because every one of the 13 taps is written back a shade
      low and the chain does that five times over. `addGain` (1.005,
      measured) compensates; the excess clamps away where the sum is
      already saturated. Drawing the taps as successive OVER lerps
      instead, which is exact for flats, is wrong for a premultiplied
      source: `over` scales the destination by the source's own alpha, so
      the transparent surround leaves the previous tap standing and the
      blur comes back far too sharp (probe RMS 42).
    - **A multisampled renderer needs a multisampled attachment per
      level.** wgpu rejects the pipeline otherwise ("the RenderPass uses
      textures with sample count 1"); each mip carries its own MSAA
      texture and resolves into itself.
    - **Below three or four levels the chain is a WORSE model than a
      plain convolution**, which is what the blurMax 16 row above says
      before the threshold went in: 10.2 through the chain against two
      passes' 5.4. `ui.chainMinBlur` (20 texels) picks between them, and
      the two together score 11.3 over the three probes where HEAD scored
      22.6 and an all-chain build 21.7.
    - **A blurred layer is never rendered smaller than half size.** The
      chain needs resolution to halve, and the two-pass blur turns out to
      need it too: at blurMax 16 a fifth-size layer scores 11.3 against
      half-size's 5.4.
      Tests: TestBlurChainPreservesFlats, TestBlurChainSpreads.

    **What it did NOT do: move the corpus.** shaders 2.2 → 2.1, frostglass
    and the other nine blurred pages unchanged, lenstabs 4.3 → 4.7 — a
    corpus of 96.2 → 96.5 for a blur that halved its own error against Qt.
    lenstabs is the whole regression and it wants the OPPOSITE of fidelity:
    it scores 4.1 when the layer is rendered at an eighth and blurred to
    mush, which our own probes say is nothing like Qt. Something else on
    that page is off — a radius we take from the wrong property, or a blur
    we apply where Qt does not — and the mushier blur was covering for it.
    That is the thread to pull next, not the blur.

86. DONE: **canvas and lenstabs: a radial gradient pads, and a wrapping
    label wraps at the width it is GIVEN** (ui, script/gml). canvas Δ4.3 →
    4.1; corpus sum 96.2 across 49 comparable pages. lenstabs is unchanged
    at 4.3, and the reason is now measured rather than guessed.

    - **Beyond its outer circle a radial gradient PADS with its last stop**
      (Qt and SVG both). The corpus' lit sphere is a circle of radius 35
      whose gradient is centred on a FOCAL point 14px off-centre, so the
      far crescent lies outside the gradient's own circle — and our ring
      fan painted nothing there, which made the rendered dome 60px wide
      where Qt's is 70. The shape is filled with the last stop first, then
      the fan draws over it.
      Test: TestShapeRadialGradientPadsBeyondItsCircle.
    - **A `Layout.fillWidth` wrapping label wraps at the width the COLUMN
      gives it.** A column asked to measure with nothing offered was
      re-measuring its children at the widest sibling's minimum — a pass
      meant for a column that has a width and needs more — and
      `heightFor` leaves that width on the paragraph, where it STUCK: an
      860px card description came back wrapped at 218, three lines too
      tall, pushing everything under it down the card. Probed against Qt:
      `A w 860 h 30` on both sides now, where ours was five lines at 215.
      Test: TestWrappingLabelGetsItsColumnsWidth.
    - **Measured and NOT landed — the same canvas-side padding.** A Canvas
      radial gradient pads too, in principle, but doing it there cost
      multiplane Δ2.9 → 10.2: that page draws its sky, fog and moon halo
      as radial ramps over paths far bigger than the ramps themselves, and
      the flat last-stop underneath swamps them. The declarative Shape
      path (above) is the one the corpus needs; the Canvas one waits for a
      real gradient primitive that can pad in the shader.
    - **Measured and NOT landed — widening our blur to Qt's extent.**
      lenstabs' frost bar is 25-58 levels BRIGHTER than Qt's wherever a
      blurred blob sits behind it (and pixel-identical where none does),
      because our blur keeps more energy near the source: probed with a
      200px circle at blurMax 60, Qt's falloff reaches 50px past the
      circle and ours 35. Widening each in-texture pass from half the
      radius to 0.7 matches the EXTENT (300..600 against Qt's 290..600)
      and takes lenstabs to 4.0 — but frostglass goes 2.0 → 2.6 and
      shaders 2.2 → 2.3, because the profile is then too diffuse; at 0.6
      it is 4.1/2.3/2.2. Half stays. This is the third measurement of the
      same gap (items 78 and 83 are the others): the difference is the
      SHAPE of the falloff, and closing it means blurring through a mip
      chain the way Qt does — a renderer change, not a constant.

85. DONE: **shapes and images: a stale render, two O(n^3) frames, colours
    that are not strings, and an image's own size** (render, gpu, ui,
    script/gml). shapes Δ3.6 → 0.8, images 3.3 → 1.0; corpus sum 102.5 →
    96.5 across 49 comparable pages, 48 of them under Δ5.

    - **The shapes number was STALE, and the metric never said so.** Its
      capture took FIVE MINUTES after item 83 made the headless run advance
      in 60fps frames (7.5x more frames), so it exceeded the regen script's
      60s timeout and quietly kept the PNG from three rounds earlier. The
      script reports a timeout now, and its budget is 150s. Two real
      performance bugs were behind the five minutes:
      - **Ear clipping was O(n^3)**: every candidate ear scanned the whole
        remaining ring. Only a REFLEX vertex can lie inside an ear, so the
        containment test now scans those alone (kept as a live set, updated
        where the ring changes, plus an AABB reject). The output is
        triangle-for-triangle what it was — the cad canary is pixel
        identical — and the page went 39s to 8.9s for twelve frames.
      - **The glyph atlas was re-uploaded WHOLE for every new glyph**: 4MB
        a frame on a page whose labels move sub-pixel and so keep asking
        for new quarter-pixel phases (90% of the frame time). gpu grew
        `WriteTextureRows` and the atlas records the band it grew, so an
        upload is the few rows that changed. 8.9s → 2.8s.
      Together: 12 frames in 2.8s instead of 39s (14x), and the full
      150-frame capture in 30s instead of 5 minutes.
      Test: TestBigOutlineFillsFast.
    - **A colour is not a string, and `.r` on a string is UNDEFINED.** That
      is JavaScript's rule and QML keeps it: a `property color` (or a
      Qt.rgba/Qt.color/Qt.darker result) is an object with components,
      while a `property string` holding "#00D1A9" is text. The corpus'
      Style.mainColor IS a string, so the shapes page's
      `fillColor: Qt.rgba(Style.mainColor.r, ..., 0.1)` is BLACK at 10% in
      Qt — we painted it teal, and probed it side by side to be sure
      (`color.r = 0 / g 0.8196078538894653` against
      `string.r = undefined / g undefined`). Colours now carry a colorVal
      tag through the evaluator; a palette entry is one, and the two
      value-kind switches that gate what may be frozen at a use site or
      called as a value accept it (missing that, every styled Button's
      flat text went white — caught by the corpus and fixed).
      Test: TestColourComponentsNeedAColour.
    - **An unsized image is its SOURCE's size** — QML's implicit size, and
      the floor a layout may not squeeze it below. A layout owns its child's
      size, so ours stripped the stated width/height and then fell back to a
      fixed 80x80 square: the images page's 9-patch tiles are 90x90 sources
      in a two-column grid whose columns are narrower than that, and Qt
      hands them their 90 and lets the grid OVERFLOW where we shrank them to
      the cell.
      Test: TestUnsizedImageMeasuresAsItsSource.
    - The dev knobs that found the perf bugs are kept: `GFX_CPUPROFILE`
      writes a CPU profile from a headless capture, `GFX_SETTLE_MS`
      shortens one (the profile needs a run that exits, not one a timeout
      kills).

84. DONE: **navdisplay and particles: a ListView is a layout item, and a
    particle fades on Qt's curve** (ui, script/gml). navdisplay Δ3.7 → 1.4,
    and the corpus sum 107.0 → 102.5 across 49 comparable pages (the
    ListView fix reached every page with a filling list). particles stays
    on its noise floor.

    - **`Layout.fillHeight` on a ListView reached NOTHING.** The fill
      switch had a case for every other layout item — Div, ScrollView,
      Grid, Canvas, Image, Label — but not for a list, so the navigation
      display's flight plan sat at the 200px a bare list defaults to,
      CENTRED in a slot nearly twice that: its first row landed 80px below
      Qt's and its last row fell off the bottom of the card. (ScrollView's
      case also still called the plain `Grow()`, so it ignored the
      preferred-size weight from item 81; both go through the same helper
      now.)
      Test: TestListViewFillsItsSlot.
    - **A particle fades in over the first TENTH of its life and out over
      the last QUARTER** — the constants in Qt's own particle shader
      (`fadeIn = min(t*10, 1)`, `fadeOut = 1 - min((t - 0.75)*4, 1)`).
      Ours faded in over a fixed 150ms and out over the last 30%. Probed
      with a single particle whose life far outlasts the capture: Qt's peak
      alpha rose 0.39 / 0.59 / 0.78 for lives of 100 / 60 / 40 seconds — a
      slope of ~10.5 per unit of life fraction, i.e. its shader's 10 plus
      an offset of about 0.13 the probe could not account for. The corpus'
      own particles live 1-3 seconds, where the fade-in is over before the
      capture, so this moved the page by less than its run-to-run noise.
      Test: TestParticleFadeFollowsQtsShader.
    - **particles is measurably right where it can be measured.** With a
      single deterministic particle: its rendered WIDTH matches Qt exactly
      at three sizes (60/30/20), its normalised falloff matches to within
      a few percent, its steady-state COUNT is Qt's (emitRate × lifeSpan =
      100 live on the basic emitter), and the colour-variation ratios were
      matched in item 81. What is left is placement, which is random on
      both sides — two references captured back to back differ from each
      other by Δ5.3, and we score 5.8.

83. DONE: **lenstabs and graphics3d: a blur spreads PAST its item, a
    captured copy needs its own texture, and content padding is per side**
    (ui, script/gml). lenstabs' blobs are round again and graphics3d's
    combo text starts where Qt starts it. Corpus RMS sum 108.3 across 50
    comparable pages, 47 under Δ5, worst page 5.7.

    **First, a correction to the numbers in items 79-82**: the sums quoted
    there (219.0, 184.7, 162.8, 159.0, 157.0) were computed with
    `awk '{s+=$1}'` over the ranking, which also added the trailing
    "N content pages" line — one page-count too high every time. The true
    figures are ~49-50 lower (this round's 108.3 is on the corrected
    metric, and rank.py now prints its own sum with a `#` prefix so the
    tail cannot be summed by accident). The per-page numbers and every
    before/after comparison in those items are unaffected.

    - **A blurred layer is PADDED by its blur** — Qt's MultiEffect pads the
      effect item (autoPaddingEnabled, on by default) so the blurred thing
      fades on outside the rectangle that cast it. Ours blurred inside the
      item's own texture, so a 200px circle blurred by 60 stopped dead at
      the circle's edge. Probed against Qt: its spread runs 290..600 for a
      circle at 350..550, ours now 320..580 with the same profile shape
      (the whole probe image at Δ0.68). This is the third attempt at the
      rule and the first that works: it only became correct once the blur
      moved entirely INTO the texture (item 75), because padding a texture
      whose blur also spread at composite time double-counted and went
      flat.
      Test: TestBlurredLayerSpreadsPastItsItem.
    - **A captured copy needs its own layer state.** An effect's capture
      builds the same markup a second time, in the effect's coordinate
      space; both copies asked for the state under one key, so each
      overwrote the other's texture and the composite drew the capture's
      content at the page-space rect. That is what put a hard rectangular
      edge around every blurred blob on the lens-tabs hero — the padding
      above made it obvious, the bug predates it.
      Test: TestCapturedLayerHasItsOwnState.
    - **A contentItem's padding is per SIDE.** A ComboBox's text is
      `leftPadding: 12; rightPadding: 30` (room for the arrow) and we
      applied the larger of the two to both sides, indenting the text by
      30 where Qt starts it at 12 — visible on every styled combo in the
      corpus.
      Test: TestContentPaddingIsPerSide.
    - Measured on graphics3d, where the panel now outweighs the 3D
      viewport (panel Δ14.3 against the scene's 6.6): the bold labels are
      the largest single contributor (the title band alone reads 41). Both
      engines draw the same face at the same size and position — the
      difference is in the RASTER, our stems landing softer across two
      pixels where Qt's hinter snaps them. That is the corpus' text floor,
      not a bug to fix here. What did close on that page: the combo
      padding above, and the arrow glyph is still 5px where Qt's is 9 (our
      symbol fallback picks a smaller triangle for U+25BE).

82. DONE: **diorama and canvas: a drop shadow is a SILHOUETTE, a Shape's
    RadialGradient is round, and a canvas font has a weight** (render, ui,
    script/gml). diorama Δ5.3 → 4.1, canvas 4.4 → 4.3 (with far more of the
    page actually right). Corpus sum 157.0 across 50 comparable pages, 47
    under Δ5, worst page 5.7 — particles, on its own noise floor.

    - **A drop shadow is the shape of what casts it, not a darker copy.**
      Ours tinted the layer's texture, and a MULTIPLY cannot colour
      anything dark: the diorama's trees are black and their shadow is
      `rgba(0.08, 0.02, 0, 0.7)`, so every shadow on the page came out
      black instead of warm brown. The renderer has a silhouette pipeline
      now — `fs_stencil` paints a texture's COVERAGE in a flat colour
      (`DrawImageStencil`, `DrawImageBlurredStencil`, batch kind
      kindStencil) — and a layer's shadow goes through it. Probing the
      falloff also settled the radius: at `shadowBlur` 0.25/0.5/1.0 of the
      default blurMax, Qt's shadow fades over 7/12/23px and ours over
      8/16/32, so our nominal radius is right at small values and about a
      third too wide at the top of the range.
      Test: TestStencilDrawPaintsTheShapeNotTheContent.
    - **A Shape's RadialGradient paints by DISTANCE, with every stop.** It
      was painted as a two-colour linear ramp along the item diagonal, so
      the canvas page's lit sphere (white → amber → orange, focal point
      offset from the centre) was a flat wash of pale peach. It now goes
      through the same ring fan a Canvas radial gradient uses, clipped to
      the path, from the focal circle to the centre circle; a LinearGradient
      with more than two stops keeps them all as well.
      Test: TestShapeRadialGradientKeepsItsCirclesAndStops.
    - **A canvas font spec has a WEIGHT and a slant**, not just a size —
      `ctx.font = "bold 22px sans-serif"` appears 29 times in the corpus (a
      clock's numerals, a chart's centre label, this page's "Canvas!").
      Only the size was read, so all of them drew in the regular face. The
      spec's family, weight and slant now resolve through the styled-face
      registry, with an over-strike where no real bold exists.
      Test: TestCanvasFontSpecReadsWeightAndSlant.
    - **A Canvas shadow* state is painted.** shadowColor/shadowBlur/
      shadowOffsetX/Y were accepted and dropped; text and paths drawn while
      they are set now get the blurred, offset copy underneath (a ring of
      low-alpha copies, the same cheap halo a glowing Text uses).
      Test: TestCanvasShadowStateIsRead.
    - **A row divides its width in proportion to its items' CONTENT** when
      they fill and state no preference — item 81 covered a stated
      preference, and Qt's stretch factor defaults to the size hint either
      way. Probed with four labelled columns of implicit width
      45.5/22.6/79.4/32.7: Qt gives them 210/105/365/150 and we now give
      211.5/104.8/366.9/146.8 (an equal split had them all at 207.5, which
      is why the shapes on the canvas page sat up to 16px from Qt's).
      Test: TestRowSharesWidthByContent.
    - Measured and NOT adopted: the same rule on the HEIGHT axis. A
      column's preferred height is height-for-width, and our first pass
      measures a card before its width is settled — weighting by that
      measurement collapsed cards whose content had not resolved (the
      canvas page's gradient card lost everything below its title, Δ4.6 →
      10.9). Widths only until the layout has real per-item size hints.

81. DONE: **toasts and particles: a layout shares its surplus in
    PROPORTION to preferred sizes, and an emitter that fills its system**
    (script/gml, ui). toasts Δ5.7 → 1.2; particles is at its noise floor
    (see below) but its colours are Qt's now. Corpus sum 159.0 across 50
    comparable pages, 47 under Δ5, worst page 5.7.

    - **Surplus is divided among the filling items in proportion to their
      preferred sizes** — Qt's stretch factor defaults to the size hint, so
      a `Layout.preferredHeight: 72` tile strip beside a bare
      `Item { Layout.fillHeight: true }` takes essentially the whole
      column. Ours split the leftover equally, which left the toasts page's
      deploy-preview tiles at half Qt's height with the panel empty
      beneath them. Probed against Qt three ways — 72+0 → 608/7, 72+24 →
      461/154 (exactly 3:1), no preferences → 308/307 — and ours now reads
      604.6/8.4, 459.8/153.2, 306.5/306.5. A filling item's preference is
      no longer its size, it is its WEIGHT: `GrowBy` on the element (added
      to Canvas/GridEl/TextEl/LazyImageEl alongside DivEl's).
      Test: TestLayoutSharesSurplusByPreferredSize.
    - **`colorVariation` LERPS each channel toward a fresh random one.**
      Ours jittered around the base (`c ± v`), which clamps half the range
      away whenever a channel sits at an end — the corpus' particles are
      `#00D1A9`, whose RED IS ZERO, so we were drawing half of Qt's pinks.
      Probed with a four-panel page (variation 0/0.3/0.6/1.0) and read as
      the mean red over each panel: Qt rises 0.00 → 0.20 → 0.44 → 1.06 of
      its own green, we now rise 0.00 → 0.20 → 0.46 → 0.96, where before
      the ratio was half that. At variation 1 the colour is fully random
      whatever the base was, which is what the lerp says and the jitter
      does not.
      Test: TestColorVariationLerpsTowardRandom.
    - **`Emitter { anchors.fill: parent }` emits over the system's whole
      box** — and the box is a LAID-OUT one, so the rect has to be resolved
      when a particle spawns, not when the emitter is read. A document
      whose particles are its only geometry never rebuilds (nothing feeds
      the geometry signature), so the emitter stayed a POINT at the origin:
      one blob in the corner where Qt fills the panel. The paint callback
      is where the laid-out box arrives, so the system records it there,
      and it registers its own element so that box reaches the geometry
      signature too.
      Test: TestEmitterFillsItsSystem.
    - **A particle part only has to NAME its system.** QML lets an Emitter
      or an ImageParticle live anywhere in the tree with `system: sys`;
      ours only read the system's own children and warned "unknown element
      Emitter" for the rest. They are collected at LOAD time now, keyed by
      the id they name — during the build is too late, since such a part
      usually sits after its system and the frame that registered has
      already snapshotted its signatures (so no rebuild follows).
      Test: TestParticlePartsBoundBySystem.
    - **particles is at its noise floor.** The page is random particles:
      two references captured back to back differ from EACH OTHER by Δ5.3,
      and ours scores 5.6 and 5.7 against them. There is nothing left there
      that RMS can see. What was measurable — the mean colour of a panel —
      now matches: mean red 29.8 against the references' 31.4 and 31.5,
      where before the fix it was 27.3.

80. DONE: **graphics3d and diorama: what `parent` means inside a
    ScrollView, and a layout's invisible items** (script/gml, scripts,
    ui/shell). graphics3d Δ24.5 → 4.4, diorama 9.1 → 5.3, and the harness
    fixes below took portal 8.0 → 2.0 and pfd 3.5 → 1.5. Corpus sum 162.8
    across 50 comparable pages, 46 of them under Δ5, worst page 5.7.

    - **A child of a ScrollView means the CONTENT ITEM by `parent`**, not
      the box the view sits in: its width is availableWidth (the viewport
      minus padding), or contentWidth where the markup states one — a
      Flickable's content item is as big as its content, not as the window
      onto it. graphics3d's control panel sizes its whole column that way
      (`ColumnLayout { width: parent.width }`); reading the panel instead
      made every control in it 320 wide where Qt gives 270, so each one
      overflowed the clip — the ComboBox indicators were CLIPPED AWAY
      (they were drawn, just past the right edge) and the slider handles
      sat ten pixels right of Qt's, because the style computes the handle
      from the slider's own width (`visualPosition * (width - calcDim)`).
      Probing it printed the numbers side by side: `sv.width` and
      `availableWidth` were right all along (270), only `parent.width` was
      wrong.
      Test: TestScrollViewContentIsWhatParentMeans.
    - **A layout gives an item with `visible: false` no slot and no
      spacing.** Ours built an empty element in its place, which still ate
      a gap: the diorama's collapsed maths panel cost its scene 12px of
      height, and every layer canvas is sized off that scene. Only layouts
      and positioners skip it — everywhere else an invisible item still
      EXISTS, with geometry. Skipping it outright first cost Δ17 on the
      shaders page, whose three MultiEffects take a hidden rounded square
      as their `source` and `anchors.fill` it: dropped, they had no source
      and filled the whole card.
      Test: TestLayoutSkipsInvisibleItems.

    Three harness findings, all of them "the reference was not where it
    said it was":

    - **Grab off an ANIMATION, not off summed frame times.** The host
      accumulated `frameTime` and grabbed at 2.5s; a probe page printing
      its own `eulerRotation` showed Qt's clocks at **2.917s** — the long
      frame that instantiates the page reaches the page's tweens but not
      the sum. A host-side `NumberAnimation { duration: 2500 }` shares its
      clock with the page's animations, so the grab lands at exactly 2.500
      (probed: `euler.x 180.0  euler.y 225.0`, twice in a row). That alone
      is most of graphics3d's Δ20: its cube was 37° further round than
      ours.
    - **Instantiate the page TWICE.** A page that clocks ITSELF off
      frameTime with the corpus' clamp (`Math.min(frameTime, 0.05)`)
      cannot recover a long frame, so the portal's shader clock read 2.035s
      while the tweens in the same page were at 2.5. Qt caches compiled
      pipelines per process: loading the page, dropping it and loading it
      again puts the measured instance on a warm cache — 2.035 → 2.393s
      (the plateau; the last 0.1s is the re-instantiation's own two long
      frames). Waiting longer than 800ms between the loads changes
      nothing.
    - **Repaint every Canvas after the second load.** The corpus idiom is
      `onAvailableChanged: if (available) requestPaint()`, and on the
      second instantiation the target can already be available when the
      handler connects — so the signal never comes and the canvas stays
      blank. pfd's speed/altitude tapes vanished from the reference that
      way (Δ1.5 → 3.5 until the host asked every Canvas in the page to
      paint once).

    And two things measured rather than fixed:

    - **Our headless capture now advances in 60fps FRAMES** (150 steps of
      16.7ms) instead of 20 slices of 125ms. Phase-neutral — `tickTimers`
      already sub-sliced each slice into frame-sized steps, which is why
      the portal render came out bit-identical either way — but the render
      loop now runs one frame per animation frame, as a real run does.
    - **A `Timer { interval: 16 }` page has a ±3 reference noise floor.**
      spritestack advances 1.2° per TICK, and Qt's tick count varies with
      what the machine is doing: three references captured back to back
      differ from each other by Δ2.9, and our single deterministic render
      scores anywhere from 3.1 to 8.0 against them. Nothing to fix on our
      side; the page's own numbers are what they are.

79. DONE: **aircraftmap: completion sees a laid-out tree, and a Repeater's
    rows keep their own positions** (script/gml). aircraftmap Δ7.1 → 3.2,
    and the page's zoom label now reads Qt's exact `46%`. Corpus sum
    184.7 across 49 content pages; 45 of them under Δ5.

    - **`Component.onCompleted` runs once the tree HAS GEOMETRY, not after
      the second build.** QML sizes the item tree during instantiation, so
      a handler reads `flick.width` as freely as anything else — the page's
      `fitToView()` divides by it. Ours only has geometry after a layout,
      and the build counter got it wrong in both directions: a frame builds
      TWICE (the layout pass and the paint pass each ask for a view), so
      "after the second build" still ran the handlers against zeros, while
      the geometry SIGNATURE cannot be the gate either — it folds each
      key's name in, so it is non-zero the moment anything is registered,
      bounds or not (`anyLaidOut` answers the real question). The queued
      handlers also hold the objects of the build that queued them, whose
      elements were registered but never laid out, so the scope is
      re-pointed at last frame's elements first — and that has to overwrite
      an UNLAID-OUT element, not only a nil one, which is what the earlier
      "fill in the nil ones" pass missed. What a handler changes is then
      rebuilt inside the same View() call, so a model it filled (QML's
      imperative `append` idiom) is visible on the frame that completed the
      document, as in Qt. Handlers in a document that never gets geometry
      at all still run, at a four-build cap.
      Tests: TestCompletionSeesLaidOutGeometry, and the six existing
      completion tests now drive two frames because that is what the
      semantics say.
    - **A Repeater's rows are the children that get POSITIONED.** Inside a
      plain Item — an absolute container, not a positioner — every row
      carries the delegate's own `x`/`y`, and they are per-row expressions
      off the model's roles (`x: posX * imageItem.width - width / 2`).
      They were placed at the origin instead, which piled all nineteen
      blueprint markers in one spot in the corner. The anchor pass now
      builds each row through the same slot a plain child gets
      (`buildAnchoredChild`), inside the row scope so the row's values are
      the ones the expressions see; and `hasAnchorChildren` looks THROUGH a
      Repeater at its delegate, or the container never entered anchor mode
      in the first place.
      Test: TestRepeaterRowsKeepTheirOwnPosition.
    - Still open on the page: the markers sidebar fits seven rows to Qt's
      eight (our list rows are a hair taller), which is what most of the
      remaining Δ3.2 is.

78. DONE: **spritestack and particles: warm the reference's window before
    its page** (scripts, script/gml, ui). spritestack Δ6.3 → 1.8, particles
    6.2 → 5.5, portal 5.3 → 0.7, diorama 35.2 → 9.1.

    - **The harness warms the WINDOW before the page exists.** The Loader
      starts inactive and is activated once two frames in a row come in
      under 25ms. Mapping a window, building the scene graph and compiling
      the first shaders makes the early frames long, and a page that caps
      its own step (`Math.min(frameTime, 0.05)`, the corpus idiom) loses
      the excess of every one of them where a declarative animation does
      not — so the two kinds of page ended up at DIFFERENT times, and the
      drift varied per run (item 74's noise floor: three captures of
      graphics3d differing from each other by up to Δ12). Instantiating
      onto a warm scene starts every clock a page owns together, which is
      the state our headless capture is in from its first step.

      It also gave **diorama its sky back**: that Canvas never repainted in
      a cold window, which is the "reference-side blank" items 67 and 68
      recorded — Δ35.2 → 9.1 with no engine change at all.

    - **A non-finite length is nothing.** A binding can divide by a zero it
      read before the thing it divides by exists, and JS answers Infinity
      or NaN. Neither is a length: they poison every rect derived from
      them, and a layout handed one walks an infinite box — the aircraftmap
      blueprint took **40 seconds a frame** once its implicit size was
      real. Qt treats such a value as nothing, and so does `attrFloat`.

    - **An Image's implicit size is its SOURCE's**, as in QML, whatever box
      the item lands in; an anchored item forwards its child's, since an
      anchored item is still the item. A page that sizes a box FROM that
      number — `Item { width: img.implicitWidth }` around an anchor-filled
      Image, which is how the blueprint viewer sizes its pan area — read
      back the zero it was waiting on.

    Corpus: 44 of 49 comparable pages under Δ5 (43 before).

    **OPEN, and now the corpus' worst delta (aircraftmap Δ109.4): a
    `Component.onCompleted` handler cannot read geometry.** Qt sizes its
    item tree during instantiation, so a handler divides by a real width;
    we only have LAST frame's rects, and a file component's own ids have
    none at all (probed: `flickable.width` and even the component root's
    `width` read 0 in the handler). The blueprint viewer's `fitToView()`
    therefore lands on 0 and the page renders at 0% zoom where Qt shows
    46%. Its reference used to be as blank as ours — that is why this page
    read Δ3.3 for so long — so the number went UP as the comparison got
    honest.

    Running the handlers one build later was tried and reverted: it fixes
    the divide (the page stops rendering NaN) but not the zoom, because a
    component instance's ids still carry no element, and it costs every
    page a frame of un-initialised state. The fix is geometry for
    component-instance ids, which is a piece of work rather than a tweak.

77. DONE: **lensphoto: an Image hands a shader its own texture** (script/gml,
    scripts). Δ13.0 → 3.1, and the reference's pointer is no longer part of
    the picture.

    - **In Qt an Image is a TEXTURE PROVIDER.** `ShaderEffect { source:
      someImage }` samples the decoded image WHOLE — the item's fillMode,
      its clipping and its size play no part — where we captured a snapshot
      of the item, which showed the photo cover-cropped. Measured against
      the source file: Qt's render matches a synthetic full-image STRETCH
      (err 8.4) and not a synthetic cover-crop (28.0); ours matched the
      crop (9.0) and not the stretch (28.8). That is a tenth of the
      picture's scale down its middle, which is what the page's whole delta
      was. The image's own cell feeds the sampler now, including when the
      value arrives through a wrapping component's declared property (how
      that page passes it: `LensEffect { source: photoImage }`).

    - **The harness parks the desktop POINTER** before each GPU reference
      (`examples/xwarp`, two X calls through our own ffi layer). A real
      window on a real display gets the actual mouse, so a page that
      follows `mouseX` rendered wherever the pointer happened to sit — the
      lens was off-centre in every reference, and cards2d5' first card came
      out hovered (items 69 and 76 recorded both as reference artifacts).
      Our headless capture has no pointer at all, so a corner is the state
      both sides agree on.

    - Also: an `ImageCell` re-decodes for a materially bigger box. A photo
      first painted while the layout was still settling kept that small
      decode, upscaled, for ever — in real steps (a box that grows a little
      every frame does not re-decode every frame), and never once the
      decode is already the source's own resolution.

    Corpus RMS sum 219.0 → 211.6, 43 of 49 comparable pages under Δ5.

    **graphics3d Δ14.7 is phase, and its noise floor (item 74) is why it
    reads anywhere from 9 to 18.** Sweeping our capture's settle budget
    puts the match at 2700ms (Δ6.34) against 2500ms (14.73), so ~8 of it is
    the reference having run ~200ms longer in animation time. At the
    matched phase the cube's rotation and the sphere's shading line up; the
    sphere is 8% smaller and 6px off, which is its own levitation
    animation at a slightly different point (two animations at different
    rates cannot both be matched by one settle time).

76. DONE: **cards2d5: the default sans is the face the SYSTEM resolves**
    (ui). Δ5.9 → 2.1, and frostglass 2.9 → 1.9 with it.

    A current Ubuntu resolves "sans-serif" to Ubuntu **Sans**, which is what
    Qt and a browser pick; our fallback list tried the older Ubuntu face
    first. Their typographic metrics differ by a pixel per line at 15px
    (Ubuntu asks for 932/189 where Ubuntu Sans asks for 940/260), so every
    multi-line paragraph in the corpus wrapped to Qt's line breaks and then
    sat a pixel per line tighter: cards2d5' three-line subtitle came out on
    a 17px pitch against Qt's 18, and the drift accumulated down the page.
    Ubuntu Sans goes first now, with its italic companion registered — and
    no static bold, so bold stays synthetic on a variable-only system
    (item 69).

    Worth noting HOW this became visible: the same experiment before item
    72 (line metrics honouring the face's own USE_TYPO_METRICS bit) read
    183.5 against 184.1 over the corpus and was recorded as
    measured-and-rejected. With the metrics right it reads 174.9 against
    170.0. A rejected experiment is worth re-running when the thing it
    depended on changes.

    Corpus RMS sum 224.5 → 219.0, 42 of 49 comparable pages under Δ5.

    **toasts stays at Δ5.7**, and its tile strip's height is the whole of
    it: Qt's surplus distribution, recorded in item 73 as needing per-item
    min/preferred/max hints. Re-running the base-weighted rule confirmed
    the same failure from another angle — the states page's button row then
    spreads to the card's full width and overflows it, because the "base"
    our layouts have is a CONTENT size rather than Qt's preferred size.

75. DONE: **frostglass and multiplane: a translucent layer, and a blur
    that sums** (render, ui, script/gml). frostglass Δ6.5 → 2.9, multiplane
    5.7 → 2.9, lenstabs 5.8 → 4.6.

    - **An offscreen target holds PREMULTIPLIED pixels** — that is what
      rendering into a cleared target produces — and we composited them
      with a straight-alpha blend, multiplying by the alpha a SECOND time.
      An ambient blob painted at alpha 0.3 reached the frame at 0.084, a
      third of Qt's. There is a premultiplied sprite pipeline for it now
      (`fs_premul`, `kindPremul`, `DrawImagePremul`), with its own bind
      group per image as every auto-derived pipeline layout needs.

    - **A blur is a weighted SUM of samples, not a stack of `over`
      composites.** Compositing the taps gains alpha the source never had
      (the same 0.3 became 0.55 after one pass and 0.75 after two). The
      in-texture pass is additive now (`fs_premul_add`, weights normalized
      to one, into the cleared ping-pong half) — a true convolution — so
      the WHOLE blur happens there, two applications of the kernel, and the
      composite is one ordinary premultiplied draw. Probed against Qt: a
      300px circle of `rgba(0.8,0.1,0.45,0.3)` blurred by 80 plateaus at
      R=69 where Qt reads 70; ours read 29 before.

      This also settles the "blur profile" item 72 left open: with the
      arithmetic fixed, the falloff matches (44/68 against Qt's 40/65 at
      the shoulder) — what looked like a mip-chain-versus-gaussian
      difference was mostly the alpha being applied twice.

    - **A QML Gradient may have any number of stops**, and a quad's ramp
      holds four: the extra stops collapsed to "the first three plus the
      last", which ran the corpus' five-stop mist straight from its third
      stop to the end and left it twice as bright through the bottom third.
      `GradientQuadStopsN` draws a band per segment — exact, since a QML
      Gradient is always axis-aligned.

    - **Qt's `Context2D.ellipse(x, y, w, h)` takes a BOUNDING BOX** where
      the HTML canvas takes a centre and two radii (Qt ignores the extra
      arguments a page written HTML-style passes). So the multiplane
      clouds belong to the lower right of the point they name, at a quarter
      of the size; ours centred them there and drew them twice as big. Two
      more fell out of the same page: a canvas radial gradient is now
      CLIPPED TO THE PATH (the ring fan paints the gradient's whole circle,
      which is routinely far bigger than the shape it fills — the ramp
      itself had become the cloud, a soft disc across the whole sky), and
      `PushClipPath` flushes an open subpath the way a fill does. Without
      that last one the clip was empty, and an empty clip hides everything.

    Corpus RMS sum 227.2 → 224.5, 41 of 49 comparable pages under Δ5.

    Left on the two pages: multiplane's Δ2.9 is the moon's halo and a few
    pixels of parallax; frostglass's Δ2.9 is the glass card's own blur
    radius. And a Qt quirk worth knowing, measured on the way: in an
    isolated probe Qt's Canvas draws `fillStyle = "rgba(160,175,230,0.16)"`
    NEARLY OPAQUE (162,174,230 over black) — the alpha reaches neither the
    fill nor `Qt.rgba()`. The same construct inside the corpus page is
    faint, as ours is, so this was not copied.

74. DONE: **spritestack and particles: a button that never looked active,
    and the phase-noise floor** (script/gml, scripts).

    - **A control's own state is in scope for its own attributes.**
      `highlighted: checked` on a checkable Button is the QML idiom for
      "look active while I am checked" — item scope, where a bare name
      resolves against the item's own properties first. We evaluated a
      control's use-site attributes with nothing bound for its contract
      state, so `checked` resolved to nothing and every such button drew
      unhighlighted: the particles page's three affector buttons ALL looked
      selected, because none of them looked active. The instance's live
      state (checked/value/currentIndex) is created before its attributes
      are evaluated now, and they evaluate in a scope that binds those
      names. particles Δ5.7 → 5.5, with the Gravity button's fill matching
      Qt's highlighted lighter teal (0,240,194) exactly.
      Test: TestUseSiteAttrReadsTheControlsOwnState.

    - **spritestack's residual is phase, and phase has a noise floor.**
      Its six stacks are driven by one `Timer { interval: 16 }` whose tick
      count follows the frames rendered; sweeping our capture's settle
      budget puts the match at 2300ms (Δ1.97) against 2500ms (Δ5.93), so
      essentially all of its delta is the reference having accumulated
      ~0.2s fewer ticks in its 2.5s window.

      Where those 0.2s go, measured: a page that caps its own step
      (`Math.min(frameTime, 0.05)`, the corpus idiom) LOSES the excess of
      every long frame, and the first frames after a window is mapped are
      long. Clamping the harness accumulator the same way makes it track
      such a page exactly (probed: the portal's own clock reads 2.4856 when
      the accumulator hits 2.5020) and it is a big win for those pages —
      portal Δ4.9 → 0.50, spritestack 5.9 → 1.97. It was still REJECTED:
      the clamp grabs ~0.25s later in WALL time, which is what a
      declarative animation runs on, and graphics3d — a `NumberAnimation`
      rotation that never settles — went Δ4.7 → 27.8. The corpus is better
      with the raw accumulator. Emulating a cold first frame in our own
      capture (one long, un-sub-stepped slice, so a clamping page loses the
      same time Qt's does) would satisfy both; our stepping deliberately
      breaks a tick into frame-sized slices (item 68), so that is a change
      to the capture's contract rather than a tweak.

    - **And a page whose animation never settles has a NOISE FLOOR in this
      metric.** Three consecutive reference captures of graphics3d differ
      from each other by up to Δ12.4 (refA↔refB 12.37, refB↔refC 5.23), and
      its delta against one unchanged render of ours reads 9.1, 12.65 or
      17.57 depending on which reference you caught. Read the number for an
      unsettled page as ±10, or compare the pictures.

73. DONE: **toasts and states: a Canvas is a layout item** (ui,
    script/gml). states Δ6.1 → 0.9, toasts Δ6.3 → 5.7.

    - **A QML `Canvas` in a layout ignored its layout hints** and sat at
      the 300x150 an HTML canvas defaults to. The states page's easing plot
      is `Layout.fillWidth: true; Layout.preferredHeight: 60`, so ours came
      out half as wide and twice as tall as Qt's — a steep diagonal where
      Qt draws a shallow curve. `ui.Canvas` takes W/H, Grow and CrossFill
      now, and gml applies the hints to it like any other item (dropping
      the size on a filled axis so the layout can stretch it, as it does
      for a box).

    - **A LAYOUT inside a layout FILLS by default.** Qt's
      `Layout.fillWidth`/`fillHeight` default to false for an item and TRUE
      for a layout, so the toasts page's tile strip — a RowLayout stating
      `Layout.preferredHeight: 72` whose tiles fill — grows past 72 rather
      than leaving the panel empty beneath it. The default is applied only
      when one of the layout's OWN items asks to fill that axis (a
      Repeater's delegate counts), which is when Qt's default is
      observable; defaulting both axes unconditionally cost the same page
      its outer structure (Δ6.3 → 18.9).

    What is left on toasts is the SURPLUS DISTRIBUTION, and it is recorded
    rather than fixed. Qt hands every filling item its preferred size and
    shares the surplus in PROPORTION to those preferred sizes — a filling
    item that asks for nothing takes almost none of it. Measured against Qt
    6.10: a strip with `Layout.preferredHeight: 72` beside a trailing
    `Item { Layout.fillHeight: true }` in a 380px column takes **330** to
    the spacer's **4**, where our even split gives each about half (the
    tiles come out 100 tall against Qt's 194). Two implementations were
    measured and rejected: surplus proportional to each grower's base, and
    the same with a one-pixel weight for zero-preferred items (which is
    what Qt's 330/4 implies). Both regressed pages that depend on the even
    split — states 0.9 → 5.8, layouts 1.2 → 2.4, ecam/pfd +0.3 — and
    neither moved toasts, because our per-item bases are CONTENT sizes
    rather than Qt's size hints. Getting this right means giving every item
    a real min/preferred/max hint triple (Qt's QGridLayoutEngine model),
    which is a project rather than a fix.

    Corpus RMS sum 232.9 → 227.2, 40 of 49 comparable pages under Δ5.

72. DONE: **lenstabs and portal: metrics, gradients and a fair clock**
    (render, scripts). Two engine fixes and one harness fix; both pages
    were dominated by things that had nothing to do with their shaders.

    - **The FACE decides which line metrics it wants.** OS/2 fsSelection
      bit 7 (USE_TYPO_METRICS) says "use my TYPOGRAPHIC
      ascender/descender", and FreeType — so Qt — honours it; otherwise the
      WINDOWS ascent/descent apply. Item 59 measured the win metrics
      against Quicksand (which does not set the bit) and we applied them to
      everything, so Ubuntu Sans — the system default face here, and what
      Qt resolves "Sans Serif" to, asking for 940/260 where its win metrics
      say 1020/223 — put our baseline 2px low and our line box 1px tall at
      20px, on EVERY unstyled label in the corpus. Probed against Qt:
      implicitHeight 16 at 13px and 24 at 20px, both exact now, and the ink
      rows line up. cards2d5 Δ11.4 → 9.0.
      Test: TestWinMetricsHonourUseTypoMetrics.

    - **Gradients interpolate PREMULTIPLIED**, as Qt's and CSS's do. A
      plain component lerp passes through colours neither stop has — from
      opaque green to a faint red the midpoint is full red at half alpha
      (measured 129 of red where the end stop contributes at most 51) — and
      a stop fading to `transparent` drags the colour toward black as well,
      so the composited ramp curves away instead of running straight down
      to the background. lenstabs' page-wide glow reached the background by
      y=240 where Qt's runs to 280.
      Test: TestGradientInterpolatesPremultiplied.

    - **The reference is grabbed after 2.5s of ANIMATION time**, not of
      wall clock. Mapping a window and instantiating a page costs ~0.5s
      here, and the page's own clock only starts after them: probing the
      portal's `time` uniform at the wall-clock grab read 1.99s where our
      headless capture had advanced the full 2.5s — a fifth of a turn of
      the swirl. The host now accumulates `FrameAnimation.frameTime` (the
      same quantity our side steps) from the frame the page loads and grabs
      at 2.5. portal Δ7.8 → 4.9.

    Corpus RMS sum 239.6 → 232.9.

    Measured and NOT landed, all three worse than what we have:
    - **Qt's `MultiEffect { autoPaddingEnabled }`**: padding the layer
      capture by the blur radius, so the blur can spread past the item's
      box. It is what Qt does, but our blur spreads by drawing the texture
      at offsets rather than by sampling outside it, so the padding only
      lets the in-texture pass bleed and the result goes flat: lenstabs
      Δ5.85 → 7.02, frostglass 6.49 → 6.73.
    - **Widening the composite spread to the full radius** (from 0.7): the
      extent then matches Qt exactly (a 60px blur of a 240px circle reaches
      70..428 against Qt's 70..429), but Qt's blur is a mip-chain — flat
      across the whole spread with a fast edge, where ours is a gaussian —
      so the wider version is more diffuse and scores worse (lenstabs 5.85
      → 7.04). The profile, not the extent, is what is left.
    - **Ubuntu Sans as our default face** (what fontconfig resolves for
      "sans-serif" here, and what Qt uses): 183.5 → 184.1 over the corpus.
      Our GPUI-style pick of Ubuntu stays.

    What is left on the two pages: portal Δ4.9 is the last ~0.2s of phase
    (the page's own FrameAnimation starts a few frames after its item
    exists, which the host cannot see), and lenstabs Δ5.8 is the blur
    profile above plus a couple of pixels of accumulated card height.

71. DONE: **the "card slack" was four defects** (render, ui, script/gml).
    Item 70 left images Δ9.0 and gridview Δ6.0 filed as "our cards come out
    a few pixels taller and it accumulates". Measured, none of it was
    slack: the cards' panels, titles and controls line up to the pixel in
    both engines.

    - **A ListModel filled in its own `Component.onCompleted` reached
      nothing.** QML builds a model imperatively that way (`for (...)
      append({...})`) — the gridview page's dynamic grid — but a model is
      DECLARED, never built, so nothing collected its lifecycle handler and
      the grid drew no cells. Models queue their own completion handlers
      now, with the model's methods resolvable BARE (as on any QML object),
      and an inline model with no id keeps ONE instance across rebuilds so
      the appends are not undone by the next declare pass.

      Two memoization holes hid the result and are closed with it. The
      document re-read its observable signature AFTER running completion
      handlers, so anything they changed was invisible until something else
      moved (completion runs once, so forcing one more rebuild cannot
      loop). And a model mutation was not visible to the PER-SUBTREE
      read-sets at all: rows live outside the property system, and marking
      the read untracked would rebuild every model-reading view every
      frame, so `listModel.ver` makes a mutation a tracked write instead.
      gridview Δ6.0 → 1.3.

    - **dropCollinear decimated curves.** It dropped any vertex that looked
      flat between its two neighbours — and on a finely flattened curve
      EVERY sample looks flat, so the dropped error piled up into a visible
      polygon: the Qt logo's Q was a dodecagon at 300px and smooth at
      600px, since the tolerance is in the path's own units and an SVG
      arrives already scaled to the screen. The test is anchored to the
      last KEPT point now (0.02px), so an exactly-straight run still
      collapses to its ends — which is what the ear clipper needed it for.

    - **A grid track never shrinks below its content's minimum.** Qt keeps
      a 90px item 90px wide in a column too narrow for it and lets the
      layout OVERFLOW — measured: two 90px cells in a 156px grid sit at 0
      and 108, running past the edge. Ours scaled every track down in
      proportion, which squeezed the BorderImage samples to 79 and moved
      everything under them.

    - **A vector source honours fillMode.** `Image.Stretch` scales the two
      axes INDEPENDENTLY (`DrawSVGScaled`); `PreserveAspectCrop` covers and
      clips. A uniform scale cannot express either, so the FillMode demo's
      stretched Qt logo sat 22px short of its frame. images Δ8.5 → 3.4.

    Also: ListEl and ScrollEl expose their children to `ui.Children`, so a
    walk of a built tree reaches what is inside a view instead of stopping
    at it.

    Corpus RMS sum 249.9 → 239.6. Left on images, recorded not fixed: Qt
    rasterizes an SVG at `sourceSize` (100x100 here) and stretches THAT, so
    the reference's logo is blurry where ours stays vector-sharp. Matching
    it means rasterizing a vector source at sourceSize and upscaling — the
    faithful reading of the property, at a deliberately worse-looking
    result.

70. DONE: **a glow that is one colour, and a list whose rows are their own
    height** (render, ui, script/gml). The rest of the newly comparable
    pages, and the deltas they were really made of.

    - **A colorizing effect colorizes the whole GROUP.** A standalone
      `MultiEffect { source: item; blurEnabled: true; colorization: 1 }` is
      the corpus' neon glow: the source is a teal rounded rectangle with a
      WHITE star on it, and Qt draws the result all one colour. Ours
      recoloured only an `Image` or an `SVG` element, so the star stayed
      white in the middle of a teal blur. A layer can colorize its own
      capture now (`ui.LayerEl.Colorize`, composited through the colorize
      pipeline with the blur taps — `DrawImageBlurredColorized`), and gml
      applies it to a blurring effect's layer as well as to `layer.effect`.
      shaders: Δ10.2 → 2.5.

    - **A ListView's rows are as tall as their delegates.** QML has no
      uniform row height: the DELEGATE's height IS the row height, and a
      delegate bound to its own data (a chat bubble as tall as its message)
      differs row by row — ours laid every row out at the first one's
      height. `ui.ListEl.Measured` learns each row's height as it is built,
      keeps it in the `ListState`, and positions the visible rows from what
      it has learned in the SAME layout pass (a height learned a frame late
      shows as a jump). The height passed to `List` stays the estimate for
      rows never built, so virtualization is untouched.

    - **`verticalLayoutDirection: ListView.BottomToTop`** puts index 0 at
      the bottom with later rows stacking above it (`ui.ListEl.BottomUp`) —
      how every chat log in the corpus is written. The enum members also
      had to join the ListView ATTACHED-property object: that object
      shadows the `ListView` namespace for a row's duration, so a view
      built inside a row could not resolve `ListView.BottomToTop` at all
      (it evaluated to nil and the attribute was silently accepted).

    - **An anchored wrapping Label reports the height it wraps to.** The
      anchor resolver gives a height-for-width child its height at the
      width the anchors imposed, so a bubble sized off `label.height` is no
      longer a line short.

    - **A line that exactly fits does not wrap.** WrapText's running sum
      adds each word and each space on its own, which can total a float32
      hair more than the same text measured in one piece — so every box
      sized to `label.implicitWidth` (the QML idiom) wrapped. It verifies
      the whole candidate line at the margin and re-syncs the sum. The
      drift needs a CFF face to reproduce (Quicksand here); the test
      asserts the property.

    chatui: Δ8.3 → 2.8. Corpus RMS sum 263.8 → 249.9 across the two items.

    Measured and NOT landed: running OUR renders with Qt's own default font
    (`GFX_FONT=DejaVuSans.ttf` — Qt's "Sans Serif" here, where ours picks
    Ubuntu as GPUI does). It is the more faithful comparison for any text
    with no `font.family`, and it does help cards2d5 (-1.0), but it costs
    more than it buys on the Controls pages (drawers +0.8, toolbar +0.6,
    combobox +0.4): 221.7 → 224.4 over the corpus. Left as a note rather
    than a harness change.

    What the pages still show, all recorded rather than fixed:
    - **cards2d5 Δ11.4**: bold. This system ships Ubuntu ONLY as a variable
      font, so a real bold face does not exist for it here (item 69) — and
      the reference's first card is hovered, because Qt hands it the real
      desktop pointer (item 69 again).
    - **images Δ9.0, lenstabs Δ7.9, gridview Δ6.0, states Δ6.1**: our cards
      come out a few pixels taller than Qt's and the difference accumulates
      down the page. Per-item metrics agree (a 34px Quicksand Bold title
      measures 164.86 to Qt's 164.86, ink within a pixel); what differs is
      how much slack a column leaves around its content.
    - **diorama Δ33.1**: reference-side. Its sky is a Canvas that never
      repaints in the bare `qml` host.

69. DONE: **the newly comparable pages: a crop that never cropped, a clip
    that ignored its transform, and bold that was not bold** (render, ui,
    script/gml). Item 68 made the shader pages comparable; working them
    turned up three defects with reach well past the pages that exposed
    them.

    - **`fillMode` was read as a NAME and arrives as a NUMBER.**
      `Image.PreserveAspectCrop` evaluates to `2`, so a
      `strings.Contains(s, "PreserveAspectCrop")` test was false for every
      page in the corpus: every cropped photo was letterboxed inside its
      frame instead of covering it, and `Image.Stretch` was ignored the
      same way. `fillModeFlags` reads the enum numerically (and still
      accepts the name as a string). diorama went from Δ42.4 to Δ33.1 on
      this alone, and lensphoto's photo finally frames like Qt's.
      Test: TestFillModeFlagsReadsTheEnumNumber.

    - **A clip did not travel through the transform stack.**
      `PushClip` turned its box straight into a framebuffer scissor, so a
      clip issued INSIDE a layer's offscreen pass — where the child paints
      in window coordinates and a transform maps them onto the target —
      landed at double the offset. The cropped photo feeding lensphoto's
      shader was cut at (20,85) inside its own texture and the lens drew a
      photo sliding out of its frame. `clipBox` maps the rectangle through
      the current transform (its bounding box under a rotation or shear,
      since a scissor has no other option) and scales the corner radius
      with it. Test: TestPushClipFollowsTheTransform.

    - **Synthetic bold was one 0.6px over-strike at any size**, which at a
      32px heading is invisible: cards2d5' title carried 1978 pixels of ink
      where Qt's carried 3089. It is now a small dilation whose spread
      follows the size (`synthBoldStrikes`), and a WRAPPING paragraph gets
      bold at all — `Paragraph` looked up a styled face only for a NAMED
      family (so the default font's real bold was never used) and never
      over-struck, so every bold description line drew regular.

      The honest gap behind it: this system ships Ubuntu as a VARIABLE font
      and nothing else — `Ubuntu-B.ttf`, `-M.ttf`, `-C.ttf` are all
      symlinks to `Ubuntu[wdth,wght].ttf`, whose default instance is
      Regular (usWeightClass 400, which is why registerStyledFaces
      correctly refuses them). Qt instantiates the `wght` axis and gets a
      real bold; we cannot, because the glyph source (x/image/font/sfnt)
      has no variable-axis support. Closing it properly means
      pre-instancing a variable face — applying `gvar` deltas (with IUP
      inference) to `glyf` and re-emitting a static TTF for sfnt to parse.
      NOT DONE, recorded here: until then a synthetic bold is what a
      variable-only system gets, and it is calibrated to land near the real
      face's ink rather than near its outline.

    - **lensphoto's residual is the REFERENCE's mouse.** Its lens follows
      `mouseX/mouseY` and keeps `lensX/lensY: 0.5` until a move arrives.
      The GPU reference runs on a real display, so Qt delivers the actual
      desktop pointer: probing the reference host shows the window at
      (116,119) and the pointer inside it at (252,349), which is exactly
      where the reference's lens sits. Ours keeps the declared default.
      Δ15.9 there is the lens being in two different places, not a defect —
      and the same caveat applies to every hover-driven page in GPU_PAGES.

    - Measured and NOT landed: putting a synthetic pointer at the window
      origin during a screenshot (the offscreen platform reports one there).
      It buys nothing — a QML `mouseX` is 0 before any event either way —
      and it switches on the hover state of whatever sits under (0,0):
      lenstabs went from Δ7.9 to Δ8.5 with it.

68. DONE: **a ShaderEffect runs its shaders** (gpu, render, script/gml).
    The one large feature item 67 left open: the corpus' custom GLSL — the
    portal's swirl, the lens indicators, the shader gallery — which this
    engine did not run at all. It ran nothing custom because WGSL is the
    only shader language here and there is no GLSL compiler.

    There does not need to be one. **A Qt app ships its shaders
    COMPILED**: `qsb` turns each .vert/.frag into a .qsb at build time and
    the app carries that. So:

    - `render/qsb.go` reads the container — zlib around a Qt data stream —
      lifts the **SPIR-V** variant out and reflects what the shader
      declares. Offsets and shapes come from the SPIR-V; the NAMES come
      from the container's own reflection, since qsb strips debug info
      from the binary. Neither reader tracks the versioned layout: the
      SPIR-V is found by its magic word behind Qt's byte-array length, and
      a name is matched to the member the SPIR-V reports at the same
      offset and size.
    - `gpu` gained `CreateShaderModuleSPIRV`. wgpu runs SPIR-V through
      naga's frontend like any other shader, so it is validated, not
      passed through; the web backend says no (WebGPU takes only WGSL).
    - `render/shaderfx.go` builds the pipeline and draws the quad INLINE
      in the 2D pass — its own pipeline and bind group, so a batch of its
      own — blending premultiplied over what is there and taking the
      scissor in force. The vertices go in CLIP space with qt_Matrix left
      as the identity, so the item's transform and DPI scale arrive
      through the same toNDC as every other primitive.
    - `script/gml/shaderfx.go` fills the uniform block Qt's convention
      defines: qt_Matrix, qt_Opacity, then the effect's own properties BY
      NAME — read from the element's own object, since that is what a
      handler writes (`portalEffect.time = ...` from a FrameAnimation).

    **Two pieces of SPIR-V surgery** were needed to make wgpu accept real
    Qt shaders:

    - a GLSL `sampler2D` compiles to a **combined image sampler**, which
      WebGPU's binding model has no place for (a texture and a sampler are
      separate) and naga rejects the module outright. The rewrite says the
      same thing the Vulkan way: the variable becomes an IMAGE, a sampler
      variable is added beside it, and every load of the old variable
      becomes load-image + load-sampler + OpSampledImage keeping the
      original result id, so nothing downstream changes. The new
      declarations go at the END of the globals section, where every type
      they name is defined — putting them earlier reported "invalid id".
    - both stages must declare the **same** uniform block or wgpu rejects
      the pipeline ("not consistent between stages"): Qt compiles each
      stage against only the members it uses, so a vertex shader that just
      multiplies by qt_Matrix ends up with a shorter block. The shorter one
      is padded with float members to match.

    And the source a shader samples is a real capture: the item its
    `source` names is rendered into an offscreen layer — the machinery a
    blur already used — which the shader then reads (QML's
    ShaderEffectSource with hideSource; here the capture is a sibling that
    paints nothing). A layer's texture had to become bindable for that:
    `ImageFromView` now keeps the view it was handed (marked borrowed, so
    Release still frees only what it made).

    Two more fixes the pages needed: a document steps in FRAME-sized
    slices, because a page that accumulates per frame (`time +=
    Math.min(frameTime, 0.05)`) saw one 125ms slice where a 60Hz run sees
    eight frames — the portal's shader clock reached 1.0s where Qt's was
    at 2.5s; and `anchors.horizontalCenterOffset` / `verticalCenterOffset`
    place a centred item, which left the swirl 40px low.

    portal Δ45.4 → 7.7, and the lens page draws its lens.

    Where the shaders come from at run time: our ShaderEffect wants the
    .qsb the way Qt's does. The corpus ships only the GLSL sources (its
    CMake compiles them), so `scripts/qml-compare.sh` compiles them for
    both sides and points ours at them with GFX_QSB_PATH — a
    colon-separated search path for compiled shaders, which is what a
    build hands us.

    Not modeled: wgpu reports a shader it cannot accept through the
    device's error callback and still returns an object, so gpu counts
    those errors and render treats a change across the calls as "that did
    not work" — an optional pipeline built from shaders we did not write
    has to be allowed to fail without taking the process down.

67. DONE: **the last lap: honest references, and the deltas they exposed**
    (render, ui, script/gml, scripts). Item 66 left a diagonal-axis
    rotation unsheared; closing it turned into a sweep of everything the
    ranking still had, and most of it came down to *what we were comparing
    against*.

    **A quad carries a SHEAR now.** The SDF quad shader took a rotation
    angle, so any transform with a shear in it drew turned but unsheared —
    which is exactly a `Rotation { axis { x: 1; y: 1 } }`, whose
    orthographic projection IS a shear. `shearDecompose` splits any 2x2
    into the scale, shear and rotation the shader can apply (QR, which
    loses nothing); the vertex stage shears before it rotates and the
    fragment stage undoes both before the distance field, so the corner
    radius and border shear WITH the quad, as Qt's rasterize-then-transform
    does. Eight axis/angle pairs land within a few px of Qt, the needle
    case included.

    **The reference itself was the biggest delta.** Qt's offscreen platform
    always loads the SOFTWARE scene graph, which draws a layered item, a
    ShaderEffect, QtQuick.Particles and QtQuick3D as NOTHING. Half the
    corpus' most interesting pages were being compared against a blank
    picture. `scripts/qml-compare.sh` now renders those pages' references
    on a real display (GFX_REF_DISPLAY, default :1) — and deliberately
    keeps the rest offscreen, because the X11 platform picks up the
    DESKTOP's palette and every Basic control's greys shift with it (an
    unchecked TabButton reads 221 there, 159 offscreen); comparing those
    pages on a GPU would measure the theme, not us. `GFX_REF_GPU=1` forces
    it everywhere. With honest references: diorama 81.9 → 27.2, frostglass
    31.6 → 6.6, lenstabs 25.8 → 9.9, cards2d5 15.0 → 6.4.

    Three more things the bare `qml` tool could not load, all of which the
    harness now patches into its private copy of the pages:

    - a page's own **qrc asset paths** ("qrc:/assets/images/particle.png"
      for an ImageParticle's texture) — Qt logged "Cannot open" and drew
      nothing, so every particle card's reference was empty;
    - its **shaders**: a ShaderEffect names "qrc:/qt/qml/<module>/shaders/
      x.qsb", which CMake builds from the .vert/.frag sources the corpus
      ships. The harness compiles them with Qt's own `qsb` — that is what
      finally made the lens and portal pages draw;
    - its **own modules**: a qmldir's `prefer :/qt/qml/<module>/` line
      points Qt at the qrc resource for its sibling components, so the bare
      tool reported every card as "Type unavailable" — 17 pages' references
      were blank because of that one line. The shaders page compares for
      the first time.

    **A 3D scene was frozen at its first frame.** The encoder draws from a
    list the viewport is APPENDED to when its target is created, and every
    rebuild produces a fresh scene object with new model transforms — which
    nothing published. Not a headless artefact: an interactive window
    showed the same frozen scene. graphics3d 38.1 → 4.7 once the entry is
    replaced.

    **Qt.lighter takes its overflow off the SATURATION.** QColor::lighter
    scales the HSV value and, when that clips, subtracts the excess from
    the saturation — which is what turns a bright colour whitish instead of
    just clamping its channels. Ours scaled RGB, so a lightened teal stayed
    #00FFFF where Qt gives #79FFE5, and the sprite-stack page's lit slices
    were cyan against Qt's near-white. Both functions now match Qt to the
    byte (measured on #00D1A9 at x1.2, x1.8, x2.2).

    **An ImageParticle draws its sprite.** QML particles ARE sprites — the
    corpus' particle.png is a soft radial glow — and drawing a hard dot was
    most of what was left of the particles page (14.1 → 5.9).

    **`contentWidth: -1`** means "adapt to the content": the content keeps
    its own width instead of being stretched to the viewport, which a page
    whose content sizes ITSELF depends on.

    **QtQuick3D, four causes at once.** The projection was already exact
    (a probe of five models at known positions matched to the pixel), and
    everything else about the 3D page was wrong:

    - a SPHERE's triangles were wound the wrong way, so back-face culling
      kept the FAR hemisphere and shaded it through inward-facing normals.
      A white sphere came out grey and every sphere in a 3D scene read
      flat — including the applications built on it;
    - `lighting: DefaultMaterial.NoLighting` was ignored because the enum
      namespace did not exist, so the expression failed silently;
    - a DefaultMaterial is NOT a PBR material. Ours shaded it through the
      PBR path, which tone-mapped every colour down. MEASURED off a Qt
      render: the unlit side of a white sphere under `ambientColor: 0.2`
      reads 50/255 and the lit crown clips at 255 — `clamp(0.2 + N·L)` in
      DISPLAY space, no tone map, no gamma. A PrincipledMaterial keeps the
      PBR path, as Qt shades those;
    - a light casts shadows only when asked to (`castsShadow` defaults
      false), so the cube above the sphere no longer darkens it.

    **A value-type animation writes the WHOLE property.** `SequentialAnimation
    on position.y` on a Model at x=200 renders it at x=0 — dead centre —
    because Qt builds a default-constructed vector carrying only the
    animated component, and the STATIC remainder is lost. SEVERAL
    animations on components of one value type all apply, though: Qt drives
    them through a value-type proxy that merges their writes (MEASURED: a
    cube with animations on eulerRotation.x AND .y renders at both angles,
    281x227 wide where either alone gives a different silhouette). My first
    reading of this was "the last declared wins", which the probe above
    disproved — a stopped animation simply writes nothing, which is how the
    3D page picks one of three float styles on position.y.

    **Animations were not running at all in a capture.** A linear-ramp
    probe (a rect moving 800px over 4s, so its x IS the elapsed time) read
    **t=0** where Qt's reference was at 2510ms. Three causes:

    - an animation attached to a property — `NumberAnimation on x { ... }`,
      QML's property-value-source form — RUNS BY DEFAULT. We defaulted it
      to stopped, so every attached animation in the corpus was inert;
    - a NumberAnimation's default easing is **LINEAR**, not InOutQuad. At
      62.5% of a ramp ours read 71.9%;
    - a document with anything moving now SAYS so (core.BumpRevision): an
      animator writes attribute overrides rather than core Properties, so
      nothing bumped the revision and the headless capture — which renders
      only while state changes — stopped after two frames.

    The capture's clock is virtual now too (core.SetVirtualClock): time
    flows only through the settle slices, 20 × 125ms = exactly the 2500ms
    Qt's reference waits, instead of also counting this process' startup
    and GPU init. A moving page is sampled at the same phase on both sides.

    **A nested grid caps at its own content.** MEASURED: two 280px cards in
    a `Layout.fillWidth` 2-column GridLayout sit at 20 and 324 — the grid
    is 584 wide, not the row's full width. Ours took the whole row and
    spread the surplus across its columns. It is the same layoutMax rule a
    nested Row/ColumnLayout already had; a column's cross-axis stretch
    honours it now (the main axis already did). Rows still share their
    surplus — also Qt (a 2-row grid of 60px cells in 300px spaces them 140
    apart, each centred in its track) — the difference is not the axis but
    whether the grid was given more room than it can use.

    Reference-side, recorded rather than fixed: **diorama's sky.** Qt's
    render leaves it black, and instrumenting the page shows its sky Canvas
    never paints at all — it is the one layer with `layer.enabled: false`,
    and a plain Canvas in the bare `qml` host does not repaint after being
    resized, while every layered sibling does. A minimal probe of the same
    component renders fine, so this is the host's timing, not the page's
    intent: ours draws the sunset the page describes.

    Where that leaves the ranking: **30 of the 38 comparable pages under
    Δ5**, and the eight above it are named causes rather than mysteries —
    portal 45.4 and part of the shaders page's 10.2 are a custom GLSL
    ShaderEffect, which we do not run at all (the honest fix is to feed the
    .qsb's SPIR-V to wgpu, or translate GLSL→WGSL — a feature, not a bug
    fix); diorama 42.4 is the reference's own unpainted sky; spritestack
    11.7 is its rotation phase; frostglass, lenstabs, cards2d5, particles
    and multiplane sit at 5.5–6.6 on blur spread, glow tint and the phase
    of a running animation.

    Tests: TestShearDecomposeRebuildsTheMatrix,
    TestSphereMeshWindingFacesOutward, TestQtQuick3DMaterialModes,
    TestValueTypeAnimationClobbersItsSiblings,
    TestValueTypeAnimationsAllApply,
    TestAttachedAnimationRunsLinearByDefault,
    TestGridCapsAtItsContentWhenNothingFills,
    TestQtLighterDarkerFollowQt.

66. DONE: **Rotation honours its axis** (render, ui, script/gml).
    `Rotation { axis { x: 0; y: 1; z: 0 }; angle: a }` turns an item about an
    axis in the PLANE, which tilts it away from the viewer. We ignored
    `axis` and spun everything in-plane, so the transforms page's 3D card
    and the card flip turned like a Z rotation instead of foreshortening —
    the last open lead from item 60.

    MEASURED against Qt 6.10 (a probe page of eight axis/angle pairs, read
    off the reference's own pixels): a 100px square turned 30 / 60 / 80
    degrees about Y is **87 / 50 / 17 px wide and FULL height** — exactly
    cos(angle), and the other dimension untouched. Qt applies no
    perspective unless one is set, so the projection stays affine: the 2D
    transform is the top-left 2x2 of the 3D rotation matrix (Rodrigues),
    which is what an orthographic projection of the turned plane IS. Past
    90 degrees the factor goes negative and the item mirrors, which Qt does
    too (a 120-degree Y turn shows its label back-to-front).

    Two bugs surfaced with it, both wider than this page:

    - **A sprite quad transformed only two of its corners** and paired
      their coordinates — valid only while the transform keeps the quad
      axis-aligned. Under a rotation it mixed one corner's x with another's
      y, which tore every glyph of a rotated text into fragments and lost
      the quad entirely at some angles. All four corners go through the
      transform now (glyphs, images, and the italic-skew path).
    - **`transform:` / `rotation` / `scale` on a Text or Label were
      rejected**, and a rejected attribute SKIPS the element: a rotated
      caption did not draw at all. A Text carries its painting transforms
      now, on a wrapper that leaves layout alone as QML's do.

    Left open: a rotation about a DIAGONAL axis (`axis { x: 1; y: 1 }`)
    shears, and the SDF quad shader takes a rotation angle rather than a
    general matrix, so we draw that case turned but unsheared. Qt shows a
    parallelogram; giving the shader a full 2x2 also means deciding what a
    corner radius and a border width mean under shear.
    Tests: TestAxisRotationProjectsOrthographically,
    TestSpriteQuadRotatesAsAWhole, TestTextCarriesItsTransforms.

65. DONE: **a Text wraps only once a width is imposed on it** (ui,
    script/gml). The open lead from item 60 — "a styled Label with no width
    still measures nothing outside a layout" — turned out to be a sharper
    rule once probed against Qt: **QML breaks a Text's lines only when a
    width is IMPOSED on it** — an explicit width, anchors pinning BOTH
    sides, or a layout told to stretch the item. With nothing imposing one
    the text stays on a single line, overflows its parent, and its own
    width IS that line (measured: a wrapping Label in a 400px card runs
    580px wide and one line tall, in Qt).

    We wrapped whenever a container happened to offer a width, so a plain
    child of an item broke lines Qt keeps whole — and reported width 0
    until some box reached it, which is what the lead had seen.

    The markup is the only place that rule can be applied, and by the time
    a styled control reaches the text builder it is the STYLE FILE's node:
    the use site's anchors and `Layout.*` attrs have both been stripped
    into wrappers, so neither is visible where the decision has to be
    made. The two passes that strip them now record what they imposed
    (`node.imposedWidth`), and a file-component instance inherits it from
    its use site. Getting that wrong is expensive and quiet: the first
    version of the rule read the style's node, found no hints, and stopped
    every card caption in the corpus from wrapping — ecam Δ1.5 → 10.2,
    layouts Δ1.2 → 8.5, hud, pfd, navdisplay, canvas, lists and animations
    all worse. Nothing in the ranking moves with it applied correctly.

    A note on probing this: an ad-hoc probe outside the corpus tree gets
    the BASIC style, where `Label` is a plain Text — the styled path it is
    meant to exercise never runs. Put the probe page in
    `~/workspace/qml-examples/examples/<name>/Main.qml` and drive it
    through `scripts/qml-compare.sh <name>`, which is what makes the two
    sides comparable at all.

    Two smaller fixes from the same probe:

    - **A paragraph nothing gave a box to** lays out at its text's width
      rather than at 0.
    - **A Text's `contentWidth`/`contentHeight`** (and the `paintedWidth`
      aliases) are readable. Reading one failed as "no member", which
      SKIPS the whole element that read it — a bar sized off a label's
      content width never built at all. The corpus' ToolTip style sizes
      itself `contentWidth + leftPadding + rightPadding`.

    Tests: TestWrapNeedsAnImposedWidth, TestTextContentSizeIsReadable,
    TestParagraphWithNoBoxIsAsWideAsItsText.

    Also settled while probing: a popup does NOT clip its content — Qt
    lets a content line too long for the panel run past it, to the window
    edge, and so do we (a clip looked right until the reference's pixels
    said otherwise).

64. DONE: **a component file's ids are ITS ids**, plus the JS its canvases
    needed (script/gml). The animations page logged two warnings that were
    a scoping bug: `BreathingCircles { id: breathSection }` instantiates a
    component whose own tree declares `Item { id: breathSection }`. QML
    scopes a component file's ids to that file, so the inner one wins
    inside the component — we pointed the name at the INSTANCE ROOT
    instead, and every read of the inner item's own state failed
    (`breathSection.time` from its Canvas: no member "time"; MatrixRain's
    `matrixSection.columns` the same). A use-site id that the component
    itself declares now leaves the component's own object and binding
    alone, and shares the instance's properties into the page's view of the
    id so reads from OUTSIDE still resolve.

    With the ids fixed, both canvases drew — and each turned up a JS defect
    the warnings had been hiding:

    - **`%` is fmod, not integer modulo.** We truncated both operands, so
      a hue written `(0.47 + i * 0.04) % 1` came out 0 and every breathing
      ring painted RED instead of cyan. It also made spritestack's
      `(rotation + 1.2) % 360` advance 1° a tick instead of 1.2, and
      multiplane's clouds (`(baseX + t * speed) % 1.2`) all sit at the same
      x.
    - **A radix literal is a number.** `0x30A0` lexed as `0` followed by an
      identifier `x30A0`, so the matrix rain's katakana never resolved.
      0x/0b/0o all parse now.
    - **String is a function AND a namespace.** `String.fromCharCode(...)`
      needed members on a value that is also callable (a new callableNS).
    - **A field of a plain JS object is assignable.** `cols[c].chars = [...]`
      had no Ref to write through; objects are reference types, so the key
      is now written in place.

    Both canvases match Qt's shape and palette (checked by instantiating
    the two components on their own beside the Qt render). Two page numbers
    moved UP as a result — spritestack Δ8.0 → 10.8, multiplane Δ3.3 → 5.5 —
    because both animate continuously and now advance at Qt's rate rather
    than a truncated one: what is left is the phase the two engines sample
    at, not a static difference.

63. DONE: **a style's icon tint (MultiEffect colorization), and two Qt
    width rules** (render, ui, script/gml). The buttons page's ToolButton
    icons were the last static gap on it: the qmlsnippets style tints its
    icon with `layer.enabled: !!root.icon.color` and
    `layer.effect: MultiEffect { colorization: 1; colorizationColor:
    root.icon.color }`. We drew the source untouched — a WHITE gear where
    Qt shows a teal one, a PURPLE cap where Qt shows teal — because our
    only recolouring was a tint MULTIPLY, which can darken a source but
    never recolour it (teal x purple is mud, not teal).

    So colorization draws for real: the source's own luminance in the
    stated colour, keeping its alpha. Three pieces:

    - **A second sprite pipeline** (`fs_colorize` in the sprite module, a
      `kindColorized` batch). It needs its own bind groups at both group 0
      and group 1: wgpu treats an auto-derived pipeline layout as
      EXCLUSIVE to its pipeline, so reusing the sprite ones is a validation
      error, not a shortcut. An image builds its colorize bind group on
      first colorized draw, so images that are never tinted pay nothing.
    - **An SVG is colorized CPU-side**, mapping each shape's fill/stroke
      through the same luminance rule as it is drawn — no texture at all.
    - **gml applies it to the ELEMENT**, not through an offscreen layer:
      a colorize-only effect needs no capture, so `layer.enabled` with
      nothing but colorization stays a plain element (`ui` SVGEl /
      LazyImageEl `Colorize`).

    Getting a reference to compare against took a detour worth recording:
    **`QT_QPA_PLATFORM=offscreen` always loads the SOFTWARE scene graph,
    where an item with `layer.enabled` renders as NOTHING** — the whole
    static corpus' references are drawn that way, so Qt's own buttons page
    shows no ToolButton icons at all. Rendering the page on the real X
    display (`DISPLAY=:1`) gives a true reference, and that is what the
    numbers below are against. The same limitation is why the pages at the
    top of the ranking (diorama, frostglass, graphics3d, lenstabs,
    cards2d5) compare against a reference that draws none of their effects.

    One Qt layout rule fell out of the comparison, and one wrong guess
    worth recording:

    - **A wrapping row (QML Flow) can do without the width its children add
      up to** — it breaks them onto more lines — so it must not report
      their sum as its minimum. It asked for 625 for four 145px buttons,
      its card's column obliged (a Qt layout works in the space it
      REQUIRES), and the third button ran over the neighbouring card
      instead of wrapping under the first two. A wrapping row now requires
      only its widest child.
    - The wrong guess: that a RowLayout PACKS its items at the left and
      shares nothing (a probe of 120/120/40 items in a 480px fillWidth row
      put them at 0/135/270, exactly packed). It does share, the same way a
      column does — two 126px items in a 360px row anchored to fill land at
      0 and 193, each cell taking half of the 83px surplus. The probe was
      packed because a NESTED layout is capped at its own content width
      (the layoutMax rule, already implemented), so it had no surplus to
      share in the first place. Acting on the guess cost the pfd page
      Δ1.5 → 3.0 before the sweep caught it: when a measurement and an
      existing rule disagree, check whether the OTHER rule explains it.

    And an SVG Image sizes from its DECLARED size, not its viewBox: the
    style sizes its icon `width: Style.resize(sourceSize.width)`, and a
    48x48 icon with a `viewBox="0 0 960 960"` measured 0 there, so the
    styled icon vanished before any of this could tint it.

    buttons page: Δ6.4 → 1.5 against the GPU reference (Δ5.5 → 2.2 against
    the software one, which draws no icons to compare).
    Tests: TestDrawImageColorized, TestMultiEffectColorizationIsRead,
    TestSpreadCellsPacksARowAtTheLeft,
    TestWrappingRowRequiresOnlyItsWidestChild.

    Open lead found on the way: the animations page logs two warnings
    (`no member "time"`, `no member "columns"`) because a file component
    declares an id its INSTANCE also carries — `BreathingCircles { id:
    breathSection }` around an inner `Item { id: breathSection }`. Ids are
    keyed bare, so the two objects collide and the outer one wins; in QML
    the component's own scope wins inside the component.

62. DONE: **SequentialAnimation and ParallelAnimation really compose**
    (script/gml, and one measurement in app). The animations page's
    composition card sat almost still: our animator drove ONE property, so
    a `SequentialAnimation { NumberAnimation ColorAnimation
    NumberAnimation }` moved nothing past its first step and a
    ParallelAnimation ran one child.

    An animator is a list of STEPS with a mode now. A sequence gives each
    step its slice of the pass in turn — the ones before it held at their
    end, the ones after it untouched until reached, which is what makes a
    sequence read as one move after another — and a parallel one advances
    them all together. The pass is the sum for a sequence and the longest
    for a parallel, and loops/start/stop/restart/complete work on the whole
    of it.

    The rest of what a real sequence is made of:

    - **PauseAnimation** (time passing is the whole of it),
      **PropertyAction** (an instant set at the point it sits in the
      sequence) and **ScriptAction** (once per pass) are steps too.
    - **ColorAnimation interpolates for real**, in RGBA between the colour
      the property held when the step began and its `to`. A colour is not a
      number, so `to: Style.mainColor` used to be dropped outright.
    - **The `on <prop>` form on a container**: `SequentialAnimation on
      opacity { NumberAnimation { from: 1; to: 0.3 } ... }` — the blink
      idiom, and how the corpus flashes a critical battery — has children
      that state no target of their own and drive the CONTAINER's property.
      That property is usually a built-in one (opacity, x, scale), which is
      layout-derived rather than a property, so it is written as an
      attribute override on the element the animation sits in. Nodes carry
      that owner now: the parser links each child to the element it sits
      in, which a non-visual child had no way to know.

    animations page: the sequential/parallel card Δ13.6 → 5.0, the spring
    card Δ14.5 → 1.6.

    And one more number measured rather than assumed: **a wheel notch is
    72px**. A QtTest case sending one notch (an angle delta of 120) to a
    ListView and a Flickable moves each 72 pixels, and notches add up
    linearly — Qt scales the angle delta by 0.6. Ours was 48, then 60 from
    the wheelScrollLines convention; at 72 the lists page's wheel step
    lands on its static baseline (Δ8.2 → 3.0).

61. DONE: **dialogs, popups and menus draw through the app's style**
    (script/gml, ui). Every other Controls kind already routed through the
    application's style file; the FLOATING ones did not, so an app with its
    own style got our chrome — a light card and a Basic button box — over
    its dark page. On the popups page that was a third of the window
    wrong: Δ37 for an open dialog, now Δ1.8.

    A styled popup composes the style's own parts — `background:`, and a
    Dialog's `header:` and `footer:` bands — around the page's content,
    while the popup itself still owns what it always did: its open state,
    its imperative API, its signals and where it sits. That state moved
    out of buildPopup into a `popupState` both paths share.

    Measured from Qt rather than guessed (a probe printing the real
    geometry, as usual): a dialog is its style's 400 wide, its header 48
    tall (a band takes its stated implicitHeight), its content inset by
    the style's padding, and its DialogButtonBox 64 tall with 12px padding
    and 188x40 buttons that TRAIL (alignment: Qt.AlignRight). The scrim's
    alpha comes from the style's own `Overlay.modal` rectangle.

    Menus followed: the Menu style states the panel, the MenuItem style
    states each row, an `Action` among a menu's children becomes the
    MenuItem it stands for, and choosing a row runs its handler (a styled
    MenuItem's signal is `onTriggered`) and closes the menu. `popup()`
    with no coordinates opens at its TRIGGER — QML opens it at the
    pointer, and the element whose handler is running is the one under it.

    Attached **ToolTips** work now too (`ToolTip.text` / `.visible` /
    `.delay` / `.timeout` were accepted and ignored), drawn through the
    app's ToolTip style and floated above the item, centred on it. Their
    timing is real: Qt shows a tip only once the pointer has RESTED for
    that item's delay, so three buttons asking for 0/500/1500ms no longer
    all show at once.

    Two more differences the same page-by-page reading exposed, both worth
    more than they look:

    - **A QML view has no scrollbar of its own.** One is attached with
      `ScrollBar.vertical`, and Qt's own is visible only while the view
      moves — so ours drew a permanent line down the side of every page.
      `ui.ScrollEl/ListEl.AutoHideBar` (solid while moving, then fading) is
      what gml's views use; the Go builders keep their permanent bar, so
      the cad chrome is untouched. It moved every scrollable page closer:
      lists 2.3→2.0, menubar 1.6→1.3, tabbar 1.4→1.1, splitview 1.1→0.7,
      portal 1.2→0.8.
    - **A wheel notch is 60px** — three lines of twenty, what Qt scrolls
      (QStyleHints::wheelScrollLines) and the desktop convention. Ours was
      an arbitrary 48, a fifth short per notch (lists' wheel Δ12.4 → 8.2).

    Three engine-level fixes fell out of it:

    - **What a styled control publishes has to reach the id the PAGE knows
      it by.** The style instantiation re-points that id's map entries at
      its own scope, so `infoDialog.open()` resolved an object with no
      methods; the instantiation now remembers every object the page
      already knew the id by.
    - **A document waiting on the CLOCK was handed its cached tree for
      ever**, so a pending tooltip could never finish waiting. The frame
      memoization folds the frame-loop clock in while anything is timing.
    - **A ComboBox's `popup` sub-object shadowed the popup() METHOD** on a
      popup kind, since props are consulted before methods.

    What the sweep leaves open, deliberately:

    - **A user's click BREAKS a binding in QML.** Clicking the tri-state
      parent writes `checkState` imperatively, which destroys the
      `checkState:` binding — so Qt's box stays frozen at what the click
      set while its children stay checked. Ours keeps recomputing. Our
      engine models binding-break for assignment (`doc.assigned`) but not
      for a click on a styled control.
    - A styled Label with no width and nothing to stretch it still
      measures nothing outside a layout (it was the corpus dialog's
      missing content line before the styles drew the dialogs).
    - Coordinates in a script come off the REFERENCE render, so a page
      whose layout still differs needs its script re-aimed after a fix.
      (A pair image carries a 26px label strip — subtract it, or read the
      raw reference.)
    - What is left across every driven page: buttons' static 5.3 (it draws
      ToolButton icons the reference omits), the animations page's easing
      and composition cards at ~5 (both engines sampled mid-flight, a
      fraction of a frame apart), and a 3D-axis `Rotation` drawn as a flat
      one (transforms, Δ2.8). Everything else sits on its page's static
      baseline.
    - pathview cannot be driven meaningfully yet: its carousel's rotation
      phase differs from Qt's by a slot, so no coordinate hits the same
      bubble on both sides.

59. DONE: **the side-by-side run, and the layout rules it exposed** (ui,
    script/gml, render). All 65 pages rendered by Qt 6.10 and by us at
    900x700 and ranked by difference. The harness was lying first: the
    corpus loads Quicksand and its icons through qrc-style absolute paths
    that only resolve inside the built executable, so the REFERENCE was
    rendering in Qt's default family with no icons — a font mismatch on
    every page, drowning out everything else. `scripts/qml-compare.sh` now
    runs Qt against a patched copy of the corpus' imports.

    With a fair reference, what was left was not drawing but the numbers a
    layout is built from. In the order they were found:

    - **Font styles collided.** A QML file writes one FontLoader per face
      and both report the family name, so keying the global registry by
      family alone let the bold file answer for the regular — every page
      rendered in bold. Keyed by (family, style) now, with the style read
      from the file's OS/2 weight class and head italic bit.
    - **Text metrics.** Qt reports a Text item's height as the font's OS/2
      WINDOW ascent+descent rounded up (37px for a 32px Quicksand heading
      where the face's own metrics say 30), and puts the baseline lower to
      match. `render.SetQtTextMetrics` carries both; the gml engine turns
      it on, a UI on the Go builders keeps the face's own.
    - **GridLayout was not a grid** but a stack of independent flex rows.
      `ui.GridEl` is now a track grid measured Qt's way: a track's
      preferred size is the largest cell in it (a spanning cell
      contributes only what its own tracks cannot cover), surplus is
      shared in PROPORTION to those sizes, and a cell that did not ask to
      fill keeps its size, left-aligned and vertically centred.
    - **Implicit sizes.** A bare Item or Rectangle has none in QML, so as
      an item of a layout it contributes nothing to its track — ours
      measured its content, which made every card grid lopsided.
    - **Layout item rules**: `Layout.alignment` (accepted and ignored,
      so centred buttons sat left), `Layout.preferredWidth` yielding to
      `Layout.fillWidth` on the same axis, and the three-way precedence
      for an item's size — Layout.preferred*, then the item's own
      implicit size, then width/height (a control keeps its implicit size
      in a layout however wide the markup says it is; a Rectangle, with no
      implicit size, gets the width it states).
    - **Height-for-width.** A wrapping Label with `Layout.fillWidth` was
      not even a layout item, and measured against the whole row rather
      than its share of it, so it ran off the card in one long line.
    - **Positioners and cross-axis anchors.** A Column places its children
      vertically and leaves x free, so `anchors.horizontalCenter:
      parent.horizontalCenter` is what centres a child; the strays pass
      dropped the attribute AND what it meant.
    - **A file component that places itself.** x/y on a component's root
      count as positioning, and the binding has to resolve INSIDE the
      component's scope — it is written in terms of the component's own
      properties.
    - **PathView centres its delegates** on their path point.
    - **A column offers its overflow width to wrapping children**: the
      dial card is 280 wide but its row of three 140px dials needs 460, so
      Qt gives the caption underneath 460 as well — it stays on one line,
      clipped by the card, and its cell keeps one line's height. Measuring
      it against the card's 280 wrapped it and pushed the dials 11px up,
      since the cells are shared out in proportion.
    - **Layout.*Margin** is space around an item inside its cell (the
      corpus indents a checkbox group under its "Select All" by 30) — a
      padded wrapper carries it and forwards the hints the layout reads.
      DivEl grew per-side padding for it.
    - **Shrinking stops at what a child cannot do without**, which for a
      nested layout is what ITS content needs: squeezing past that piled
      the checkbox card's footer on top of its own options, where Qt lets
      the column overflow.
    - **Two Canvas 2D bugs** behind the PFD's artificial horizon:
      clipping a rectangle to a circle lands the rect's corner on the
      circle twice (once per clip edge it crosses), and a zero-area ear
      stops the ear-clipper early — a tilted black wedge across the
      instrument; and a canvas gradient is PLACED by its own two points,
      holding its end colours beyond them, where ours stretched the ramp
      over the shape (right for a QML gradient, wrong for a canvas one:
      the sky runs from two radii above the horizon down to it, so only
      its bottom half should show). `FillPathRampAxis` is the placed
      variant.
    - **Where the corpus does not style a control, Qt uses BASIC** — and
      Basic's metrics are its own, not the application's. Its MenuBar and
      MenuBarItem are 40 tall with 12/16 horizontal padding and 12px
      labels; ours were 34 tall at the page's font, so every menu bar sat
      short and its titles were spaced wrong (the same reasoning as the
      Basic tab palette, item 53). Same for TabBar: 40 tall, tabs 1px
      apart with 6px padding and 12px labels, and the BAR itself light —
      where no tab covers it (three narrow tabs in a wide card) Qt shows
      #efefef and we painted the inactive-tab grey.
    - **ListView details**: `currentIndex: -1` is no selection at all (an
      absent one still starts on the first row); `spacing` is a gap
      between rows, so the pitch grows while each row keeps its height;
      and `section.property` + `section.delegate` put a header in front of
      each RUN of equal values (ui.ListEl grew per-row heights for it, and
      still virtualises).
    - **A wrapping Label kept its family and weight**: it was wrapping on
      the default font's advances, so a card caption came out one line
      where Qt takes two — and the face is what decides where lines break.
    - **A layout with nothing expanding** shares its leftover across all
      items in proportion to their sizes, rather than packing them at the
      top. Every card in the corpus is written that way. An item sits at
      the LEADING edge of a slice wider than itself horizontally and
      CENTRED in one taller than itself — Qt's default alignment, applied
      the same way by its grid engine.
    - **A layout item keeps the size it can USE**: a nested layout asked
      to fill its parent stops at its own content when nothing inside it
      can grow along that axis (QtQuick.Layouts' Layout.maximumHeight),
      and the space it leaves goes back into the pot for the items around
      it.
    - **A layout works in the space it NEEDS**, not the space it has, and
      that required size is recursive: an item which FILLS an axis can
      shrink on it (to its own minimum, or a nested layout's), one which
      does not is stuck at what it measured. So a row of three unfilled
      buttons needs 390px and pushes its card wider, while a row of
      filling sliders needs only its labels. Both ALIGNMENT and the width
      a filling item is given follow that extent — which is why Qt
      centres the 2D-transforms card's logo 30px right of the card's own
      middle. And a control that fills an axis must not carry its implicit
      size as a floor there: `Slider { Layout.fillWidth: true }` blew the
      ecam gauges up to fill their panel when it did.

    Corpus RMS, worst first, before -> after:
    buttons 26.4 -> 5.9, sliders 19.1 -> 2.3, popups 20.1 -> 1.4, tabbar
    18.8 -> 7.6, drawers 17.1 -> 2.3, multiplane 17.0 -> 3.2, layouts
    17.2 -> 3.0, pathview 14.5 -> 3.2, rangesliders 17.2 -> 1.4,
    textinputs 13.0 -> 3.8, lists 10.5 -> 2.3, transforms 9.3 -> 2.2,
    ecam 3.1 -> 1.7, menubar 26.4 -> 1.6, pfd 8.9 -> 1.7, tabbar 18.8 ->
    1.4, switches 10.8 -> 1.1, indicators 12.9 -> 3.1, splitview 4.8 ->
    1.1. Twenty-six of the 37
    comparable pages are now under 5. The only one just above it is
    buttons (5.3), which draws ToolButton icons the REFERENCE does not
    (its style tints them through MultiEffect, which the bare qml tool has
    not got).

    What tops the list now is the REFERENCE missing content, not us:
    diorama (81.9), frostglass (31.6), graphics3d (27.5), lenstabs (25.8)
    and cards2d5 (15.1) all draw through shaders or `layer.effect:
    MultiEffect`, which the bare `qml` tool has not got — Qt renders the
    pages bare and we draw them. particles (10.6) is the same story for a
    different reason: QtQuick.Particles needs GPU rendering (the card's
    own comment says so), and the offscreen platform gives Qt none, in
    either the software or the default backend — checked both. Mask the
    four emitter panels out and that page sits at 2.5, in line with the
    best of the corpus. Our own particle screenshots also catch the
    systems a few frames in, so they show a burst near the emitter rather
    than a steady-state fountain.
    Tests: TestGridTracksShareSurplusProportionally,
    TestGridSpanDoesNotInflateSharedTracks,
    TestGridCellWithoutFillKeepsItsSize,
    TestGridPositionedKeepsPreferredTracks, TestQtLineMetrics,
    TestGlobalFontStylesStayApart, TestFaceStyleFromFile,
    TestRowAsksWrappingChildAgainForItsShare,
    TestRowWrappingChildShrinksWhenWider,
    TestAlignSelfPlacesInsteadOfStretching,
    TestAlignSelfBeatsContainerAlignment, TestSpreadCellsSharesLeftover,
    TestSpreadCellsYieldsToAnExpandingChild,
    TestPositionerHonoursCrossAxisAnchor,
    TestFileComponentPositionsItself, TestPathViewCentresDelegates,
    TestLayoutMaxCapsAFillingChild, TestLayoutMaxYieldsToGrowableContent,
    TestSpreadCellsAlignsAgainstOverflow, TestListViewCurrentIndexMinusOne,
    TestListViewSections, TestListPerRowHeights,
    TestListSpacingIsAPitchNotAHeight, TestRequiredSizeFollowsWhatCanShrink,
    TestFillingChildStretchesToTheOverflow, TestMenuBarBasicMetrics,
    TestCanvasClipHasNoDuplicatePoints,
    TestFillPathRampAxisIsPlacedNotStretched, TestTabBarBasicMetrics,
    TestPaddingPerSide, TestShrinkStopsAtTheRequiredSize,
    TestLayoutMarginIndentsAnItem,
    TestColumnOffersTheOverflowToWrappingChildren.

58. DONE: **soft masks** (render, render/pdf). A soft mask is a whole
    DRAWING used as coverage: its luminosity (or its alpha) fades what
    follows. PDF's /SMask is the reason — it is how a gradient fade or a
    feathered edge is written — and an ExtGState carrying one was ignored,
    so a masked object painted at full strength.

    `BeginSoftMask`/`EndSoftMask(kind, backdrop)` sit on the two pieces
    already here: the mask's drawing goes into a blend group (item 57) and
    that group's target is bound as the coverage mask a clip already knows
    how to sample (item 56). The copy out of the group turns the pixels
    into coverage on the way — Rec.709 luma over the backdrop, or the
    alpha channel — so nothing downstream cares which kind it was. A soft
    mask IS a clip, so it intersects the clip it is pushed inside, and it
    nests with path clips both ways.

    That nesting turned up a real bug in the clip uniform: a box clip
    pushed inside a mask kept the mask but narrowed the rect, and the
    shaders map a pixel into the mask BY that rect — so trimming the
    scissor slid the mask. A clip now carries the mask's extent apart
    from its box. Clip masks and blend groups also resolve together now,
    in the order the frame created them (dependency order): one allocation
    pass, since the clip uniforms name the textures, then one private
    command buffer for every offscreen pass.

    PDF reads /SMask (/S /Luminosity or /Alpha, /G, /BC in the group's own
    colour space) and draws the mask under the CTM in force when the `gs`
    ran, as the spec requires — re-drawn per operation, since the state
    can move in between. /TR, the transfer function, is still ignored.
    Tests: TestSoftMaskLuminosity, TestSoftMaskAlpha,
    TestSoftMaskNarrowedByBoxClip, TestEmptySoftMaskClipsAll,
    TestPathClipInsideSoftMask, TestSoftMaskEntry.

57. DONE: **blend modes and transparency groups** (gpu, render,
    render/pdf). Everything the renderer drew was composited one way:
    source-over. A PDF `/BM /Multiply` was ignored, and a transparency
    group's alpha was applied per object, so the objects in a
    half-transparent group showed through each other.

    `BeginGroup`/`EndGroup(mode, alpha)` renders the draws between them
    into a target of their own and composites the result as ONE image.
    That is PDF's transparency group; a single operation gets a blend mode
    by being wrapped in a group of one.

    Compositing is the only place a blend mode can live here. WebGPU has
    no programmable blending — a fragment shader cannot read the pixel it
    is about to blend with — so a mode has to be a fixed-function blend
    equation on the composite draw (`gpu.BlendDesc`, new, on both
    backends). A group's target starts transparent, so its pixels come out
    premultiplied, which is what makes Multiply and Screen exact at any
    alpha. Darken and Lighten are min()/max(), which know nothing about
    coverage — an uncovered pixel would win as black — so the darken
    fragment fades toward WHITE, the identity of min; both are exact where
    the group is opaque and fade to the backdrop where it is not. The
    modes that need a backdrop read (Overlay, HardLight, Difference, and
    the non-separable Hue/Saturation/Colour/Luminosity) stay Normal rather
    than guess.

    A group's pass runs before the caller's on a private command buffer,
    and targets are filled in the order groups CLOSE, so a nested group is
    ready before the group that composites it. Content is drawn at frame
    scale through one full-size stage per nesting depth and copied out at
    the group's bounds — a frame holds the painted area, not a
    screen-sized texture per group. Still ignored: a soft mask (/SMask in
    an ExtGState).
    Tests: TestBlendMultiply, TestBlendScreen, TestBlendDarkenLighten,
    TestGroupAlphaIsNotPerDraw, TestNestedGroupsCompose,
    TestEmptyGroupDrawsNothing, TestBlendModeNames.

56. DONE: **clips can be any shape** (render, render/pdf). A clip was a
    box plus a corner radius, so a clip shaped like anything else — PDF's
    `W n` around vector art — had to be widened to the path's bounding box
    and let through pixels it should have cut.

    `PushClipPath(path, rule)` renders the path into an offscreen coverage
    mask, and the four 2D shaders multiply their fragment coverage by it,
    so a triangle, a circle or a glyph outline clips exactly like a box
    does. The mask is supersampled 2x and drawn from a private encoder
    submitted just before the caller's pass, the way Layer2D already does.
    Nesting costs no extra pass: each mask vertex carries a uv into its
    parent mask, so the sprite fragment's `textureSample(parent) * color`
    *is* the intersection. An empty path clips everything away.

    The PDF interpreter uses it, but a path that is exactly one
    axis-aligned rectangle still takes the scissor-only route — that is
    nearly every clip in nearly every document, and it would otherwise pay
    a mask render each. Clip chains are shared between graphics states, so
    a `q`/clip/`Q` pair copies a pointer rather than the paths.
    Tests: TestPushClipPathTriangle, TestNestedPathClipsIntersect,
    TestPushClipPathAppliesToImages, TestEmptyClipPathClipsAll,
    TestBoxPathDetection, TestClipChainNests.

55. DONE: **rounded clips** (render, ui). Clipping was a scissor
    rectangle, so a rounded card clipped its content to the bounding box
    and the corners showed. A clip now carries a corner radius: the box
    stays the scissor and the corners are cut in the shaders, through a new
    group(1) uniform that all four pipelines read — so it applies to quads,
    shadows, images, path fills and text alike. `Rectangle { radius: 28;
    clip: true }` cuts its children the way Qt's does, and radius = half
    the side gives a circular crop.

    Arbitrary path clips are item 56, built on the group(1) slot this adds.
    Tests: TestPushClipRoundCutsCorners, TestPushClipRoundAppliesToImages,
    TestClipIntersection.

54. DONE: **PDF pages are really rendered** (render/pdf). The last real
    gap: QtQuick.Pdf had a full object model here but no renderer, so a
    PdfDocument always reported Error.

    There is no separate rasterizer — a page's content stream is
    interpreted straight into Renderer2D calls, so PDF pages get the same
    antialiasing, transforms and glyph atlas as everything else, and no cgo
    comes near it. The reader handles classic xref tables and xref streams
    (with /Prev chains and hybrid /XRefStm), object streams, a
    scan-for-objects recovery for broken files, the standard filters and
    predictors, and page trees with inherited attributes. Drawing covers
    paths and clips, Gray/RGB/CMYK/ICCBased/Indexed/Separation colour with
    tint transforms, all four PDF function types (including the type-4
    PostScript calculator programs a spot colour's tint transform is
    written in), form XObjects, images with stencil and
    soft masks, and text positioned per glyph from the font's own widths
    (standard-14 metrics included, so a font with no /Widths still lands).

    An embedded PDF font is usually a subset whose cmap covers only that
    document's glyphs — often no cmap at all — so a glyph the embedded face
    lacks is drawn with the fallback face (render.Font.Covers, new).

    PdfMultiPageView scrolls the pages at renderScale and interprets only
    the ones near the viewport, so a 728-page book costs one page. Checked
    page by page against the two books the corpus ships, with the new
    examples/pdfsmoke tool.

53. DONE: **an unstyled TabBar is Qt's Basic style**. A page whose style
    module ships no TabButton.qml gets Basic, which is what the corpus
    reference renders: the current tab near-white (#efefef) with black
    text, the others mid-grey (#9f9f9f) with white text, divided by
    hairlines. Ours drew a white bar with an accent underline — a
    Material/Fusion idea, not Basic's. The palette was sampled off the
    reference renders; the MenuBar was already Basic-like and now shares
    the constant.

    ui gained two tooling accessors the test needed: `Children` (walk a
    built tree) and `FillColor` (read a widget's own fill without
    rendering it). Test: TestTabBarBasicPalette.

52. DONE: **the symbol fallback is a chain**. No one face carries every
    dingbat block a UI font skips: the broad Latin-plus-symbols faces
    (DejaVu) stop before the sparser blocks, so five codepoints the corpus
    uses — ⎘ NEXT PAGE, ⎙ PRINT SCREEN, ⑂ OCR FORK, ⭳ arrow-to-bar, ⮞
    arrowhead — drew nothing at all. Noto's two symbol families carry them,
    and `symbolFontChain` now installs every symbol face that is available
    (the game shells too, not just the UI shell).

    Swept all 163 symbol codepoints the corpus uses into one page and
    measured the ink per cell: 162 render, and the one blank is a
    private-use codepoint with nothing to draw. The "folder glyph" from
    item 49's list was a false alarm — U+2750 ❐ renders correctly, and Qt
    draws the same hollow drop-shadowed square (checked against the Qt
    runtime); it just does not look like a folder.

    While there: **a control that states its own size keeps it in a
    layout.** Qt hands an item its preferred size — the implicit size when
    no Layout.preferred* is stated — and overflows rather than squeezing,
    so a zoom ComboBox asking for 90px keeps 90 and its label fits. The
    toolbars now overflow their cards exactly where Qt's do.
    Test: shell.TestSymbolFontChain.

51. DONE: **a TableView cell is its column's width**. The delegate's own
    implicitWidth is a fallback for a column the width provider does not
    cover; the view sizes each delegate to its column. Ours left the
    delegate at its implicit size inside a wider cell, so a strip of the
    view showed through every cell and the alternating row colours came out
    as gapped blocks rather than stripes (the synced header had the same
    gaps). Test: TestTableViewDelegateFillsTheColumn.

50. DONE: **frosted glass** — the glassmorphism pages (frostglass,
    lenstabs) drew no frost at all: the card was transparent and what
    looked like an effect was the background showing through it. The
    "layer-composited elements paint above later siblings" note from item
    49 was a misreading of that symptom; layers paint in tree order, and a
    plain sibling declared after a layered one covers it, as it should.

    Four things stood between the markup and the effect, each general:

    - `sourceItem: root.backgroundItem` — a property holding the item to
      capture, which is how a reusable glass card is handed its background
      — was documented in our own source as unfollowable. Any expression
      whose value is an element now resolves to that element's id.
    - A ShaderEffectSource and an effect are the size they STATE. Their
      source is usually an anchor-filled item, which measures nothing on
      its own, so both came out 0x0 — and a zero-sized effect has no
      texture to blur.
    - The captured copy registered itself under the SOURCE's key, so every
      binding reading the source's x/y followed the copy instead. The copy
      is positioned from those same reads, so each frame moved it again:
      the capture drifted across the page by the card's origin per frame.
      Nothing built inside a capture claims the registry now.
    - An id'd element's parent-relative x/y needs its parent's LIVE rect.
      The parent object is captured when the id is declared — at load time,
      when nothing has been laid out — so x/y read as window coordinates,
      and `x: -root.x` (pull the captured background back by the card's
      own position) came out zero. The parent's registry key is kept
      alongside the object and resolved against the live registry, and a
      component instance's root takes its parent from the use site.

    Test: TestEffectCaptureKeepsTheSourceGeometry.

49. DONE: **the page-by-page walk** — all 65 pages read one at a time,
    ours against Qt's where the reference carries content (37 of them; the
    other 28 are C++-backed pages the bare `qml` runtime renders empty, so
    only our side can be judged). Eight more engine defects came out of it,
    every one of them general:

    - **The pointer started at (0, 0)**, which is a real point on screen, so
      a control laid out over the origin on its first frame latched a hover
      it never lost — a headless screenshot came out with tinted flat
      buttons. It starts nowhere now.
    - **A corner radius past half the box** put the shader's corner centre
      outside it and carved the quad into a lens: QML pill radii (`radius:
      30` on a 36px-tall button) rendered as diamonds. Clamped in render,
      where quads and shadows are pushed.
    - **`visible: false` removed an item instead of hiding it**, so
      `anchors.fill: sourceImage` over a hidden Image — the Qt5Compat
      effect-chain idiom — resolved against a zero rect and the whole chain
      came out 0x0. An id'd item outside a layout now lays out and registers
      its geometry (ui.Hidden: no paint, no input).
    - **`sourceSize` was pinned to zero**, so `width: Math.min(parent.width,
      sourceSize.width)` sized an image to nothing. Raster sources report
      their real header size; vector sources still report zero (a style
      multiplying an icon's 960-unit viewBox by its own scale factor was the
      reason for the pin).
    - **A RowLayout stretched every auto-sized child**, leaving labels
      riding at the top of the tallest item. Qt stretches only the items
      that asked (Layout.fillHeight) and centres the rest.
    - **Layout.preferredWidth/Height never reached a Label**, so a table of
      them collapsed every column to its own text width.
    - **One axis stated in terms of the other** (`Layout.preferredWidth: 10;
      Layout.preferredHeight: width` — a round dot) read 0, because the
      item's own width was not in scope while its layout hints were
      evaluated. The dots down a sidebar were one continuous bar.
    - **A Loader only resolved a bare id**, so neither of the two idioms the
      pages use worked: a ternary switching components, and an inline
      `Component { }` as the attribute value. It publishes `status` and
      `item` now too.

    Plus three smaller ones: `Qt.locale()` did not exist (every
    `toLocaleString(Qt.locale("en_US"), 'f', 0)` threw and took its Text
    with it — the database table lost a column), `ComboBox.editText` was
    accepted but never published, and `PathAngleArc` joined itself to the
    path's start instead of beginning its own subpath.

    What the walk leaves behind, all cosmetic and all noted here rather than
    fixed (two items from the first version of this list turned out not to
    be defects at all: the layer-order suspicion was item 50, and layers do
    paint in tree order; and spritestack's half-width card is the fixpoint
    of the page's own circular size binding — Qt settles on exactly the same
    312x164, checked by running the same markup through the Qt runtime):
    nothing — items 50-53 closed the rest of it.

    One thing the walk settled rather than fixed: our type looks bigger than
    the reference's because the corpus loads **Quicksand** through a
    FontLoader with a `qrc:` path. The bare `qml` runtime cannot resolve it
    and falls back to a generic face; we do load it. On type, ours is the
    more faithful render.

48. DONE: **styled RangeSlider**. It goes through the Templates layer now:
    the style's groove, its `first.handle` / `second.handle` delegates, and
    a bare overlay slider for the drag (`ui.RangeSliderEl.Bare`, matching
    SliderEl). Each handle is a sub-object with its own live value,
    published on the control's id at declare time — a page's
    `priceRange.first.value` is usually read by a Label written above the
    slider — and adopted by the styled control so both sides move together.
    Position and visualPosition derive on the control's range, inverted for
    a vertical one.
    Test: TestStyledRangeSliderHandles.

Every Controls kind the corpus styles now renders through its style file.
Still open: the short list at the end of item 49 the side-by-side pairs still show.

The pages built on the app's C++-registered types (SqlQueryModel,
SettingsManager, AsyncTask, SpriteStack, ToastHost, GraphsView,
ParticleSystem) degrade the way Qt does without its plugin, and the three
modules with no backend here — QtMultimedia (no decoder), QtLocation (no
tiles), QtQuick3D (no shadows/IBL/mesh files) — are modeled as far as they
can be and honest about the rest. QtQuick.Pdf is no longer among them: it
renders (item 54).
