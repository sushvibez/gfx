# ui — the 2D toolkit and the application shell

`github.com/sushvibez/gfx/ui` is the widget toolkit; `ui/shell` opens the
window, runs the frame loop and renders headless screenshots. Every snippet
below is taken from `examples/` (`counter`, `kitchensink`, `gallery`,
`interact`, `overlays`, `settings`, `viewport3d`, `animation`) or from the
package tests.

## The model: retained state, immediate tree

The architecture is the GPUI hybrid with QML's property system underneath:

- **State is retained.** View objects own `core.Property` values and widget
  state objects (`ui.InputState`, `ui.ScrollState`, `ui.ListState`,
  `ui.LayerState`, `ui.ImageCell`). Bindings, signals and animation work on
  them exactly as described in [core.md](core.md).
- **The tree is immediate.** A render function rebuilds a lightweight
  element tree from that state every frame, with a Tailwind-style fluent
  builder. Elements hold no state of their own and nothing retains them;
  there is no element identity to manage and no invalidation protocol.
- **Layout** is a flexbox subset (direction, gap, padding, fixed sizes, grow
  and shrink factors, justify/align, wrap) plus QML anchors and a track
  grid. **Painting** goes through `render.Renderer2D` as SDF-styled instanced
  quads; hit testing walks the laid-out tree topmost first.

`examples/settings` shows the shape of a real view:

```go
type state struct {
    username  *ui.InputState
    dark      core.Property[bool]
    volume    core.Property[float32]
    body      ui.ScrollState
    summary   core.Property[string]
}

func newState() *state {
    s := &state{username: ui.NewInput("gopher")}
    s.dark.Set(true)
    s.volume.Set(0.7)
    s.summary.Bind(func() string {
        mode := "light"
        if s.dark.Get() {
            mode = "dark"
        }
        return fmt.Sprintf("%s · %s mode · volume %d%%",
            s.username.Text.Get(), mode, int(s.volume.Get()*100+0.5))
    })
    return s
}

func (s *state) view() ui.Element {
    return ui.Div().Col().P(18).Gap(12).Bg(ui.RGB(0x131318)).Children(
        ui.Text("Settings", ui.RGB(0xf1f1f7)),
        ui.Scroll(&s.body).H(360).Gap(12).Children(
            ui.Checkbox(&s.dark, "Dark mode"),
            ui.Slider(&s.volume, 220),
            ui.Input(s.username, 220),
        ),
        ui.Text(s.summary.Get(), ui.RGB(0x91a7ff)),
    )
}
```

Because every property read during `view()` is observable, the shell can
skip frames in which nothing changed (`core.Revision()` unchanged, no
animation running, no input), and partial damage repaint only what moved.

## Opening a window: `ui/shell`

`shell.RunUI` is the whole bootstrap for a plain UI: window, GPU device,
2D renderer with 4x MSAA, fonts with CJK/emoji fallbacks, clipboard, frame
loop with present-on-damage and an animation rate cap.

```go
err := shell.RunUI(shell.Options{
    Title: "graphics: counter", Width: 420, Height: 240,
    FontSize: 20, MaxFrames: *frames,
}, view)
```

| `shell.Options` field | Meaning |
|-----------------------|---------|
| `Title`, `Width`, `Height` | window title and logical size |
| `FontSize` | default text size, 18 if zero |
| `FontPaths` | font files tried before the platform defaults (`GFX_FONT` / `GFX_CJK_FONT` also apply) |
| `MaxFrames` | exit after N presented frames (0 = until closed); used by CI runs |
| `MaxFPS` | frame-rate cap; 0 = 60 frames a second of animation in `Run` (input still draws at once) and the display rate in the game runners (`GFX_MAX_FPS` overrides `Run`'s) |
| `ClearHex` | window clear colour, `0x0d0d12` by default |
| `LineHeight` | default line height as a multiple of font size (0 = natural) |
| `MSDFText` | distance-field text as the window default (`GFX_MSDF=1\|0` overrides) |
| `EventDriven` | a `FrameStart` hook that only reacts to events, so idle skipping stays on |
| `Undecorated`, `ResizeBand`, `Transparent` | client-side decorations: the shell handles border resizing and `Hooks.DragRegion` moving |
| `Unmovable`, `Unresizable`, `Unminimizable` | window hints (Windows; X11 through window-manager hints) |
| `X`, `Y`, `Positioned`, `Fullscreen` | placement in virtual-desktop pixels, borderless over the monitor containing (X, Y) |

The general form is `shell.Run(o, shell.Hooks{...})`:

| `shell.Hooks` field | When it runs |
|---------------------|--------------|
| `Init func(a *App) (func() ui.Element, error)` | once, after renderers exist; returns the view function |
| `FrameStart func(a *App, dt float32, mouse []app.MouseEvent, keys []app.KeyEvent)` | each frame before UI event dispatch (cameras, polling) |
| `Encode3D func(a *App, enc *gpu.CommandEncoder, target, depth *gpu.TextureView) error` | after UI layout, before the 2D pass: record 3D work; the 2D pass then loads the target instead of clearing it |
| `DragRegion func(a *App, x, y float32) bool` | whether a point is in the titlebar move region (undecorated windows) |

`shell.App` hands the hooks `Win`, `Device`, `Queue`, `Format`, `R2`
(`*render.Renderer2D`), `Font`, `UI` (`*ui.UI`), `W, H` in logical pixels,
plus the frame's touch stream (`Touches`, `Gest`). `a.OpenChild(o, view)`
opens a secondary window sharing the device, renderer and fonts; it returns a
`*shell.ChildWindow` with `Close()`, `Size()`, `SetDragRegion`, an
`OnClose` callback and its own `Encode3D`. Child windows are X11-only today;
other backends return the app layer's error.

### Headless rendering and screenshots

Every runner has a windowless twin that renders offscreen and writes a PNG,
which is how CI and agents verify a screen:

| Function | Use |
|----------|-----|
| `shell.ScreenshotUI(o, view, path)` | the no-hooks counterpart of `RunUI` |
| `shell.Screenshot(o, hooks, path)` | with hooks; with `Encode3D` two frames run so offscreen viewports get sized first |
| `shell.ScreenshotScript(o, hooks, step)` | a sequence of captures, `step(i, u)` injecting input between them through `ui.UI.Click`/`Drag`/`Fling`/`Key` |
| `shell.RunScript(o, hooks, spec, base)` | the same driven by a text script (`click 120,60`, `drag 40,80 200,80`, `wheel 200,300 -120`, `key ctrl+Enter`, `type hello`, `wait`), one PNG per step |

```go
opts := shell.Options{Title: "graphics: gallery", Width: 480, Height: 320}
if *shot != "" {
    err = shell.ScreenshotUI(opts, view, *shot)
} else {
    err = shell.RunUI(opts, view)
}
```

Screenshots need GPU readback and are native-only; the `js` build of these
functions returns an error. Between scripted steps the animation clock
advances by `shell.ScriptStepSettle` (700 ms) so animations a step started
are captured settled.

### Game loops

`shell.RunGame(o, update func(*shell.Frame) bool)` is the immediate-mode
sibling of `RunUI`: no retained tree, just a `Frame` with the renderer `G`,
`Font`, `W, H`, `Dt` and an `In *shell.Input` of held keys, edge events,
mouse buttons, `WheelY` and touch gestures. Return `false` to quit; call
`f.Unchanged()` to let the loop sleep when nothing moved.
`shell.ScreenshotGame(o, steps, dt, update, prep, path)` steps it headlessly,
and `shell.RunGame2` / `RunGame2Deferred` open two windows on one device
(X11 and Windows). The game runners read the same window, font, clear,
frame-cap and `MSDFText` options as `Run`; an `Undecorated` game window
resizes from its border and moves by `Frame.DragRegion`. `LineHeight` and
`EventDriven` concern the retained tree and only `Run` reads them.

## The element catalogue

Every constructor returns a pointer to its element type with chainable
setters; `ui.Element` is the interface they all satisfy. Colours are
`ui.RGBA` (an alias of `render.RGBA`); `ui.RGB(0xRRGGBB)` builds an opaque
one and `ui.ParseColor("#rgb" | "rgba(...)" | "steelblue")` parses the CSS
and Qt forms.

### Containers and layout

| Constructor | Element | Purpose |
|-------------|---------|---------|
| `ui.Div()` | `*DivEl` | the flexbox-lite box: column by default, `Row()` for a row |
| `ui.Grid(cols, rows)` | `*GridEl` | track grid with `Cell(el, colSpan, rowSpan)` / `Children(...)`, `Gap`, `GapXY`, Qt-style proportional track sizing; `Positioned()` for a non-stretching positioner |
| `ui.Anchored(child, id, ui.Anchors{...})` | `*AnchorEl` | a child of a `Div().AnchorMode()` container placed by QML anchors |
| `ui.Hidden(child)` | `*HiddenEl` | keeps its geometry, paints nothing, takes no input (QML `visible: false`) |
| `ui.Slide(from, to, progress, dir)` | `*SlideEl` | page transition: `to` slides in while `from` slides out, driven by a tweened `progress` 1→0 |

`DivEl` carries most of the styling API:

| Group | Methods |
|-------|---------|
| Axis and size | `Row()`, `Col()`, `W(px)`, `H(px)`, `Size(w, h)`, `MinW`, `MinH`, `NoImplicitSize()` |
| Flex | `Grow()`, `GrowBy(f)`, `Shrink()`, `ShrinkBy(f)`, `Wrap()`, `SpreadCells()`, `CrossFill()`, `CrossCenter()`, `AlignSelf(a)` |
| Alignment | `Justify(a)`, `Items(a)`, `JustifyCenter()`, `ItemsCenter()` with `ui.AlignStart`, `ui.AlignCenter`, `ui.AlignEnd` |
| Spacing | `P(px)`, `PadXY(x, y)`, `Padding(t, r, b, l)`, `Gap(px)` |
| Fill | `Bg(c)`, `Gradient(c0, c1, angleRad)`, `GradientStops`, `GradientStopsN`, `GradientRamp`, `RadialGradient(c0, c1)`, `Opacity(a)` |
| Border and shape | `Rounded(px)`, `Border(w, c)`, `BorderSide(side, w, c)`, `BorderTop/Right/Bottom/Left`, `BorderDashed()`, `Clip()` |
| Shadow | `Shadow(blur, dx, dy, c)`, `ShadowSpread(blur, dx, dy, spread, c)`; each call adds one shadow |
| Interaction | `OnClick(fn)`, `AlsoOnClick(fn)`, `HoverBg(c)`, `PressBg(c)`, `OnHoverChanged(fn)`, `OnPressChanged(fn)`, `Cursor(app.Cursor)` |
| Gestures | `OnDrag(fn)`, `OnPointer(fn)`, `OnFling(fn)`, `OnSwipe(fn)` |
| Keyboard | `Focusable()`, `Focus()`, `TabIndex(n)`, `OnKey(fn)`, `FocusBorder(w, c)`, `FocusVisibleBorder(w, c)` |
| Transform | `Rotate(deg)`, `ScaleBy(s)`, `Transform(t)` with `ui.Translate`, `ui.Rotation`, `ui.Rotation3D`, `ui.ScaleXY` |
| Anchors | `AnchorMode()`, `AnchorFit()` |
| Popups | `PopupSkin()`, `DarkPopupSkin()` give an unstyled panel a card look |
| Children | `Children(els ...Element)`, appends |

Unlike CSS, elements are rigid by default: a child never shrinks below its
measured size unless it calls `Shrink()`, because scroll content relies on
overflowing its viewport. Transforms affect paint and hit testing only;
layout is untouched, as in QML.

```go
card := func(title string, children ...ui.Element) ui.Element {
    return ui.Div().Col().P(16).Gap(10).Bg(ui.RGB(0x1d1d27)).Rounded(14).
        Border(1, ui.RGB(0x32323f)).Shadow(14, 0, 6, ui.RGBA{A: 0.5}).
        Children(append([]ui.Element{ui.Text(title, ui.RGB(0xb6b6c8))}, children...)...)
}
```

QML anchors, from `anchors_test.go`:

```go
a := ui.Anchored(ui.Div().Size(40, 40), "a",
    ui.Anchors{Left: &ui.AnchorLink{"parent", ui.EdgeLeft}, Top: &ui.AnchorLink{"parent", ui.EdgeTop}, Margins: 10})
b := ui.Anchored(ui.Div().Size(20, 20), "b",
    ui.Anchors{Left: &ui.AnchorLink{"a", ui.EdgeRight}, Top: &ui.AnchorLink{"a", ui.EdgeTop}})
root := ui.Div().AnchorMode().Children(b, a) // order does not matter: resolution is a fixpoint
```

`ui.Anchors` has `Fill`, `CenterIn`, the six edge links (`Left`, `Right`,
`Top`, `Bottom`, `HCenter`, `VCenter` against `ui.EdgeLeft` … `ui.EdgeVCenter`),
`Margins` and per-side margins, `HCenterOffset`/`VCenterOffset`, and manual
`X`/`Y` with `HasX`/`HasY`.

### Text

| Constructor | Element | Purpose |
|-------------|---------|---------|
| `ui.Text(s, c)` | `*TextEl` | one line in the context font |
| `ui.Wrapped(s, c, width)` | `*Paragraph` | word-wrapped text at a width |
| `ui.Rich(width, spans...)` | `*RichEl` | wrapped multi-colour text from `ui.Span{Text, Color, Font}` |

`TextEl` setters: `SizePx`, `Font(*render.Font)`, `FontFamily(name)`,
`Bold()`, `Italic()`, `Underline()`, `UnderlineWavy()`, `UnderlineColor`,
`UnderlineThickness`, `StrikeThrough()`, `Tracking(px)`, `LineHeight(rel)`,
`Glow(c)`, `W`/`H` with `HAlign`/`VAlign`, `Truncate()`, `TruncateClip()`,
`Grow`, `GrowBy`, `CrossFill`, `AlignSelf`, `DistanceField(on)`.
`Paragraph` has `SizePx`, `FontFamily`, `Bold`, `Italic`, `Center`,
`LineHeight`, `Lines(n)` (line clamp with ellipsis), `NoEllipsis`, `Grow`,
`CrossFill`, `AlignSelf`.

```go
ui.Rich(360,
    ui.Span{Text: "red ", Color: ui.RGB(0xff6b6b)},
    ui.Span{Text: "green ", Color: ui.RGB(0x69db7c)},
    ui.Span{Text: "blue", Color: ui.RGB(0x74c0fc)},
),
ui.Wrapped(lorem, ui.RGB(0xc6c6d4), 360),
```

Text draws from a rasterised glyph atlas by default. `UI.SetMSDFText(true)`
(or `shell.Options.MSDFText`) switches the window to the multi-channel
distance-field path: one sheet serves every size, text survives rotation and
sliding sizes, and `UI.SetTextEffect(weightEm, outlineEm, outline, glowEm)`
becomes available. `TextEl.DistanceField(on)` overrides it for one run (QML's
`renderType`). Bold and italic resolve real faces from the renderer's
registry when one is loaded and fall back to synthetic emphasis otherwise.

### Controls kit

QML's Quick Controls as Go constructors, drawn in the current `ui.Theme`
(see Theming); gml's controls are built on the same functions.
`examples/controls` puts all of them on one page. State that has to live
across frames is a small struct you keep (`MenuState`, `DrawerState`, …),
like `ScrollState`.

| Constructor | QML | Notes |
|-------------|-----|-------|
| `ui.Button(label, ui.ButtonOpts{…})` | Button | 96×38 accent button: `OnClick`, `Width`/`Height`, `Color`/`TextColor`, `Radius`, `Flat`, `Highlighted`, `Disabled`, `Icon`, `Checked *core.Property[bool]` (checkable), `Theme`; Enter/Space press it while focused |
| `ui.ToolButton`, `ui.RoundButton` | ToolButton, RoundButton | flat and label-sized; round, 40 across |
| `ui.DelayButton(label, &st, ui.DelayOpts{…})` | DelayButton | hold for `Delay` (300 ms) to run `OnActivated`; a bar under it fills |
| `ui.Switch(&prop, label, onToggle…)`, `ui.SwitchOpts{Disabled}.Build(…)` | Switch | pill and knob |
| `ui.Checkbox`, `ui.TriCheckbox`, `ui.Radio` / `ui.RadioRing` with `ui.ButtonGroup` | CheckBox, RadioButton, ButtonGroup | a group makes members exclusive |
| `ui.ComboBox(&cur, &open, ui.ComboOpts{…})`, `ui.SpinBox(&v, ui.SpinOpts{…})` | ComboBox, SpinBox | editable through an `InputState` |
| `ui.DialControl(&v, from, to, ui.DialOpts{…})`, `ui.DialFace(pos, size, theme)` | Dial | a painted knob over `ui.Dial`'s interaction |
| `ui.ProgressBar(v, ui.ProgressOpts{…})`, `ui.BusyIndicator(&activity, size)` | ProgressBar, BusyIndicator | `Indeterminate: &activity` sweeps; a `ui.Activity` runs off the animation clock and asks for frames only while running |
| `ui.MenuBar(&st, []ui.MenuBarMenu{…})`, `ui.MenuPanel(&st, items, onClose)` | MenuBar, Menu, MenuItem, Action | `ui.MenuItem{Text, OnTrigger, Checkable, Checked, Group, Disabled, Separator, Sub, Shortcut}`; submenus open to the right; Up/Down/Enter/Right/Left/Escape; `item.Activate()` is what picking it does (wire a shortcut to it) |
| `ui.Dialog(&open, content, ui.DialogOpts{…})`, `ui.DialogFrame`, `ui.DialogButtonBox` | Dialog, DialogButtonBox | `Buttons: ui.ButtonOk \| ui.ButtonCancel` (Qt's values), `OnAccepted`/`OnRejected`/`OnClosed` |
| `ui.Drawer(&st, content, ui.DrawerOpts{…})` | Drawer | slides in from `Edge`; drag toward the edge or click the scrim to close; `DragMargin` lets a drag from the edge open it |
| `ui.TabBar(cur, []ui.Tab{…}, onSelect)` | TabBar, TabButton | Left/Right with focus |
| `ui.Tumbler(&st, cur, set, ui.TumblerOpts{…}, item)` | Tumbler | drag a row at a time, flick to coast, Up/Down; `item(i, displacement)` draws a row |
| `ui.PageIndicator(n, cur, onSelect, ui.PageIndicatorOpts{})` | PageIndicator | |
| `ui.SplitView(&st, []ui.SplitPane{…}, ui.SplitOpts{…})` | SplitView | panes with `Preferred`/`Minimum`/`Maximum`/`Fill`; handles drag within the limits |
| `ui.ToolTip(text)`, `(&st).Attach(el, text, ui.ToolTipOpts{Delay, Timeout})` | ToolTip | `Track`/`Due` for a host with its own rule for when it shows |
| `ui.Separator(vertical)`, `ui.ToolBar(…)`, `ui.GroupBox(title, …)` | ToolSeparator/MenuSeparator, ToolBar, GroupBox | |
| `ui.Pane()`, `ui.Frame()`, `ui.Page(header, content, footer)` | Pane, Frame, Page | boxes with a name: sugar for `Div` |

```go
var s struct {
    open  core.Property[bool]
    draw  ui.DrawerState
}
ui.Div().Col().Children(
    ui.Button("Save…", ui.ButtonOpts{OnClick: func() { s.open.Set(true) }}),
    ui.Dialog(&s.open, ui.Text("Save changes?", ui.CurrentTheme().Text),
        ui.DialogOpts{Title: "Save", Buttons: ui.ButtonSave | ui.ButtonCancel, OnAccepted: save}),
    ui.Drawer(&s.draw, nav(), ui.DrawerOpts{Size: 260, DragMargin: 12}),
)
```

A plain `Div` with hover and press styles is still a button too, when the
kit's look is not wanted:

```go
ui.Div().Size(120, 38).Bg(ui.RGB(0x3b5bdb)).Rounded(8).Focusable().
    HoverBg(ui.RGB(0x4c6ef5)).PressBg(ui.RGB(0x2f44ad)).Cursor(app.CursorPointer).
    JustifyCenter().ItemsCenter().OnClick(func() {}).
    Children(ui.Text(label, ui.RGB(0xffffff)))
```

### Input widgets

| Constructor | State | Purpose |
|-------------|-------|---------|
| `ui.Input(state, w)` | `*ui.InputState` from `ui.NewInput(initial)` | single-line text field |
| `ui.TextArea(state, w, h)` | the same state, made multi-line | wrapped editor: Enter inserts a newline, Ctrl+Enter submits |
| `ui.Checkbox(&prop, label, onToggle...)` | `core.Property[bool]` | toggle; clicking the box or label flips it |
| `ui.Slider(&prop, w)` | `core.Property[float32]` in `[0,1]` | horizontal slider; `Range(from, to)`, `Step(step, snap)`, `Vertical()`, `Colors`, `Thickness`, `Disabled`, `Bare`, `OnMoved` |
| `ui.RangeSlider(&first, &second, w)` | two properties | two handles that cannot cross |
| `ui.Dial(&prop, from, to)` | `core.Property[float32]` | rotary interaction over a 280° sweep; paints nothing itself (`ui.DialControl` draws a knob over it) |

`InputState` keeps `Text core.Property[string]` plus caret and selection,
so a binding downstream of the field updates as the user types:

```go
s := &state{name: ui.NewInput("gopher")}
s.greeting.Bind(func() string { return "Hello, " + s.name.Text.Get() + "!" })
// in the view:
ui.Input(s.name, 300)
ui.Text(s.greeting.Get(), ui.RGB(0xe8e8f0))
```

`InputState` methods: `SetText`, `OnSubmit(fn)` (Enter), `Accept(fn)`
(validator that rejects a keystroke), `MaxLen(n)`, `SelectAll`, `HasFocus`,
`SetFocused`, `Key(ev)`, `Multiline()`. `InputEl` setters: `Placeholder(s, c)`,
`Bg`, `Border`, `Accent`, `TextColor`, `Rounded`, `SizePx`, `Center`,
`Echo(mask)` (password), `ReadOnly(v)`, `Focus()`. Editing supports
Shift-extended selection, Ctrl+A/C/X/V, word movement and mouse drag
selection; `ui.SetClipboard(get, set)` routes copy/paste through an external
clipboard (the shell installs the OS one; tests keep the in-process buffer).

### Scrolling and lists

| Constructor | State | Purpose |
|-------------|-------|---------|
| `ui.Scroll(&st)` | `ui.ScrollState` | vertically scrolling column: `W`, `H`, `P`, `Gap`, `Children`, `Grow`, `BarColor`, `AutoHideBar()`, `Flickable()` (drag to pan with kinetic release), `ContentSized()` |
| `ui.List(&st, rowHeight, count, build)` | `ui.ListState` | virtualised list: only visible rows are built, so 10,000 rows cost the same as ten. `Spacing`, `Heights([]float32)`, `Measured()` (rows size themselves), `BottomUp()` (chat logs), `AutoHideBar`, `W`, `H`, `Grow`, `VisibleRange()` |

```go
ui.List(&s.list, 32, 10000, func(i int) ui.Element {
    bg := uint32(0x1e1e28)
    if i%2 == 1 {
        bg = 0x24242f
    }
    return ui.Div().Row().H(32).P(8).Bg(ui.RGB(bg)).ItemsCenter().
        Children(ui.Text(fmt.Sprintf("Item #%d", i), ui.RGB(0xd4d4e0)))
}).W(180).H(180),
ui.Scroll(&s.inner).W(150).H(180).P(8).Gap(6).Children(lines(12)...),
```

`ScrollState` exposes `X`, `Y`, `MaxX`, `MaxY`, `ViewW`, `ViewH`,
`ContentW`, `ContentH` and `StopFling` for app-drawn scrollbars;
`ListState` exposes `Offset`, `SetOffset`, `MaxY`, `ViewH`. Wheel events go
to the deepest scroll region under the pointer and bubble outward per axis
when a region cannot consume them; one wheel notch is 72 px, matching Qt.

### Images, SVG, icons and canvas

| Constructor | Purpose |
|-------------|---------|
| `ui.Img(img *render.Image, w, h)` | a GPU image loaded once with `Renderer2D.LoadImage` / `NewImage`; `Tint`, `Colorize`; a zero `w` or `h` takes the source size |
| `ui.LazyImage(cell *ui.ImageCell, w, h)` | decodes `cell.Data` (PNG/JPEG/GIF, animated GIFs included) on first paint, downscaled to the box; `Stretch`, `Crop`, `Natural`, `Grow`, `Tint`, `Colorize` |
| `ui.SVG(svg *render.SVG, w, h)` | a document parsed once with `render.ParseSVG`; `Tint`, `Colorize`, `Stretch`, `Crop`, `Rotate` |
| `ui.NewCanvas(w, h, draw)` | immediate-mode leaf: `draw(r *render.Renderer2D, rect ui.Rect)` emits paths, gradients, images into its rect (clipped unless `Unclipped()`) |
| `ui.NewCanvasFont(w, h, draw)` | the same with the UI font passed in |
| `ui.NewCanvasFlow(w, height, draw)` | height computed from width at measure time, for content that flows like text |
| `ui/icons` | built-in Lucide-flavoured 24×24 stroke icons (`back`, `close`, `plus`, `search`, `pencil`, `folder`, `settings`, `trash`, 35 in all): `icons.SVG(name, color)` returns an SVG document to parse; `icons.Register` / `RegisterFull` add your own; `icons.Has(name)` |

```go
var heart, _ = render.ParseSVG([]byte(heartSVG))

func star(r *render.Renderer2D, rect ui.Rect) {
    p := render.NewPath()
    // ... MoveTo / LineTo around the rect centre ...
    p.Close()
    r.FillPath(p, ui.RGB(0xffd43b))
}

ui.Div().Row().Gap(12).ItemsCenter().Children(
    ui.NewCanvas(80, 80, star),
    ui.SVG(heart, 80, 80),
    ui.NewCanvas(160, 70, wave),
)
```

A canvas re-runs its draw function every paint; nothing can tell what it
will draw, so it is never culled by damage tracking.

### Overlays, popups and modals

Overlays occupy no space in normal flow, paint after the main tree (on top,
free of any clip) and hit-test before it. Build them inside the tree where
the trigger lives, gated on your own open/visible state.

| Constructor | Purpose |
|-------------|---------|
| `ui.Overlay(child, x, y)` | float `child` at an absolute point; `AnchorTo(el, dx, dy)`, `AnchorBelow(el, gap)`, `AnchorAbove(el, gap)` position it relative to an element laid out earlier in the tree; `Pin(ui.AnchorBottomLeft …)` chooses which corner lands there; `SnapToWindow(margin)` keeps it on screen; `OnClickOutside(fn)` dismisses |
| `ui.Modal(child, dim, onDismiss)` | centred over the window behind a scrim of colour `dim`; a scrim click calls `onDismiss` |
| `ui.PositionedModal(child, x, y, dim, onDismiss)` | the same at a point (menus, dropdowns) |
| `ui.Layer(state, child)` | renders `child` into an offscreen texture (`ui.NewLayerState()`); `Blur(px)` is a real gaussian for frosted glass, `Opacity` composites the group as one, `Tint`, `Colorize`, `Shadow`, `Offscreen()` |

From `examples/overlays`:

```go
root := ui.Div().Col().P(20).Gap(16).Bg(ui.RGB(0x14141c)).Children(
    ui.Div().Size(160, 38).Bg(ui.RGB(0x1d1d27)).Rounded(8).Border(1, ui.RGB(0x3b3b4f)).
        ItemsCenter().P(8).OnClick(func() { open.Set(!open.Get()) }).
        Children(ui.Text(pickedText, ui.RGB(0xe8e8f0))),
)
if open.Get() {
    menu := ui.Div().Col().Bg(ui.RGB(0x24242f)).Rounded(8).Border(1, ui.RGB(0x3b3b4f)).
        Shadow(18, 0, 8, ui.RGBA{A: 0.6}).
        Children(dropdownItem("Apple"), dropdownItem("Banana"), dropdownItem("Cherry"))
    root.Children(ui.Overlay(menu, 20, 252))
}
```

Escape closes the topmost dismissable modal.

### Drag and drop

`ui.Draggable(child, payload)` is a drag source; once the pointer moves past
a threshold a translucent ghost of the child follows the cursor.
`ui.DropZone(child, onDrop)` receives the payload on release, optionally
filtered by `Accept(func(payload any) bool)`.

```go
chips := ui.Div().Row().Gap(10).Children(chip(0xe03131), chip(0xf08c00))
drop := ui.DropZone(
    ui.Div().H(80).P(12).Bg(ui.RGB(0x1d1d27)).Rounded(12).Border(2, ui.RGB(0x3b3b4f)).
        Children(basketRow),
    func(p any) { basket = append(basket, p.(uint32)) },
)
```

### Embedding 3D

`ui.Viewport3D(v *render.View3D)` embeds a live 3D view as an ordinary
element: it reserves a box (`Size(w, h)` or `Grow()`), resizes the offscreen
target to its laid-out rect during layout, and paints the rendered frame as
an image, so the scene clips, scrolls and stacks with normal widgets.
`OnTap(func(x, y float32))` delivers clicks in viewport-local pixels for
picking; `Overlay(func(r *render.Renderer2D, rect ui.Rect))` paints 2D ink
over the frame; `Rect()` returns the laid-out box so camera code can route
drags that land inside.

The frame order is: UI layout (sizes the target), 3D pass into the target,
UI paint sampling it. From `examples/viewport3d`:

```go
view3d := render.NewView3D(device, r2, format)
s.vp = ui.Viewport3D(view3d).Grow().OnTap(func(x, y float32) {
    vw, vh := view3d.Size()
    vp, _ := cam.ViewProj(vw, vh)
    if o, d, ok := render.ScreenRay(vp, x, y, float32(vw), float32(vh)); ok {
        if hit, ok := r3.Pick(o, d); ok && hit.Tag >= 0 {
            s.selected.Set(hit.Tag)
        }
    }
})
u := ui.New(r2, font, s.view)

// per frame
u.Layout(float32(w), float32(h))       // resolves the viewport rect + target size
r3.Begin(vp, lightDir, eye)
scene(r3, cube, s.selected.Get())
enc := dev.CreateCommandEncoder()
view3d.Render(enc, r3)
r2.Begin(w, h)
u.Frame(float32(w), float32(h))
pass := enc.BeginRenderPass(target, gpu.Color{R: 0.09, G: 0.10, B: 0.12, A: 1})
r2.Flush(pass)
pass.End()
q.Submit(enc.Finish())
```

Under the shell the same work goes in `Hooks.Encode3D`, which runs between
`UI.Layout` and the 2D pass with the frame's encoder:

```go
hooks.Encode3D = func(_ *shell.App, enc *gpu.CommandEncoder, _, _ *gpu.TextureView) error {
    r3.Begin(vp, lightDir, eye)
    scene(r3)
    return view3d.Render(enc, r3)
}
```

One `Renderer3D` serves one `View3D` per submission; give simultaneous
viewports their own renderer.

### Tooling helpers

`ui.Bounds(e)`, `ui.Implicit(e)`, `ui.FixedSize(e)`, `ui.Children(e)`,
`ui.TextOf(e)`, `ui.FillColor(e)` and `ui.CornerRadius(e)` read a built
tree without rendering it, for tests and tooling; `DivEl.Rect()` reports the
rect the last layout assigned. `ui.ReplaceChild(root, old, repl)` patches a
subtree in place.

## How input and focus flow

`ui.UI` (from `ui.New(r, font, view)`) owns the frame: `Layout(w, h)` builds
and lays out the tree, `Frame(w, h)` paints it (flushing bindings first),
and the shell calls both every frame between `Renderer2D.Begin` and `Flush`.
Input arrives as `app.MouseEvent` and `app.KeyEvent` slices drained from the
window:

```go
u.Mouse(win.MouseEvents())
for _, k := range win.KeyEvents() {
    u.Key(k)
}
win.SetCursor(u.Cursor())
```

| Event | Routing |
|-------|---------|
| Pointer move | updates hover; `HoverBg`, `OnHoverChanged` and `Cursor()` read it at paint |
| Press and release | a click fires only if press and release hit a handler with the same bounds, since elements are rebuilt each frame and matched by rect. Overlays are hit-tested first, then the main tree deepest-first. A press on a `Draggable` arms drag-and-drop; on a slider, dial, scrollbar thumb, text field or `OnDrag` region it starts the corresponding drag |
| Wheel | the deepest scroll region under the pointer, bubbling outward per axis when it cannot consume the delta |
| Keys | `Tab`/`Shift+Tab` move focus through `Focusable()` elements and inputs in tab order (`TabIndex` first, then layout order), wrapping; `Escape` dismisses the topmost modal; printable and editing keys go to the focused `InputState`; a focused element with `OnKey` gets every event; `Enter` or `Space` activates a focused element's `OnClick` |

Keyboard-acquired focus is "focus-visible" (CSS `:focus-visible`): the UI
draws a generic ring unless the element paints its own via `FocusBorder` or
`FocusVisibleBorder`. `Div().Focus()` and `Input(...).Focus()` request
initial focus; `u.Focus(state)` focuses a field programmatically (nil to
blur); `state.HasFocus()` is QML's `activeFocus`.

Synthetic input for tests and tools: `u.Click(x, y)`, `u.Drag(x0, y0, x1, y1)`,
`u.Fling(x0, y0, x1, y1, vx, vy)`, `u.Hit(x, y)`. From `ui_test.go`:

```go
type k = app.KeyEvent
u.Key(k{Down: true, Key: app.KeyTab})   // focus the first button
u.Key(k{Down: true, Key: app.KeyEnter}) // activate it
u.Key(k{Down: true, Key: app.KeyTab})   // focus the input
u.Key(k{Down: true, Rune: 'h'})
u.Key(k{Down: true, Rune: 'i'})
field.Text.Get() // "hi"
```

Gesture handlers on a `Div`: `OnDrag(func(dx, dy, phase))` reports movement
since the press with `ui.DragStart`, `ui.DragMove`, `ui.DragEnd`;
`OnPointer` reports window coordinates instead; `OnFling(func(vx, vy))`
delivers release velocity in px/s; `OnSwipe(func(dir int))` fires once for a
mostly horizontal press-drag-release of 48 px or more. The shell folds
multi-touch into `app.Gestures` (pinch/pan) for hooks that want it.

## Theming

The controls (the kit's buttons, switches, menus, dialogs, drawers, tabs,
progress bars, tool tips, and the check boxes, sliders, inputs, combo and
spin boxes) are drawn from a `ui.Theme`: the accent, text, fields,
tracks, popup surfaces, bands (a menu bar, a dialog's button box, the
current tab), menus and tool tips. `ui.SetTheme(t)` sets it for the whole
program and redraws whatever caches the tree; the option structs
(`ButtonOpts`, `SwitchOpts`, `DialogOpts`, `DrawerOpts`, `ProgressOpts`)
take a `Theme *Theme` of their own.

| Theme | Look |
|-------|------|
| `ui.DefaultTheme()` | the controls' original look, and the default: light kit surfaces with dark-ground check boxes, sliders and inputs and a dark menu card |
| `ui.LightTheme()` | light throughout |
| `ui.DarkTheme()` | dark throughout |

```go
t := ui.DarkTheme()
t.Accent, t.AccentHover, t.AccentPress = ui.RGB(0x12b886), ui.RGB(0x20c997), ui.RGB(0x099268)
ui.SetTheme(t)
```

Beyond the theme, colour, radius, padding and shadow are set per element.
The pieces that carry defaults can also be restyled one by one:

| Widget | Setters |
|--------|---------|
| `Input` / `TextArea` | `Bg`, `Border(w, c)`, `Accent(c)` (focused border and caret), `TextColor`, `Placeholder(s, c)`, `Rounded`; the theme's input colours by default |
| `Slider` | `Colors(track, fill, knob)`, `Thickness(track, knob)`; `Bare()` paints nothing so a style can draw its own |
| `Scroll` / `List` | `BarColor(c)` (translucent white by default), `AutoHideBar()` |
| popups | `PopupSkin()` (the theme's card) / `DarkPopupSkin()` (dark card), applied only to a panel with no background of its own |
| text | `UI.SetLineHeight(rel)` for the window, `LineHeight` per element; `FontFamily("monospace")` and friends resolve through the renderer's font registry, which the shell fills with the platform's named families and their fallbacks |
| icons | `icons.SVG(name, color)` tints a stroked glyph at request time |

## Running headless in a test

`ui_test.go` drives a complete UI loop without a window: render into an
offscreen texture, inject a click, re-render, read the pixels back.

```go
r, _ := render.New2D(device, queue, gpu.FormatRGBA8Unorm)
font, _ := r.NewFont(goregular.TTF, 16)

var count core.Property[int]
var button *ui.DivEl
u := ui.New(r, font, func() ui.Element {
    button = ui.Div().Size(80, 32).Bg(ui.RGB(0x3355ff)).JustifyCenter().ItemsCenter().
        OnClick(func() { count.Set(count.Get() + 1) }).
        Children(ui.Text("+1", ui.RGB(0xffffff)))
    return ui.Div().Col().P(10).Gap(10).Bg(ui.RGB(0x202028)).Children(
        ui.Text(fmt.Sprintf("count: %d", count.Get()), ui.RGB(0xffffff)),
        button,
    )
})

r.Begin(W, H)
u.Frame(float32(W), float32(H))
enc := device.CreateCommandEncoder()
pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
r.Flush(pass)
pass.End()
queue.Submit(enc.Finish())
pix, _ := device.ReadTexture(queue, target, W, H)

br := button.Rect()
u.Click(br.X+br.W/2, br.Y+br.H/2) // count is now 1
```

`ui.New(nil, nil, view)` also works for layout-only tests that never paint.
For an application, `shell.ScreenshotUI` wraps all of this in one call:

```sh
cd examples
go run ./kitchensink -screenshot out.png
go run ./settings -frames 120   # windowed, deterministic exit
```
