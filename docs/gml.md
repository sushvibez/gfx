# gml — the QML-compatible markup engine

`script/gml` (`github.com/sushvibez/gfx/script/gml`) reads `.gml` and `.qml`
files written in QML syntax and turns them into the framework's `ui`
element tree, every frame, with live bindings. The goal is to run real QML
files unmodified: a corpus of 65 Qt Quick pages (controls, layouts, canvas,
shapes, particles, states, 3D) renders without warnings, and the remaining
work is visual and interaction parity, judged page by page against Qt.

This document is the language reference and user guide. It is derived from
the source; where the engine accepts something it does not implement, that
is said here as a no-op rather than left implied.

## Running a file

```bash
cd examples
go run ./qml -file qml/gpui/hello_world.gml -w 600 -h 400 -screenshot out.png   # headless
go run ./qml -file path/to/Main.qml                                            # window, hot reload
go run ./qml -file page.gml -screenshot out.png -script @steps.txt             # scripted PNG per step
```

`examples/qml` is the generic runner. It registers a set of Go-backed
types (`gml.RegisterType`, see below), loads the file with `gml.Watch`,
sets the host properties the corpus pages expect (`fullSize`, `appDirPath`)
and runs the frame loop through `ui/shell`. Flags:

| flag | meaning |
| --- | --- |
| `-file` | the document (default: `app.gml` next to the example) |
| `-w`, `-h` | window / render size (default 560 x 560) |
| `-screenshot out.png` | render offscreen and write a PNG, no window |
| `-frames N` | exit after N frames |
| `-script text\|@file` | drive clicks/keys headlessly, one PNG per step |

Native runs need wgpu-native (`scripts/fetch-wgpu.sh`, or `GFX_WGPU_LIB`)
and fonts (`scripts/fetch-fonts.sh`, or `GFX_FONT` / `GFX_CJK_FONT`).

Hot reload: while the window is open, saving the file reloads it on the next
frame (mtime polled at most every 200 ms). A parse error keeps the last good
document and shows an error banner until the file parses again. Declared
properties, text-field contents and scroll positions survive a reload
(state is keyed by name/id, not tree position), and anything the host set
with `SetProperty` is re-applied. Component files (`<Name>.gml` next to the
document) are parsed once and do not reload on their own: save the main
document to pick up a changed component.

Lenient mode: a document whose path ends in `.qml` is built leniently — an
element that fails to build is logged (`gml: ... (element skipped)`) and
replaced by an empty box, as Qt's runtime warns and renders. Authored
`.gml` is strict: the first build error replaces the page with an error
banner naming the line.

## The Go API

A document is a per-frame view function. `Load` parses and binds it to an
`Env` (the scope the Go side shares with markup); `View()` returns the
function the `ui` layer drives.

```go
package main

import (
    "github.com/sushvibez/gfx/core"
    "github.com/sushvibez/gfx/script/gml"
    "github.com/sushvibez/gfx/ui/shell"
)

func main() {
    env := gml.NewEnv()

    // Host state the markup can read and write by bare name.
    volume := &core.Property[float32]{}
    env.Float("volume", volume)
    env.Func("save", func() { /* ... */ })

    doc, err := gml.Load("app.gml", env)     // or gml.Watch for hot reload
    if err != nil {
        panic(err)
    }
    doc.SetProperty("fullSize", true)          // a `property bool fullSize` the file declares

    opts := shell.Options{Title: "app", Width: 800, Height: 600}
    if err := shell.RunUI(opts, doc.View()); err != nil {
        panic(err)
    }
    // headless: shell.ScreenshotUI(opts, doc.View(), "out.png")
}
```

| API | what it does |
| --- | --- |
| `gml.NewEnv()` | a fresh scope with the JS globals and QML enums bound |
| `env.Int/Float/Bool/Str(name, *core.Property[T])` | expose a property; markup reads it live and `name = v` writes it |
| `env.Func(name, fn)` | expose a Go function (`func()`, `func(float64)`, `func(string)`, `func(float64, float64)`, `func([]any) (any, error)`) |
| `env.Set(name, v)` | expose any value (a `*render.View3D`, a map, a slice); a changed value bumps the frame revision |
| `env.Get(name)` | read a document-scope value back (a property `Ref`, a text field's `*ui.InputState`, ...) |
| `env.Input(name, *ui.InputState)`, `env.Scroll(name, *ui.ScrollState)` | share text/scroll state with hand-written ui code |
| `gml.Load(path, env)`, `gml.LoadString(src, env)`, `gml.LoadStringAt(src, dir, env)` | parse and bind; `Load` makes the file's directory the base for images, fonts, components and modules, `LoadStringAt` uses `dir` for embedded markup; `Load` turns QML's lenient mode on for `.qml` files, `Document.SetLenient` does it for the others |
| `gml.Watch(path, env)` | `Load` plus hot reload; returns a `*Live` with the same `View`/`SetProperty`/`DeclaresType` and an `Err()` for hosts drawing their own chrome |
| `doc.View()` | the per-frame `func() ui.Element` |
| `doc.SetProperty(name, v)` / `doc.Property(name)` | write / read a property the document root declares; a host write breaks the binding, as an assignment in markup would |
| `doc.DeclaresType("View3D")` | whether the markup uses a type, so the host can install the hooks it needs |
| `gml.SetView3DGPU(...)`, `gml.EncodeView3D(enc)` | the two calls a host makes for `View3D` scenes (see `examples/qml/main.go`) |
| `gml.Format(src)` | gofmt-for-gml: parse and pretty-print (comments are dropped) |
| `gml.NewBuilder(typeName)` | construct a document programmatically and `String()` it |

### Go-backed element types

`gml.RegisterType` is the `qmlRegisterType` equivalent: a factory returns a
`NativeObject` with bindable `Props` and invokable `Methods`, and the
element is instantiated from markup like any other (one instance per id,
retained across frames).

```go
gml.RegisterType("WebSocketClient", func(emit gml.EmitFunc) *gml.NativeObject {
    connected := &core.Property[bool]{}
    return &gml.NativeObject{
        Props: map[string]any{"connected": gml.BindBool(connected)},
        Methods: map[string]any{
            "connectToServer": func() { connected.Set(true); emit("connected") },
        },
    }
})
```

```qml
WebSocketClient {
    id: ws
    onMessageReceived: function(message) { log.append({text: message}) }
}
Button { text: ws.connected ? "disconnect" : "connect"; onClicked: ws.connectToServer() }
```

`BindBool`, `BindString`, `BindFloat`, `BindProp[T]` adapt a
`core.Property`; `NewVarProp`/`BindVar` is a `property var` whose contents
change (a results array). `emit(signal, args...)` fires the element's
`on<Signal>` handler with the arguments bound and is safe from any
goroutine: emissions queue and drain on the UI thread at the start of the
next frame. `gml.Post(fn)` queues any other work the same way, since
properties and models are single-owner (the event-loop goroutine).

`gml.RegisterPaintedType` is the `QQuickPaintedItem` equivalent: the same
shape plus a `Draw(r *render.Renderer2D, f *render.Font, rect ui.Rect)` that
paints the laid-out box.

## Syntax

A file is an optional header followed by exactly one root element.

```qml
import QtQuick 2.15              // header: import / pragma lines
import QtQuick.Controls          // version optional
import QtQuick.Templates as T    // namespace alias
pragma ComponentBehavior: Bound  // accepted, ignored

Rectangle {                      // root element
    id: root                     // id
    width: 320; height: 200      // attributes; `;` or newline separates
    color: "#131318"             // colour strings; #rrggbb / #aarrggbb literals also lex unquoted
    border.width: 1              // group property, dotted
    border { color: "#333" }     // group property, block form (folded into dotted attributes)
    property int count: 0        // property declaration
    readonly property real half: width / 2
    signal tick(int value)       // signal declaration
    function bump(n) { count += n }   // method
    onTick: bump(value)          // signal handler: statements, or a { block }
    Text { text: "count " + count }   // child element
    Behavior on width { NumberAnimation { duration: 200 } }   // `Type on <prop>` clause
    states: [ State { name: "big" } ]  // element-valued list
    gradient: Gradient { GradientStop { position: 0; color: "red" } }  // element-valued attribute
    component Card: Rectangle { radius: 8 }   // inline component
}
```

Comments are `//` and `/* */`. Newlines end an attribute unless the
expression continues: a line that starts with a binary operator, `?`, `.`
or `?.` continues the previous one, as JS allows.

### Bindings

Every attribute value is an expression re-evaluated against the live scope
whenever something it read has changed, so `text: "count " + count` is a
binding with no binding syntax. Three value forms exist:

- an expression — `width: parent.width / 2`;
- a block binding — `text: { if (x > 0) return "pos"; return "neg" }`, a
  statement block whose `return` is the value (a `{` followed by `key:` is an
  object literal instead);
- an element — `delegate: Item { ... }`, `background: Rectangle { ... }`.

Assigning a declared property imperatively (`count = 5` in a handler, or
`SetProperty` from Go) breaks its default binding, as QML does. Built-in
geometry (`x`, `y`, `width`, `height`) is layout-derived: it can be read in
expressions but cannot be assigned or animated by a standalone animation.

### Signal handlers

An attribute whose last segment starts with `on` is a handler:
`onClicked: count++`, `onClicked: { a(); b() }`, `onClicked: a(); b()`,
`Component.onCompleted: init()`, `Keys.onPressed: ...`,
`onTick: function(value) { ... }`. Handler bodies are the JS subset below.
A declared `signal tick(int value)` is emitted by calling `tick(1)`; its
`onTick` handler on the same element runs with `value` bound.
`on<Prop>Changed` handlers fire on declared properties and control state.
`Connections { target: id; function onMoved(dx) { ... } }` attaches a
handler from elsewhere, and so does the Qt 5 form `onMoved: ...`, which
sees the signal's own parameter names. A signal declared in markup passes
its arguments to both, and is callable from outside as `id.moved(3)`.
Element signals such as `onClicked` reach a Connections handler without
arguments.

### Property declarations

```qml
property int count: 0
property real ratio                   // no initializer: the type's zero
property bool open: true
property string title: "x"
property color accent: "#3b5bdb"      // a colour object: accent.r/.g/.b/.a read
property var items: [1, 2, 3]         // any JS value
property list<Item> pages: []         // typed lists hold as var
property alias taps: chip.hits        // forwards to another element's property
property FontLoader body: FontLoader { source: "fonts/Inter.ttf" }  // element-valued
readonly property real half: width / 2
required property string name         // a delegate slot filled from the model
default property alias contentData: panel.data   // where a use site's children go
```

Types honoured: `int`, `real`/`double`, `bool`, `string`, `color`, `var`,
`alias`. Every other type name (`Item`, `point`, `date`, `url`, `list<T>`,
...) is accepted and held as `var`. `readonly` and `required` are parsed and
otherwise informational. An element-valued default instantiates the types
the engine models (`FontLoader`) and holds `null` for the rest. A default
whose expression reads an element that has not been built yet (a control's
`value`) is retried after the first build.

A `color` property reads back as a colour object, so `accent.r` works and
`Qt.rgba(accent.r, accent.g, accent.b, 0.5)` means what it does in Qt; a
`string` holding `"#00d1a9"` is just text (`.r` is undefined), which is also
Qt's rule.

### Scope and name resolution

A bare name resolves, innermost first: handler/function locals; the
delegate row (`index`, `model`, `modelData`, every role by name); the
component instance's declared properties and ids; the document's declared
properties, functions, ids and host `Env` values; the frozen builtins
(`Math`, `Qt`, enums). Inside an item, `width`, `height`, `opacity` and
`visible` resolve to the item's own values (`radius: height / 2`), and a
Flickable's `contentX`/`contentY`/`contentWidth`/`contentHeight` likewise.
`parent` is the enclosing item. An id object exposes the element's declared
properties, functions, live control state (`checked`, `value`, `text`,
`containsMouse`, `pressed`, `hovered`, `currentIndex`, ...) and the laid-out
geometry from the previous frame (`x`, `y`, `width`, `height`, `right`,
`bottom`, `implicitWidth`, `contentWidth`, ...). Geometry is zero on the
first frame and settles on the next.

### Components

Four ways to define a reusable element:

- `Component { id: x; Item { ... } }` — instantiated by `Loader {
  sourceComponent: x }`, `StackView.push(x)`, or as a view's `delegate`.
- `component Card: Rectangle { ... }` — inline component, used as the type
  `Card { }` in the same file.
- a file component: an element type with no built-in meaning resolves to
  `<dir>/<Type>.gml`, then `<dir>/<Type>.qml`, then a type a `qmldir`
  module registered. `dir` is the main document's directory (a file
  component's own relative assets and imports resolve against the main
  document, not the component file).
- `Loader { source: "screens/Foo.qml" }` — a component addressed by
  relative path.

At a use site, attributes naming a declared property of the component
override its default (evaluated in the use site's scope, so `root` means
the page's root even when the component has its own `id: root`); other
attributes (`x`, `width`, `anchors.*`, `Layout.*`) apply to the instance
root; the use site's children append to the component root's children, or
to the element a `default property alias ...: id.data` names. Each use site
is its own instance: declared properties and ids inside the component are
scoped per instance, and the use-site id (`Card { id: c }`) resolves to the
instance so `c.title` reads that instance's value. A component's own
`function`s and `Component.onCompleted` run per instance.

### Imports and modules

The header's `import` lines are honoured as follows:

| import | effect |
| --- | --- |
| `QtQuick`, `QtQuick.Controls`, `QtQuick.Layouts`, `QtQuick.Shapes`, `QtQuick.Particles`, `QtQuick3D`, `QtMultimedia`, `QtLocation`, `QtQuick.Pdf`, `QtGraphs`, `Qt5Compat.GraphicalEffects`, `QtQuick.Effects`, `Qt.labs.*`, `QtQuick.Dialogs` ... | no module directory found: skipped. The element vocabulary for these is built in; the version is ignored |
| `QtQuick.Templates as T` | the alias makes `T.Button { }` and friends build as Templates controls (style files) |
| `import MyModule` (a directory with a `qmldir`) | searched as `<A>/MyModule`, `<A>/imports/MyModule`, `<A>/styles/MyModule` for the document directory `A` and up to five ancestors; dots become path separators |
| `import "dir"` / `import "file.js" as X` | not resolved (quoted imports and JavaScript libraries are not loaded) |
| `pragma ...` | skipped |

From a `qmldir`, `singleton Type 1.0 Type.qml` loads once per document
and exposes the file's properties and functions as one shared object
(`Style.mainColor`, `Style.resize(4)`); `Type 1.0 Type.qml` registers a
file component; `module`, `plugin`, `depends`, `typeinfo` and the rest are
ignored. A `qtquickcontrols2.conf` with `Style=<module>` in the document
directory or an ancestor selects an application-wide Controls style.

A module that provides its own `Button.qml`, `Slider.qml`, `Dialog.qml`
... shadows the built-in control in every file that imports it (QML import
shadowing). The style file's root is a `T.*` Templates control; the engine
composes its `background`, `contentItem`, `indicator`, `handle`,
`up.indicator`/`down.indicator`, `first.handle`/`second.handle`, `header`,
`footer`, `delegate` and `Overlay.modal` delegates and seeds the control
contract (`text`, `enabled`, `flat`, `checked`, `down`, `hovered`,
`pressed`, `value`, `from`, `to`, `position`, `visualPosition`, `padding`,
`availableWidth`, `palette.*`, `font`, `icon`, `popup.visible`, ...) into
the file's id object. Shadowed kinds: Button, Label, CheckBox, Switch,
RadioButton, ToolButton, Slider, RangeSlider, TextField, TextArea,
ComboBox, ProgressBar, Dial, SpinBox, Tumbler, BusyIndicator, Popup,
Dialog, Menu, MenuItem, ToolTip. Other styled controls keep the built-in
rendering.

## The JavaScript subset

Expressions and handler bodies are JavaScript, evaluated by a tree-walking
interpreter over the value types `float64`, `string`, `bool`, `null`,
`[]any` (arrays) and `map[string]any` (objects). Every number is a
`float64`; `===`/`!==` behave like `==`/`!=` (the engine is loosely typed).

### Supported

Literals: decimal, `0x`/`0b`/`0o`, exponents (`1e9`); `'...'`/`"..."` with
`\n \t \uXXXX` (surrogate pairs join); template literals with `${expr}`;
`#rrggbb`/`#aarrggbb` unquoted; `true false null undefined`; array literals
with spread (`[...a, 1]`) and inline elements (`[ State { } ]`); object
literals `{ key: v, "key": v }`; regex literals `/ab+/gi`, RegExp values
with `test`, `exec`, `source`, `flags`, `global` (also `RegExp(src, flags)`;
Go's RE2 syntax, so no lookaround or backreferences), which `replace`,
`replaceAll`, `match`, `search` and `split` accept, `$1`/`$&` and function
replacements included; `function (a, b = 1) { }` and arrow functions `x => x + 1`,
`(a, b) => { ... }` — closures over the enclosing scope.

Operators: `+ - * / %`, `< > <= >= == != === !==`, `&& || !`, `?:`, `??`,
`?.`, `| & ^ ~ << >>`, unary `- + typeof`, `new T(...)` (calls `T`),
member `.`, index `[ ]`, call, assignment `= += -= *= /= %= |= &= ^=` (as
expressions), `++`/`--` prefix and postfix.

Statements: `var`/`let`/`const` (several declarators), `if/else`, `for(;;)`,
`for (x of iterable)`, `while`, `switch/case/default` with fall-through,
`break`, `continue`, `return`, `throw`, `try/catch/finally`, function
declarations inside bodies, blocks, expression statements. Loops are capped
at ten million iterations per run. QML 6 type annotations on parameters,
return types and catch bindings are skipped.

Globals: `Math` (constants plus `abs floor ceil round trunc sqrt cbrt exp
expm1 log log2 log10 log1p sin cos tan asin acos atan sinh cosh tanh atan2
hypot sign max min pow random`); `console.log/warn/error/info` (stderr);
`JSON.stringify/parse`; `Number()`, `String()`, `String.fromCharCode`,
`Boolean()`, `parseInt`, `parseFloat`; `new Date()` / `Date(ms)` /
`Date("2024-01-02")` / `Date(y, m, d, h, min, s, ms)` with `getTime
getFullYear getMonth getDate getDay getHours getMinutes getSeconds
getMilliseconds toLocaleTimeString toLocaleDateString toLocaleString
toISOString toString valueOf`; `animate(prop, to, ms)` (tweens an exposed
or declared property).

Methods: numbers `toFixed toPrecision toString toLocaleString valueOf`;
strings `length toUpperCase toLowerCase trim trimStart trimEnd charAt
indexOf lastIndexOf includes startsWith endsWith substring slice substr
split replace replaceAll repeat padStart padEnd`; arrays `length join
indexOf includes slice map filter forEach some every find reduce` plus the
mutators `push pop shift unshift splice reverse sort`; everything answers
`toString()` and `valueOf()`.

`Qt` namespace: `rgba rgb hsla hsva color lighter darker` (colours are
`#aarrggbb` strings tagged as colours), `formatTime formatDate
formatDateTime` (the common `yyyy MM dd hh mm ss ap` tokens),
`resolvedUrl` (identity), `callLater(fn, args...)` (runs at the top of the
next frame), `vector2d vector3d vector4d size point rect` (plain objects),
`locale(name)` (separators, currency and AM/PM for the common locales,
en-US for the rest), `Qt.Key_*` codes and `Qt.*Modifier` bits, the
alignment, orientation, edge, sort-order, check-state and cursor enums and
the named colours `black white red green blue yellow transparent`.

### Not supported

Destructuring, default/rest in arrow parameters, `in`, `instanceof`,
`delete`, `void`, `do...while`, `for...in`, labels, classes, getters and
setters, prototypes, `this` (functions resolve names lexically), computed
object keys, shorthand object properties, `>>>`, `**`, bitwise on anything
but small integers, `Array.isArray`, `Object.keys/values/entries/assign`,
`Array.from`, `Promise`/`async`, `setTimeout`/`setInterval`,
`String.prototype` methods beyond the list above, translation catalogues
(`qsTr`, `qsTranslate` and `qsTrId` return their source text, with `%n`
filled in), the `Qt.*` members not listed (`Qt.binding`, `Qt.createComponent`, `Qt.createQmlObject`,
`Qt.quit`, `Qt.openUrlExternally`, `Qt.application`, `Qt.platform`),
`Screen`, `Qt.labs.settings` persistence, JavaScript library imports
(`.js` files). Arrays are values: a mutator (`push`, `sort`, ...) on a
property, a local or an element of one writes the result back through it,
and on a temporary (`a.slice().sort((x, y) => x - y)`) returns the result.
Call arguments spread (`Math.max(...xs)`), and a thrown object reaches
`catch` intact.

### Example

```qml
Column {
    padding: 20; spacing: 8
    color: "#131318"
    property var items: [3, 1, 2]
    property var cfg: { "name": "demo", "size": 4 }
    property var missing: null

    function total(xs) {
        let sum = 0
        for (const x of xs) sum += x
        return sum
    }
    function labels() {
        const more = [...items, 9]
        return more.map(v => `n${v}`).join(", ")
    }
    function sorted() { var s = items.slice(); s.sort(); return JSON.stringify(s) }

    Text { text: "sum = " + total(items); color: "#e8e8f0" }
    Text { text: labels(); color: "#e8e8f0" }
    Text { text: "cfg.name = " + cfg.name + ", size*2 = " + cfg.size * 2; color: "#e8e8f0" }
    Text { text: "missing?.x ?? 'none' = " + (missing?.x ?? "none"); color: "#e8e8f0" }
    Text { text: Qt.formatTime(new Date(2024, 0, 2, 13, 5, 9), "hh:mm:ss"); color: "#e8e8f0" }
    Text { text: "sorted " + sorted(); color: "#e8e8f0" }
}
```

## Element catalogue

Every type the engine builds, grouped as QML does. "Accepted" means the
attribute parses and evaluates but changes nothing; such lists are the
honest no-ops. Unknown attributes are an error on strict `.gml` (the
message names the element and attribute) and skip the element in lenient
`.qml`.

### Common item attributes

These apply to every box-like item (Item, Rectangle, MouseArea, the
positioners, layouts and Controls containers):

| attribute | status |
| --- | --- |
| `id`, `width`, `height`, `implicitWidth`, `implicitHeight`, `color`, `radius`, `border.width`, `border.color`, `border.style` (`"dashed"` extension), `opacity`, `visible`, `clip`, `gradient` (`Gradient { GradientStop }`, vertical/horizontal, 2 stops), `rotation`, `scale`, `transformOrigin`, `transform` (`Translate`, `Rotation`, `Scale` list, painting only) | honoured |
| `anchors.fill`, `centerIn`, `left/right/top/bottom`, `horizontalCenter`, `verticalCenter`, `margins`, `leftMargin/rightMargin/topMargin/bottomMargin`, `horizontalCenterOffset`, `verticalCenterOffset`; `x`, `y` | honoured; an item with anchored or positioned children becomes an anchor layout. `baseline`, `baselineOffset`, `alignWhenCentered` are accepted |
| `Layout.fillWidth/fillHeight`, `preferredWidth/Height`, `minimumWidth/Height`, `maximumWidth/Height`, `alignment`, `margins`, `*Margin`, `columnSpan`, `rowSpan` | honoured inside RowLayout/ColumnLayout/GridLayout |
| `state`, `states`, `transitions`, `Behavior on <prop>` | honoured (see Animations and states) |
| `onClicked`, `onPressed`, `onReleased`, `onEntered`, `onExited`, `onContainsMouseChanged`, `onHoveredChanged`, `cursorShape` (`PointingHandCursor`, `IBeamCursor`), `focus`, `Keys.onPressed`, `Keys.onReleased` | honoured; any box with handlers is a hit area, and `id.containsMouse` / `id.pressed` read live |
| `layer.enabled` with `layer.effect` blur (`MultiEffect.blurEnabled`, `GaussianBlur`, `FastBlur`, `RecursiveBlur`), `DropShadow`/`Glow` shadow | honoured as an offscreen layer |
| `Component.onCompleted` | runs once, after the first laid-out frame (so geometry reads work) |
| `ToolTip.text`, `ToolTip.visible`, `ToolTip.delay`, `ToolTip.timeout` | honoured (attached tooltip floated over the item) |
| `enabled`, `z`, `antialiasing`, `smooth`, `activeFocusOnTab`, `objectName`, `parent`, `baselineOffset`, `on<Geometry>Changed`, `onVisibleChanged`, `onOpacityChanged`, `onActiveFocusChanged`, `title`, `flags`, `modality`, `visibility`, `hoverEnabled`, `focusPolicy`, `*Padding` on plain items, `Component.onDestruction`, `drag.filterChildren`, `drag.smoothed`, `acceptedButtons`, `propagateComposedEvents`, `preventStealing`, `onCanceled`, `xAxis.*`/`yAxis.*`, `gesturePolicy`, `onTranslationChanged` | accepted, not modelled |

Extensions beyond QML, used by the GPUI-port examples: `justify`, `items`
(flexbox alignment on a box), `grow`, `spacing` and `padding` on a plain
Rectangle/Column/Row (a plain box is a flex column), `hoverColor`,
`pressColor`, `shadow: [blur, ox, oy, color]`, `glow` on Text, `textColor`
on Button, `Spacer { }`.

### Items and positioners

| element | notes |
| --- | --- |
| `Item`, `Rectangle`, `FocusScope` | a box; no implicit size inside a layout, as in Qt |
| `MouseArea` | a hit box with click/press/hover; `containsMouse`, `pressed`, `mouseX`, `mouseY` live; `onDoubleClicked`, `onPressAndHold` (Qt's 800 ms; the release is then not a click), `onWheel` (`wheel.angleDelta`, `pixelDelta`; setting `wheel.accepted = false` lets the wheel scroll the view behind), `onPositionChanged` (while pressed, or hovering with `hoverEnabled`); handlers get `mouse`/`wheel`, by that name or as an arrow function's parameter. `drag.target` (over `ui.Mover`, the Go API for the same drag) moves the target's `x`/`y` with the pointer (breaking their bindings) once it passes `drag.threshold` (default 10), along `drag.axis` (`Drag.XAxis`, `YAxis`, `XAndYAxis`), inside `drag.minimumX`…`drag.maximumY`; `id.drag.active` reads true meanwhile; a press that never passes the threshold still clicks. A handler inside the MouseArea reads it as `id.drag.active`, not bare `drag.active`; `onPositionChanged` does not fire while the drag carries the area along under the pointer; `drag.filterChildren`, `drag.smoothed` are accepted |
| `TapHandler`, `HoverHandler`, `DragHandler`, `PinchHandler`, `WheelHandler`, `PointHandler` | rewritten to a MouseArea on the parent: `onTapped`/`onSingleTapped` → click, hover works. `DragHandler` drags its `target` (the item it sits in by default; `null` moves nothing and only reports) over `ui.Mover`, with `xAxis`/`yAxis` `minimum`/`maximum`/`enabled`, `dragThreshold`, `active`, `activeTranslation`, `persistentTranslation`, `onTranslationChanged(delta)` and `onActiveChanged` (reading bare `active`); pinch, wheel and point handlers do nothing |
| `DropArea` | built; drops are not delivered |
| `Row`, `Column`, `Flow`, `Grid` | positioners: `spacing`, `padding`; `Grid` takes `columns`, `rows`, `rowSpacing`, `columnSpacing`; `flow` accepted |
| `RowLayout`, `ColumnLayout`, `GridLayout`, `StackLayout` | QtQuick.Layouts with the `Layout.*` hints above; `StackLayout` shows `currentIndex` only |
| `Repeater` | `model` (number, array, ListModel, object model) and a delegate child; expands inline in its parent |
| `Loader` | `sourceComponent` (a Component id or any expression yielding one), `source` (relative file), inline child, `active`; `status`/`item` readable; `asynchronous` accepted |
| `Flickable`, `ScrollView` | scrolling viewport; `contentWidth/Height`, `contentX/Y` (readable and writable), `interactive`, `padding`, `spacing`; `atYEnd`, `atYBeginning`, `visibleArea`, `moving` readable; `boundsBehavior`, `flickableDirection` accepted |
| `ScrollBar`, `ScrollIndicator` | ScrollBar renders a bar bound to `position`/`size`; ScrollIndicator is a plain box |
| `Image`, `AnimatedImage`, `BorderImage` | `source`, `width`, `height`, `fillMode` (Stretch, PreserveAspectFit, PreserveAspectCrop, Tile, TileVertically, TileHorizontally, Pad; tiles are the source's pixel size, centred), `rotation`, `visible`, `sourceSize` readable; `.svg` and `icon:name[/color]` sources render as vectors (no tiling). `AnimatedImage` plays a GIF (over `ui.ImagePlayer`; `BorderImage` and the tile modes are `ui.LazyImageEl.NinePatch`/`Fill`) with `playing` (restarts from the first frame), `paused` (holds and resumes), `speed`, `currentFrame` (writing jumps) and `frameCount`, read at build (no `onFrameChanged`); `BorderImage` is a 9-patch: `border.left/top/right/bottom` (or `border { … }`) in source pixels, `horizontalTileMode`/`verticalTileMode` Stretch, Repeat (from the top-left edge, the last tile cut) or Round; `asynchronous`, `cache`, `mipmap`, `status`, `progress`, `onStatusChanged`, `horizontalAlignment` accepted |
| `Text`, `Label` | `text`, `color`, `font.pixelSize/pointSize/bold/italic/weight/family/underline/strikeout/letterSpacing`, `font` (object), `wrapMode`, `elide`, `maximumLineCount`, `horizontalAlignment`, `verticalAlignment`, `textFormat` (PlainText, RichText subset, MarkdownText, StyledText/AutoText as rich), `onLinkActivated`, `renderType`, `opacity`, transforms, `lineHeight` (accepted), `style`/`styleColor`, `font.capitalization`, `fontSizeMode`, `minimumPixelSize`, `textInteractionFlags`, `linkColor` accepted |
| `Canvas` | `onPaint` with `getContext("2d")`; `requestPaint()` works; `width`, `height`; `renderTarget`, `renderStrategy`, `contextType` accepted |
| `Shape` with `ShapePath` | `strokeColor`, `strokeWidth`, `fillColor`, `fillGradient` (Linear/Radial/Conical approximated by a 2-stop linear), `fillRule`, `capStyle`, `strokeStyle`/`dashPattern`/`dashOffset`; segments `PathMove`, `PathLine`, `PathQuad`, `PathCubic`, `PathArc`, `PathAngleArc`, `PathSvg`, `PathPolyline`. `joinStyle` and per-path transforms are not modelled |
| `View3D`, `Viewport3D` | see 3D |
| `Spacer` | extension: a fixed or growing gap |

### Controls

| element | honoured | accepted / not modelled |
| --- | --- | --- |
| `Button`, `ToolButton`, `DelayButton`, `RoundButton`, `TabButton`, `MenuItem`, `MenuBarItem` | `text`, `icon.source/name/color/width/height`, `width`, `height`, `color`, `radius`, `flat`, `highlighted`, `checkable`, `checked`, `down`, `onClicked`, `onToggled`, `palette.button/buttonText`, `background`/`contentItem` via a style module | `autoRepeat`, `display`, `action` beyond text, `DelayButton.delay` progress |
| `ItemDelegate`, `SwipeDelegate`, `CheckDelegate`, `RadioDelegate`, `SwitchDelegate` | a row with text, highlight, `onClicked` | swipe actions |
| `CheckBox`, `Switch`, `RadioButton` | `text`, `checked`, `onToggled`/`onClicked`, `enabled` | `tristate`/`checkState`, `autoExclusive`, `ButtonGroup` exclusivity |
| `Slider`, `RangeSlider`, `Dial` | `value`, `from`, `to`, `stepSize`, `snapMode`, `orientation`, `onMoved`, `onValueChanged`, `live`; RangeSlider `first.value`/`second.value` with non-crossing drag | Dial dragging (value follows its binding), `wheelEnabled`, `touchDragThreshold` |
| `SpinBox` | `value`, `from`, `to`, `stepSize`, `editable`, `textFromValue`/`valueFromText` via a style | `wrap`, `inputMode`, `validator` |
| `ComboBox` | `model`, `currentIndex`, `textRole`, `valueRole`, `currentText`, `currentValue`, `displayText`, `editable`, `onActivated`, `onCurrentIndexChanged`, `popup` via a style | `editText` round-trip, `find()` |
| `ProgressBar`, `BusyIndicator` | `value`, `from`, `to`, `indeterminate`, `running` | |
| `TextField`, `TextInput`, `TextArea`, `TextEdit` | `text`, `placeholderText`, `color`, `font`, `echoMode`, `maximumLength`, `validator` (`IntValidator`, `DoubleValidator`, `RegularExpressionValidator` — `acceptableInput` reflects it), `readOnly`, `onAccepted`, `onTextChanged`, `onEditingFinished`, `Keys.onReturnPressed`, `focus`, `activeFocus`, `background`, `borderColor`/`textColor`/`radius` (extensions), `selectByMouse`, `wrapMode` | `inputMask`, `cursorPosition`, `selectionStart/End`, selection APIs, `selectAll()`, `TextArea` rich text |
| `Pane`, `Frame`, `GroupBox`, `Page`, `Control`, `Container`, `ToolBar`, `ApplicationWindow`, `Window` | plain boxes that lay out their children; `padding`, `spacing`, `background` | `title`, `header`/`footer` on Page and ApplicationWindow, `menuBar`, window `flags`/`visibility`, `GroupBox.title` |
| `MenuBar`, `Menu`, `MenuItem`, `MenuSeparator`, `Action` | menu titles, open-on-click, `popup()`/`open()`/`close()`, `Action { text; checkable; checked; onTriggered }`, checkable items | submenus, `Action.shortcut` (a separate `Shortcut` works), `ActionGroup` |
| `TabBar`, `TabButton` | `currentIndex` driven by its buttons | `position`, `contentWidth` |
| `SplitView` | `orientation`, `handle:`, `SplitView.preferredWidth/Height`, `fillWidth/Height`, `minimum*`/`maximum*`; handles drag | `SplitHandle.pressed/hovered` read false |
| `SwipeView`, `StackView`, `StackLayout` | `currentIndex` (SwipeView slides), `push/pop/replace/clear`, `depth`, `initialItem` | `StackView` transitions, `SwipeView` drag paging |
| `PageIndicator` | `count`, `currentIndex`, `interactive`, `spacing`, `delegate` | |
| `ToolSeparator`, `MenuSeparator` | a line | |
| `Tumbler` | `model`, `currentIndex`, `visibleItemCount`, `delegate` with `Tumbler.displacement`; click, keys, drag, flick | `wrap` |
| `MonthGrid`, `DayOfWeekRow` | `month`, `year`, `delegate`; weeks start Sunday, English names | `locale` |
| `FileDialog`, `FolderDialog` | an in-app browser panel: `open()`/`close()`, `selectedFile(s)`, `currentFolder`, `fileMode`, `nameFilters`, `onAccepted`/`onRejected` | the desktop's native dialog; `MessageDialog`, `ColorDialog`, `FontDialog` are not elements |
| `Popup`, `Dialog`, `Drawer`, `ToolTip` | floated above the page when `visible`/`opened`; `open()`/`close()`, `modal`, `dim`, `x`, `y`, `width`, `height`, `title`, `standardButtons`, `onAccepted`/`onRejected`/`onOpened`/`onClosed`, `closePolicy`, Drawer `edge`/`position`, styled through a module | `enter`/`exit` transitions, `Overlay` attached beyond `Overlay.modal` in styles |

### Models and views

| element | notes |
| --- | --- |
| `ListModel`, `ListElement` | rows of named roles; runtime `append insert remove clear set get count` work from handlers and mark readers dirty; `setProperty`, `move`, `sync` are not modelled |
| `ListView` | `model`, `delegate`, `currentIndex`, `highlight`, `spacing`, `height`/`rowHeight`, `section.property`/`section.delegate`, `verticalLayoutDirection`, `onAtYEndChanged`/`onAtYBeginningChanged`; delegates read `ListView.isCurrentItem`, `ListView.view`; `orientation: ListView.Horizontal` (delegates side by side at their own width, scrolling sideways, every delegate built); `header`/`footer` (above and below the rows, not scrolling with them). `add`/`remove`/`move`/`displaced`/`populate` transitions, `highlightRangeMode`, `cacheBuffer`, `keyNavigationEnabled`, `snapMode` are not modelled |
| `GridView` | `model`, `delegate`, `cellWidth`, `cellHeight`, `currentIndex`, `highlight`; `flow`, `snapMode`, `cacheBuffer` accepted |
| `PathView` with `Path` | `model`, `delegate`, `path` (`PathLine`, `PathQuad`, `PathCubic`, `PathArc`, `PathAttribute`, `PathPercent`), `currentIndex`, `preferredHighlightBegin`, `dragMargin`; drag and flick turn the carousel; `PathView.isCurrentItem`, `PathView.onPath` and `PathAttribute` values in delegates |
| `TableView`, `TableModel`, `TableModelColumn`, `HorizontalHeaderView`, `ItemSelectionModel`, `DelegateChooser`, `DelegateChoice` | one delegate per cell with `row`, `column`, `display`, `selected`, `current`; `editDelegate` commits into a TableModel; `selectionModel` enables tap-to-select a row; `syncView` headers. Column widths come from the delegate; `columnWidthProvider` and `TableView.*` attached editing flags are accepted |
| `TreeView` | nested `{roles, children: [...]}` array (or an object exposing it as `tree`); delegates get `treeView`, `isTreeNode`, `expanded`, `hasChildren`, `depth`, `row`; `toggleExpanded/expand/collapse`, `currentRow` |
| `Timer` | see below |
| `ButtonGroup`, `ActionGroup`, `Settings`, `SystemPalette`, `Shortcut`, `Connections`, `Component`, `FontLoader`, `Binding`, `FrameAnimation` | non-visual declarations; `Shortcut` fires (see Keys); `Settings` persists (below); `SystemPalette` holds the light palette gml's controls are drawn in (`ui.DefaultPalette`) (every QPalette role, `colorGroup` Active/Inactive/Disabled, live), not the platform's theme |

A delegate sees `index`, `model` (the row object), `modelData` (for plain
arrays and numbers) and every role of the row by bare name; `required
property` declarations are filled from the same scope. Each row is its own
instance: ids and `property` declarations inside the delegate are per row
and persist across frames and reloads.

### Animations and states

| element | notes |
| --- | --- |
| `NumberAnimation`, `PropertyAnimation`, `RotationAnimation`, `ColorAnimation`, `SmoothedAnimation`, `SpringAnimation`, `NumberAnimator`, `RotationAnimator`, `OpacityAnimator`, `ScaleAnimator`, `XAnimator`, `YAnimator`, `UniformAnimator` | standalone: `target`/`targets`, `property`/`properties`, `from`, `to`, `duration` (default 250), `easing.type`, `loops` (`Animation.Infinite`), `running`, `alwaysRunToEnd`; `start() stop() restart() pause() resume() complete()` from handlers; `onFinished`. A `Type on <prop>` form drives the owner's property, including built-in `opacity`, `x`, `y`, `scale`, `rotation`, `width`, `height`. Spring and smoothed animations tween like NumberAnimation (`spring`, `damping`, `velocity` accepted) |
| `SequentialAnimation`, `ParallelAnimation`, `PauseAnimation`, `PropertyAction`, `ScriptAction` | composition with the same controls; children inherit the container's `on` target |
| `Behavior on <prop>` | tweens the owner's property on every change, with the contained animation's `duration`/`easing` (or its own); `enabled` accepted |
| `State`, `PropertyChanges`, `Transition` | `state:` selects a named state; `PropertyChanges { target: id; prop: value }` overrides attributes on the target (including `anchors.*`, `font.*` groups); a `Transition`'s animations tween the listed `properties` with `duration`/`easing.type`. `State.when`, `from`/`to` on Transition, `reversible`, `AnchorChanges`, `ParentChange`, `StateChangeScript`, `extend` are not modelled |
| `Easing.*` | `Linear`, `In/Out/InOut` × `Quad Cubic Quart Sine Expo Back Bounce Elastic`; `easing.type` takes the enum or the name as a string; `amplitude`, `period`, `overshoot`, `bezierCurve` accepted |
| `FrameAnimation` | `onTriggered` every frame with `frameTime`/`elapsedTime`; `running` |
| `Binding` | `target`, `property`, `value`, `when`; writes the property while `when` holds (`restoreMode` ignored) |

### Timers and keys

`Timer { interval; running; repeat; triggeredOnStart; onTriggered }` with
QML defaults (1000 ms, not running, repeating); `start() stop() restart()`
from handlers. Time comes from the frame loop, so a timer only advances
while frames are produced (headless screenshots warm one frame).

`Keys.onPressed`/`Keys.onReleased` on a focused item run with `event`
bound: `event.key` (a `Qt.Key_*` code: the editing and arrow keys, Insert,
PageUp/PageDown, F1–F12, Space, punctuation, and letters/digits as their
uppercase code), `event.text`, `event.modifiers` (`Qt.ShiftModifier`,
`ControlModifier`, `AltModifier` bits). As in Qt, a `Keys.onPressed`
handler that acts on a key sets `event.accepted = true`; a key it leaves
unaccepted falls through to the default handling (Enter/Space activate).
The key-specific handlers (`Keys.onLeftPressed`, `onRightPressed`,
`onUpPressed`, `onDownPressed`, `onEscapePressed`, `onSpacePressed`,
`onTabPressed`, `onBacktabPressed`, `onDeletePressed`, `onReturnPressed`,
`onEnterPressed`, `onDigit0Pressed`…`onDigit9Pressed`) start accepted, and
`onPressed` runs after one only if it un-accepts the event. `KeyNavigation.*` is not
modelled.

`Shortcut { sequence; sequences; enabled; onActivated }` (over
`ui.DivEl.Shortcut`, `ui.ParseKeySequence` and `ui.StandardKey`, the Go
API for the same) fires wherever focus is, before the focused item's `Keys` handlers. A sequence is a string
(`"Ctrl+Shift+S"`, `"F5"`, `"Esc"`, `"Ctrl++"`), a `StandardKey`
(`StandardKey.Save`, with the Windows/Linux bindings) or a key code with
modifier bits (`Qt.CTRL | Qt.Key_S`); `Connections { onActivated }` on its
id fires too. A focused text field keeps the plain keys it types. Not
modelled: multi-chord sequences (`"Ctrl+K, Ctrl+C"`), `Meta`, `context`,
`autoRepeat`, `onActivatedAmbiguously`, `nativeText`/`portableText`.

### Settings
`Settings { category; location; property … }` (QtCore, or
Qt.labs.settings' `fileName`) keeps its declared properties across runs:
on the first frame a saved value replaces the default (breaking its
binding, as an assignment would), and a changed value is written at the
start of the next frame. Keys are `category/name`. `property alias winW:
window.width` works; the alias binds after the first build, so the saved
value shows from the second frame. `value(key, default)`,
`setValue(key, value)` and `sync()` are its methods. Values are kept as
JSON: natively one file per settings file in `<user config dir>/gfx/`
(`<program name>.json` by default; an absolute `location` is used as
given), on the web in `localStorage` under `gfx.settings:<file>`. A host
picks another place with `Document.SetSettingsStore` (before the first
frame) or `gml.SetDefaultSettingsStore`; `MemorySettingsStore` forgets
everything at exit, and `Document.SaveSettings` writes pending changes at
once. Declared properties share one document-wide name space, so a
Settings property should not reuse a name another element declares.

### Canvas 2D context

`getContext("2d")` returns a context with: `fillStyle`, `strokeStyle`
(colours and `createLinearGradient`/`createRadialGradient` approximated by a
2-stop gradient), `lineWidth`, `lineCap`, `lineJoin`, `miterLimit`,
`globalAlpha`, `globalCompositeOperation` (accepted), `shadowBlur`,
`shadowColor`, `shadowOffsetX/Y`, `font`, `textAlign`, `textBaseline`;
`beginPath moveTo lineTo quadraticCurveTo bezierCurveTo arc ellipse rect
roundedRect closePath fill stroke clip`, `fillRect strokeRect clearRect`,
`fillText measureText`, `save restore`, `translate rotate scale transform
setTransform resetTransform reset`, `setLineDash getLineDash
lineDashOffset`. `strokeText`, `drawImage`, `createPattern`, `getImageData`,
`putImageData`, `arcTo` and `isPointInPath` are not implemented;
`canvasSize`, `canvasWindow`, `tileSize`, `onImageLoaded`, `onPainted` are
accepted.

### Rich text and Markdown

`textFormat: Text.RichText` renders `<b> <strong> <i> <em> <u> <s> <strike>
<del> <font color> <br> <p> <h1>-<h4> <div> <li>` and the common entities;
unknown tags are ignored and their content kept. `textFormat:
Text.MarkdownText` renders through the framework's Markdown renderer (400 px
wide when the item states no width), with images relative to the document
and clickable links when `onLinkActivated` is declared (`link` bound).

### Effects

| element | notes |
| --- | --- |
| `ShaderEffect` | with `vertexShader`/`fragmentShader` as `.qsb` files, runs the SPIR-V inside on the GPU, uniforms bound by property name (`qt_Matrix`, `qt_Opacity` first); a shader that cannot load renders the source unshaded |
| `ShaderEffectSource` | `sourceItem` captured by building it again in place; `hideSource`, `live`, `recursive`, `textureSize`, `wrapMode` accepted |
| `MultiEffect`, `FastBlur`, `GaussianBlur`, `RecursiveBlur`, `DropShadow`, `Glow` | blur and shadow honoured (`blur`/`blurMax`, `radius`, `shadowColor`, offsets); `colorization` + `colorizationColor` tints icons |
| `MaskedBlur`, `RadialBlur`, `DirectionalBlur`, `ZoomBlur`, `InnerShadow`, `ColorOverlay`, `Colorize`, `Desaturate`, `Blend`, `BrightnessContrast`, `HueSaturation`, `LevelAdjust`, `Displace`, `OpacityMask`, `ThresholdMask`, `GammaAdjust` | accepted; the source renders unprocessed |

### Particles

`ParticleSystem` with `Emitter` (`emitRate`, `lifeSpan`, `lifeSpanVariation`,
`size`, `endSize`, `sizeVariation`, `velocity`, `acceleration`, `group`,
`enabled`, `x/y/width/height`), `ImageParticle` (`source`, `color`,
`colorVariation`, `alpha`, `entryEffect`, `groups`), directions
`AngleDirection`, `PointDirection`, `TargetDirection`,
`CumulativeDirection`, affectors `Gravity`, `Wander`, `Turbulence`,
`Attractor`, `Friction`, `Age`. Parts may be declared anywhere and bound by
`system:`. `TrailEmitter`, `ItemParticle`, `CustomParticle` build as their
plain counterparts; sprite animation, `Affector.onAffected` and group
transitions are not modelled.

### 3D

Two separate systems share the markup syntax.

`View3D` is QtQuick3D: `environment: SceneEnvironment { clearColor }`,
`PerspectiveCamera`/`OrthographicCamera` (`position`, `eulerRotation`,
`fieldOfView`, `clipNear/Far`, magnifications), `DirectionalLight`,
`PointLight`, `SpotLight` (`color`, `brightness`, `ambientColor`;
`castsShadow` accepted), `Node` (`position`, `eulerRotation`, `scale`,
`visible`) and `Model` (`source` `#Cube #Sphere #Cylinder #Cone #Plane`;
`materials`/`material` with `DefaultMaterial`/`PrincipledMaterial`
`diffuseColor`/`baseColor`/`albedoColor` and `lighting:
DefaultMaterial.NoLighting`). `NumberAnimation on eulerRotation.y` works.
Mesh files, textures, shadows, IBL, instancing, skeletal animation,
`Texture`, `SpecularGlossyMaterial` parameters and the rest of QtQuick3D are
accepted and not modelled. The host must call `gml.SetView3DGPU` once and
`gml.EncodeView3D` per frame (see `examples/qml/main.go`); without them a
View3D is an empty box of the right size.

`Scene3D` is this framework's own scene document, loaded with
`gml.LoadScene`/`LoadSceneFile` rather than `Load`: `Box`, `Sphere`,
`Cylinder`, `Node3D`, `Trigger`, `MeshInstance` (glTF, `animation`,
`collider: "box"|"mesh"`) with `position`, `rotation`, `rotationY`, `size`,
`radius`, `height`, `color`, `metallic`, `roughness`, `texture`/`uvScale`,
`collider`, `layer`, `lightDir`. Every solid gets a physics body; bindings
re-pose bodies when they move.

`Viewport3D { source: view }` embeds a `*render.View3D` the host put in the
Env with `env.Set`; `onTapped` runs with `tapX`/`tapY`.

### Charts, maps, media, PDF

| element | notes |
| --- | --- |
| `GraphsView` with `LineSeries`, `ScatterSeries`, `SplineSeries`, `AreaSeries`, `BarSeries`/`BarSet` over `ValueAxis`/`BarCategoryAxis` | the plot, grid and axes are drawn; series `append replace removeMultiple clear` work. Axis labels/titles, legends, themes beyond transparency, zoom/pan are not modelled |
| `Bars3D`, `Scatter3D`, `Surface3D` with `Bar3DSeries`/`Scatter3DSeries`/`Surface3DSeries` and `ItemModel*DataProxy` | drawn as an isometric projection on the 2D renderer; `cameraPreset` picks handedness; shadows, themes, selection, rotation not modelled |
| `Map`, `MapPolyline`, `MapPolygon`, `MapCircle`, `MapRectangle`, `MapQuickItem`, `MapItemView`, `Plugin`, `PluginParameter`, `QtPositioning.coordinate` | a real Web-Mercator projection around `center` at `zoomLevel` (both writable), `toCoordinate`/`fromCoordinate`; no basemap tiles are fetched |
| `MediaPlayer`, `AudioOutput`, `AudioInput`, `VideoOutput`, `CaptureSession`, `Camera`, `ImageCapture`, `MediaRecorder`, `MediaDevices`, `SoundEffect` | the state machine without a decoder: a source goes to `InvalidMedia`, `playbackState` stays stopped, `position`/`duration` are 0, VideoOutput paints black, MediaDevices lists nothing |
| `PdfDocument`, `PdfMultiPageView`, `PdfPageView`, `PdfScrollablePageView` | pages are rendered by `render/pdf`; `status`, `pageCount`, metadata and `renderScale` are real |

### Validators and misc

`IntValidator { bottom; top }`, `DoubleValidator { bottom; top; decimals }`,
`RegularExpressionValidator`/`RegExpValidator { regularExpression }` (the
pattern is kept, not matched — `acceptableInput` reflects only the numeric
validators). `Gradient`/`GradientStop`, `Translate`/`Rotation`/`Scale`,
`Path*` segments, `ListElement`, `TableModelColumn`, `State`,
`PropertyChanges`, `Transition`, `DelegateChoice` are only meaningful as
children or attribute values of their owners.

## Enums and attached properties

`enums.go` registers Qt's scoped enumerations as namespace objects under
the type names, with Qt's numbering where code compares raw values:

| namespace | members |
| --- | --- |
| `Qt` | `AlignLeft AlignRight AlignHCenter AlignJustify AlignTop AlignBottom AlignVCenter AlignCenter`, `Horizontal Vertical`, `TopEdge LeftEdge RightEdge BottomEdge`, `AscendingOrder DescendingOrder`, `Unchecked PartiallyChecked Checked`, `ArrowCursor IBeamCursor CrossCursor PointingHandCursor SplitHCursor SplitVCursor`, `Key_*`, named colours, and the functions listed under the JS subset |
| `Text`, `TextInput`, `TextEdit` | `NoWrap WordWrap WrapAnywhere Wrap`, `PlainText RichText AutoText StyledText MarkdownText`, `QtRendering NativeRendering`, `ElideNone ElideLeft ElideMiddle ElideRight`, the `Align*` flags; TextInput/TextEdit add `Normal Password NoEcho PasswordEchoOnEdit SelectCharacters SelectWords SelectAll` |
| `Image`, `BorderImage`, `AnimatedImage` | `Stretch PreserveAspectFit PreserveAspectCrop Tile TileVertically TileHorizontally Pad`, `Null Ready Loading Error`; BorderImage adds `Repeat Round` |
| `Font` | `Thin ExtraLight Light Normal Medium DemiBold Bold ExtraBold Black` |
| `ListView` | `Horizontal Vertical TopToBottom BottomToTop LeftToRight RightToLeft` |
| `TableView`, `ItemSelectionModel`, `Flickable` | selection behaviours/modes, edit triggers, `NoUpdate Clear Select Deselect Toggle Current ClearAndSelect SelectCurrent Rows Columns`, `StopAtBounds DragOverBounds OvershootBounds DragAndOvershootBounds` (evaluate; mostly not consumed) |
| `Animation`, `Easing` | `Animation.Infinite`; the easing names |
| `PathView` | `NoHighlightRange ApplyRange StrictlyEnforceRange`, fallback `isCurrentItem`/`onPath` outside delegates |
| `Gradient`, `ShapePath`, `Shape` | `Vertical Horizontal`; `OddEvenFill WindingFill`, pen styles, caps, joins; `GeometryRenderer CurveRenderer` |
| `Item` | `TopLeft Top TopRight Left Center Right BottomLeft Bottom BottomRight` (transformOrigin) |
| `Loader` | `Null Ready Loading Error` |
| `Slider`, `RangeSlider`, `Dial` | `NoSnap SnapAlways SnapOnRelease` |
| `Popup`, `Dialog`, `DialogButtonBox` | `NoAutoClose CloseOnPressOutside ... CloseOnEscape`; `Ok Save SaveAll Open Yes YesToAll No NoToAll Abort Retry Ignore Close Cancel Discard Help Apply Reset RestoreDefaults NoButton` |
| `DefaultMaterial`, `PrincipledMaterial`, `SpecularGlossyMaterial`, `Material` | lighting, blend, specular, cull and alpha modes (by name) |
| `Graphs3D`, `GraphsTheme`, `Surface3DSeries`, `Bar3DSeries`, `Scatter3DSeries`, `Abstract3DSeries` | camera presets, shadow quality, selection flags, themes, mesh kinds |
| `MediaPlayer`, `PdfDocument`, `QtPositioning` | media status/playback state, PDF status/errors, `coordinate(lat, lon)` |

Enum members not listed evaluate to an error on strict `.gml` ("no member")
and skip the element on `.qml`. Enums without a numeric role in the engine
evaluate to their own name, so code that only compares or prints them works.

Attached properties honoured: `Layout.*` (in layouts), `anchors.*`,
`Keys.on*`, `Component.onCompleted`, `ToolTip.*`, `ListView.isCurrentItem`
/ `ListView.view`, `GridView.isCurrentItem`, `PathView.isCurrentItem` /
`onPath`, `Tumbler.displacement`, `SplitView.*` sizing, `TableView.*`
editing flags (accepted), `Overlay.modal` in style files. `Drag.*`,
`KeyNavigation.*`, `Accessible.*`, `LayoutMirroring.*`,
`EnterKey.*` and `ScrollBar.vertical`/`horizontal` on a Flickable are not
modelled (the last is accepted).

## Image and font paths

`Image.source`, `ImageParticle.source`, `icon.source` and `FontLoader.source`
resolve the same way:

1. `http://` and `https://` sources are not fetched (the image is empty).
2. `file://`, `qrc:/` and `qrc` prefixes are stripped.
3. An absolute path that exists is used as-is. An absolute path that does
   not exist (a `qrc` tree path like `/assets/icons/x.svg`) is searched as
   `<A>/assets/icons/x.svg` and `<A>/imports/assets/icons/x.svg` for the
   document directory `A` and each ancestor.
4. A relative path is joined to the document directory (`Load`'s file
   directory; `LoadString` has none, so a relative source stays relative to
   the process working directory). Component files, Loader sources and
   singletons resolve against the main document's directory, not their own.
5. `icon:<name>` and `icon:<name>/<color>` come from the `ui/icons`
   registry (works on wasm too).

Raster sources (`png`, `jpeg`, `gif`) are read once per document and
decoded/uploaded lazily on first paint; `sourceSize` and the implicit size
come from the header. `.svg` sources are parsed once and drawn as paths;
`sourceSize` for a vector is the declared width/height. A missing file is
logged once and renders an empty box of the stated size (80 x 80 when no
size is known).

`FontLoader` reads the file, takes the family name from the font's name
table, registers it process-wide and exposes `name`, `source`, `status`
(`1` Ready, `3` Error); `font.family: loader.name` then selects it. Fonts
load once per resolved path. A family that was never loaded falls back to
the default face. The default UI font is the shell's (`GFX_FONT`), and the
engine uses Qt's line metrics for Text items (OS/2 ascent + descent) so
markup laid out against Qt lands at the same heights.

## Performance

Bindings have no dependency graph of their own: the document is a view
function, and each frame the ui layer asks for the element tree. What keeps
that cheap:

- **Whole-document memoization.** `Document.View()` first ticks timers,
  drains queued Go-side emissions and re-evaluates declared property
  bindings whose read-sets are stale. It then compares an observable
  signature — `core.Revision()` (bumped by every property, model and
  `Env.Set` write) plus the scroll-state tokens — with the one the cached
  tree was built against, and when that matches, a geometry signature
  folded from every registered element's rect. Both unchanged: the previous
  tree is returned untouched and no interpreter work happens. The ui layer
  still lays the cached elements out every frame, so hover, focus and
  resize resolve at paint time. A frame that rebuilt leaves the geometry
  signature unknown, so the first quiet frame after a change rebuilds once
  more and records it — one extra build per change, not one per frame.
- **Subtree cache with read-sets.** Within a rebuild, each element's build
  is tracked (`core.TrackReads`): a subtree whose read-set is not stale is
  served from cache; one that is stale is patched in place as a slot, so a
  change deep inside a component instance or a delegate row redoes that
  slot and not the use site. Delegate rows bind their data through a hash
  token so rows are cacheable; a row holding a value the hash cannot follow
  stays uncacheable. Builtins, JS locals, an item's own attributes read back
  by siblings and decoded image sizes are "derived" reads that do not
  poison a read-set.
- **Keys are integers** interned per document; element state (ids, refs,
  tokens, overrides, cache entries) is filed under them.
- **Known costs.** A styled control or a component use site re-declares its
  scope every build even when its subtree will hit the cache; a flat scope
  map is re-bound per patched slot (the measured remainder on an animated
  page is patch machinery, not evaluation — evaluation is ~3 % of a frame).
  `width`/`height` read where an item is not registered stay untracked on
  purpose.
- **Animations and timers** are driven by the frame clock, so a page that
  animates rebuilds the animated leaf every frame and nothing else.

Debug switches (environment variables): `GFX_BUILD_STATS=1` prints per-build
counts of fresh/cached/patched subtrees and the cache-miss causes;
`GFX_BUILD_TRACE=1` names every subtree built or served; `GFX_NO_SUBTREE_CACHE=1`
rebuilds everything (ground truth for a suspected stale cache);
`GFX_CACHE_SKIP=<substring>` forces subtrees whose key contains it to
rebuild; `GFX_NO_PATCH=1` disables in-place patching; `GFX_PATCH_VERIFY=1|all`
rebuilds alongside the patch and reports differences; `GFX_NO_ROW_CACHE=1`
takes delegate rows out of the cache.

## Differences from Qt QML and known gaps

Semantics that differ by design:

- Loosely typed: all numbers are doubles, `===` is `==`, declared types
  other than the seven listed are `var`. Strings are not colours; colours
  are `#aarrggbb` strings tagged at the boundary.
- Bindings re-evaluate when their read-set is stale rather than through
  per-property change notification; `on<Prop>Changed` exists for declared
  properties and control state, not for every built-in property (geometry
  change handlers are accepted no-ops).
- Geometry is one frame behind: `x`, `y`, `width`, `height` read the last
  laid-out rect, zero on the first frame. `Component.onCompleted` runs after
  the first laid-out frame for the same reason.
- Built-in geometry is layout-owned: `width = 100` in a handler does not
  move an item; declare a property and bind to it. `x`/`y` on in-flow
  children of positioners and layouts are ignored; inside a plain Item they
  position.
- A plain `Rectangle`/`Item` is a flex column that sizes to its children
  outside a layout (QML's has no implicit size); the extension attributes
  (`spacing`, `padding`, `justify`, `items`) exist because of this.
- Component files are parsed once per document and do not hot-reload
  independently; their relative assets resolve against the main document.
- `Connections` takes the Qt 6 `function onSignal()` form only, and the
  handler is called without the signal's arguments.
- Lenient `.qml` loading skips failing elements; strict `.gml` fails the
  page.

Not implemented (summarised from `script/TODO.md` and the findings log):

- Interaction: pointer-handler gestures (pinch, wheel); keyboard
  navigation (`KeyNavigation`, tab focus order), `event.modifiers`.
- Views: horizontal ListView, `header`/`footer`, view transitions, Flickable
  `boundsBehavior`, PathView `highlightRangeMode`, `DelegateModel`,
  `ObjectModel`, `Instantiator`, `XmlListModel`, `WorkerScript`.
- Controls: native file/colour/font/message dialogs, popup enter/exit
  transitions, Dial dragging, SpinBox wrap/validator, ComboBox editable
  round-trip, TextField selection and `inputMask`, ButtonGroup exclusivity,
  `Settings` persistence, `SystemPalette` colours, Page/ApplicationWindow
  `header`/`footer`/`menuBar`, locale-aware calendars.
- States: `State.when`, Transition `from`/`to`/`reversible`,
  `AnchorChanges`, `ParentChange`, `StateChangeScript`.
- Graphics: animated PNG/WebP (`AnimatedImage` plays GIFs only), remote
  images, `layer.effect` beyond blur/shadow/colorize, the Qt5Compat
  effects listed as accepted, Canvas `drawImage`/patterns/pixel access,
  Shape joins/dash on Canvas, `TextMetrics`/`FontMetrics`, Text
  `lineHeight`, `style`, `font.capitalization`.
- Modules without a backend: QtMultimedia decoding and capture, QtLocation
  tiles, QtQuick3D mesh files/textures/shadows, QtGraphs legends and axis
  labels, Qt.labs.settings.
- JavaScript: see the "Not supported" list above; `qsTr`/translation,
  `Qt.binding`, dynamic object creation, `.js` imports.
- Controls styling: Tumbler keeps the built-in wheel even when a style
  module provides one; hover/press tinting in style files reads the live
  state but `SplitHandle.pressed/hovered` and `Overlay` beyond `modal` do
  not.

Where the engine is honest rather than complete, the page still loads: an
accepted attribute evaluates (so a binding error is still reported) and
changes nothing.
