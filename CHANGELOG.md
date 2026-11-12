# Changelog

## Unreleased

- Rain drips from edges: the rain pass finds where the sky map's surface falls away by most of a metre (a roof's rim, the top edge of a box) and drops beads from it under gravity.

- Rain as particles: `FrameSettings.Rain` now also fills a box of air round the camera with falling drops and the ground with splashes (`render/rain.go`: stateless, one instanced draw, lit by the ambient light and every lamp, stopped by the sky map at roofs and the ground). `Atmosphere.Rain`'s screen-space streaks remain as an option.
- Lamp beams are shadowed: the haze reads the shadow maps of spot lights with `Shadow` set.
- `gpu.RenderPipelineDesc.DepthNoWrite`: depth test without depth write.

- Rain keeps out from under roofs: when a frame is wet or rained on the renderer draws a sky map (a depth map looking straight down on 64 m round the camera); wetness, puddles and `Atmosphere.Rain`'s streaks test it. Each raindrop also leaves a pale fleck where it lands.

- Weather: `FrameSettings.Wetness` wets every material surface (damp surfaces darken and gloss over; puddles gather on level ground and, with `SceneReflection`, mirror the scene), `FrameSettings.Rain` rings the puddles with drops, and `Atmosphere.Rain` draws rain in the air in three depth layers. Water and rained-on puddles now ripple their mirror image (a negative alpha in the HDR target marks them for Post).

- `FrameSettings.SceneReflection`: glossy surfaces mirror the scene itself. Opaque material draws leave their mirror strength in the HDR target's alpha and `render.Post` traces the reflected ray through the depth buffer (screen-space: what is off screen or hidden is not reflected), swapping the sky's reflection for what the ray meets.

- `SpotLight.Shadow`: spot lights cast shadows, the first `MaxSpotShadows` (4) of a frame, each through its own 1024 map (material draws; unskinned casters). `SetSpotLights` and `SetPointLights` take effect at `Begin`, which packs them (their comments said otherwise).

- Fixed for browsers: the plain 3D shader sampled its shadow map with `textureSampleCompare` inside a branch, and the 2D renderer compiled the subpixel text shader on devices without dual-source blending; both raised WebGPU validation errors that invalidated every frame.

- `render.Post`: an opt-in post-processing stack. A `Renderer3D` built for
  `FormatRGBA16Float` with `FrameSettings.Tonemap = TonemapHDR` writes
  linear radiance (unlit materials times `Emission`), and Post displays
  it: bloom, auto exposure, an ACES/AgX tone curve, a grade (split tone,
  S-curve, lift, saturation), vignette, chromatic aberration, FXAA,
  sharpening and film grain. `View3D.Post` runs it for a viewport.
- `Post` ambient occlusion (`PostSettings.AO`, `AORadius`; set
  `Post.Projection`): half-resolution, normals from depth, a depth-aware
  blur, applied to ambient light only. `Renderer3D.KeepDepth` and
  `CommandEncoder.BeginRenderPassDepthStore` keep the depth buffer for it.
- `Atmosphere.Lamps`: the renderer's point and spot lights light the air
  in the volumetric pass, so a spot shows its cone as a beam (unshadowed).
- `FrameSettings.SkyReflection`: materials mirror the sky (its gradient and
  the sun's glow) by Fresnel, sharp when glossy and smeared when rough.
  `Material.Water` with `FrameSettings.Time`: a wave map sampled as two
  layers scrolling across each other.
- `Post.Atmosphere`: light shafts (the view ray marched against the sun's
  shadow map at half resolution, a two-lobe phase, air thinning with
  height, an analytic far haze) and an analytic sky (gradient, a warm band
  toward the sun, its glow and disc). `Post.View`, `Post.UseShadow`.
- The sun's shadow volume snaps to its own texels, so shadow edges hold
  still while a camera it follows moves (they shimmered).
- `FrameSettings.SunSoftness`: PCSS sun shadows for material draws, crisp
  at contact and wider with distance from the caster, with an analytic
  receiver-plane bias (no screen derivatives).
- `FrameSettings.Sun`: the directional light's colour and energy (it was
  white at a fixed 2.6, still the default), for material draws.
- A release more than `ui.TapSlop` (4 px) from its press over a `Viewport3D`
  is a drag (an orbit, say), not a tap: `OnTap` no longer runs for it.
- `Viewport3DEl.View` returns the View3D an element shows.
- `navcube.Options.Axes` draws the world axes from the cube's (-1, -1, -1)
  corner along its edges, depth-tested; `navcube.ViewProj`, `Scale`,
  `AxisTip` and `AxisColors` let a host label them.
- Linux joysticks are read with raw non-blocking syscalls: a quiet
  `/dev/input/jsN` no longer parks `Gamepads` until its next event.

## v0.3.0 (2026-11-11)

Accessibility, menus and a lighter gml: every control describes itself in
an accessibility tree, menus gained icons, context menus and submenus that
open on hover, gml translates shown text by convention, settled gml frames
allocate a third of what they did, and 3D views render at full resolution
on scaled displays.

### Upgrading from v0.2.0

- **`markdown/` (the editor app) left gfx.** It is its own module now,
  `github.com/sushvibez/markdown`. gfx keeps `md`: the parser, the layout
  and the ui element the editor builds on.
- **`gml.Env.Func` panics on a signature it cannot call.** It used to
  accept any function and fail only when markup called it ("not
  callable"). The supported shapes are `func()`, `func(float64)`,
  `func(float64, float64)`, `func(string)`, `func(bool)` (new) and
  `func([]any) (any, error)`.
- **`render.View3D.Size` is the logical size.** `ui.Viewport3D` now sizes
  its target in physical pixels (`View3D.ResizeScaled` with the 2D
  renderer's scale), so code that measures in texels (readback,
  pixel-exact widths) wants the new `View3D.PixelSize`. Picking and
  camera aspect keep using `Size` with mouse coordinates, unchanged.
- **The shells set the DPI scale before layout,** not after it, so the
  first frame lays out at the right scale.

### Accessibility (`ui`, gml)

- Every element can carry an `Access`: a role (ARIA's set), a name, a
  description, a value and states. `UI.AccessTree` builds the tree a
  platform bridge presents, and `AccessActivate` does what a click does.
- Every kit control describes itself; a labelled `Switch` is one switch
  with a clickable label.
- gml: `Accessible.role`, `.name` and `.description`. The platform
  bridges (UIA, AT-SPI, ARIA) come next.

### Menus and input

- Menu icons, a chevron on submenus, submenus that open on hover (fresh
  each time), a text size, and menus taller than the window scroll.
- gml: context menus at the pointer; menus from a model, `onClosed`,
  `visible` bound both ways, `Action`-only shortcuts, `Menu`
  `font.pixelSize`; a popped-up menu stays inside the window.
- Secondary clicks and `MouseArea.acceptedButtons`.
- `TextField` `onTextChanged` and `onEditingFinished`; `UI.TextFocused`
  for hosts that leave keys to a field being typed in.
- Table columns resize and move (`TableOpts.Resizable`, `Reorderable`).
- Ctrl+1 … Ctrl+9 arrive on Windows.

### gml

- **Translation by convention:** `SetTranslations` installs a catalogue
  (source text → text to show), and any text an element shows that
  matches a key exactly shows its value. `qsTr` stays a no-op, so QML
  pages run unchanged.
- **Speed:** a settled frame allocates a third of what it did (buttons 823
  → 310 allocations a frame) and the element registry persists across
  frames, so a cache hit replays nothing (buttons 94 → 79 µs a frame).
  `TestCorpusFrameAllocs` holds each corpus page under a budget.
- `Qt.SizeHorCursor`; a closed `Popup` takes no space in its parent's
  flow; an `Image` source's `?query` names a version of the same file, so
  changing it reloads.
- A `Text` with no size is Qt's 13 px application font; a dark page keeps
  Qt's light Basic bands; `MonthGrid` opens a week early when the 1st is
  a Sunday.
- Automated parity scores for the QML corpus (`qml-compare.sh`, runnable
  in WSL).

### Rendering and platform

- `ui.Viewport3D` renders at physical pixels on scaled displays
  (`View3D.ResizeScaled`, `PixelSize`, `render.ScaledPixels`).
- A `Scroll` skips painting content scrolled out of its viewport (2000
  rows: 582 → 85 µs); layout, hit testing and focus are unchanged.
- `Renderer3D.Release` frees everything: two bind group layouts kept the
  line pipeline, and about 38 MB of driver memory, alive.
- `Layer.Blur` is documented as blurring the layer's own content; there
  is no backdrop blur yet.
- `app.Window.SetIcon`, `Window.Maximized`, `app.OpenURL`.
- `Hooks.Offscreen3D` keeps the 4x MSAA 2D path for apps whose 3D is
  only offscreen views.
- The workspace-local wgpu-native (`build/wgpu`) is found from any depth
  below it.

## v0.2.0 (2026-11-08)

The controls kit: QML's Quick Controls as Go constructors in `ui`, drawn
in one theme, with gml's controls built on the same functions. See the
"Controls kit" and "Theming" sections of `docs/ui.md`, and
`examples/controls` for every control on one page.

### Upgrading from v0.1.0

- **The controls start in the light theme.** Check boxes, sliders and text
  inputs used to be drawn for a dark page while buttons and dialogs were
  light. A program now starts in `ui.LightTheme()`. A dark app calls
  `ui.SetTheme(ui.DarkTheme())` once at start, which keeps those widgets
  the colours they had; `ui.ClassicTheme()` is the old mixed look.
- **`ui.TableView` and `ui.TreeView` return `ui.Element`** (a box around
  the rows when the table has a header or takes the keyboard).
- **A click on the empty part of a floating panel** (a dialog, a menu, a
  drawer) no longer falls through to the scrim and dismisses it.

### Controls (`ui`)

- Buttons: `Button`, `ToolButton`, `RoundButton`, `DelayButton`, with
  `ButtonOpts`; `Switch`.
- Menus: `MenuBar`, `MenuPanel` and `MenuItem` (checkable entries,
  exclusive groups, submenus, shortcuts, the keyboard).
- `Dialog`, `DialogFrame`, `DialogButtonBox` with Qt's standard buttons.
- `Drawer`: it slides in from an edge, can be dragged shut, and can be
  pulled open from the edge.
- `TabBar`, `PageIndicator`, `Tumbler`, `SplitView`.
- `ProgressBar` (it can be indeterminate), `BusyIndicator` and `Activity`.
- `ToolTip` and `ToolTipState`.
- `DialControl`, `Separator`, `ToolBar`, `GroupBox`; `Pane`, `Frame` and
  `Page` are sugar for `Div`.
- Data views: `GridView`; `TableView` with `HeaderView` (row selection,
  editing a cell in place, sorting from the header, a filter, virtualised
  rows, the keyboard); `TreeView` (expand and collapse, virtualised rows,
  the keyboard).
- Earlier in the cycle: `ButtonGroup`, `TriCheckbox`, `SpinBox`,
  `ComboBox`, input masks, `PopupState` enter/exit animations, keyboard
  focus navigation and focus scopes, key sequences and `DivEl.Shortcut`,
  `Mover`, `ImagePlayer`, `Palette`, and double-click, press-and-hold,
  wheel, hover-position, pinch and touch handlers.

### Theming (`ui`)

- `Theme` holds the colours the controls are drawn in. `SetTheme` changes
  it for the program, and the option structs take a `Theme` of their own.
- `ThemeScope` themes part of a tree.
- `Theme.WithAccent` derives the hover and press shades from one colour.

### gml

- The controls are built on the `ui` kit above.
- `Material.theme`/`accent`, Universal's attached properties and the
  palette roles theme the controls inside an item. A page starts light or
  dark to suit its background.
- `ActionGroup`; `Action.shortcut` works in menus.
- Drawers slide in from their edge, including styled ones.
- `DelayButton` `delay`, `onActivated` and `progress`.
- States and transitions; `SequentialAnimation`.
- `Shortcut`, `Settings`, `SystemPalette`, `BorderImage`, `AnimatedImage`.
- `MouseArea` `drag.target`, `DragHandler`, `WheelHandler`,
  `PinchHandler`, `PointHandler`.
- Keyboard focus, `FocusScope`, `Window.activeFocusItem`.
- ListView header, footer and horizontal virtualisation.

### Platform and tooling

- `core/settings`: settings files for Go code.
- Android pointer events in logical pixels; `ui/shell` wheel steps and
  `Input.PointerScale`.
- GitHub Actions check workflow.
- Go 1.27.3.
- `scripts/check.sh` renders `examples/controls` in its screenshot smoke
  test.

## v0.1.0 (2026-11-06)

First public release.
