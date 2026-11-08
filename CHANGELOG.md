# Changelog

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
