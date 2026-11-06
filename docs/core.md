# core — properties, signals, the event loop, animation, math

`github.com/sushvibez/gfx/core` is the foundation every other package
builds on. It has no dependencies inside the framework and four parts:

| Part | Files | What it gives you |
|------|-------|-------------------|
| Reactive state | `property.go`, `signal.go` | `Property[T]` with automatic dependency tracking, `Signal[T]`, `Revision()`, read-set tracking |
| Event loop | `loop.go` | `Loop`: the single goroutine that owns all reactive state, `Post` from other goroutines |
| Animation | `anim.go` | tweens with 31 easing curves, loops, springs, the animation clock |
| Math | `vec.go`, `mat.go`, `quat.go`, `geom.go` | `Vec2/3/4`, `Mat4`, `Quat`, ray and closest-point queries |

Everything here is plain structs and package-level functions; there is no
reflection, no code generation and no locking.

## Property[T]

`Property[T comparable]` is a reactive value, the Go equivalent of a QML
property. The zero value is ready to use, so properties are embedded in
structs without a constructor.

```go
var width, height core.Property[float32]
height.Set(100)
width.Bind(func() float32 { return height.Get() * 2 })
width.Get() // 200

height.Set(150)
width.Get() // 300 — re-derived because the binding read height
```

| Method | Behaviour |
|--------|-----------|
| `Get() T` | Returns the value. If the property has a dirty binding, it is re-evaluated first (pull). Inside a binding closure, `Get` also records this property as a dependency of that binding. |
| `Set(v T)` | Imperative write. Removes any binding on the property (QML semantics), then stores `v`. A write of an equal value is a no-op: no dirtying, no signal, no revision bump. |
| `Bind(fn func() T)` | Makes the property derive its value from `fn`. The closure runs once immediately and again whenever any property it read changes. Rebinding drops the old binding. |
| `Changed() *Signal[T]` | The signal emitted with the new value on every real change, whether by `Set` or by a binding re-evaluating. Allocated lazily on first call. |

### Automatic dependency tracking

While a binding closure runs, every `Property.Get` it touches registers the
calling binding as a dependent. Dependencies are cleared and re-captured on
every evaluation, so a conditional binding tracks exactly what it last read:

```go
var flag core.Property[bool]
var a, b, out core.Property[int]
a.Set(1)
b.Set(2)
out.Bind(func() int {
    if flag.Get() {
        return a.Get()
    }
    return b.Get()
})
out.Get()    // 2
a.Set(99)    // a is not a dependency right now: out is not re-evaluated
flag.Set(true)
core.FlushBindings()
out.Get()    // 99 — and b has been dropped as a dependency
```

### Lazy, batched evaluation

`Set` does not recompute anything. It marks every binding that depends on
the property dirty, cascades that mark through their dependents, and queues
them. Dirty bindings are evaluated in one of two ways:

- **Pull**: a `Get` on a property with a dirty binding evaluates it right
  there, so a read between frames always sees a consistent value.
- **Push**: `core.FlushBindings()` evaluates every queued binding. The event
  loop calls it once per frame (see `Loop.Step`) before layout and paint;
  tests call it directly.

Evaluation order is dependency order: a binding whose sources are themselves
dirty pulls them first through `Get`. A diamond (`sink` reads `left` and
`right`, both of which read `src`) therefore evaluates `sink` exactly once per
change of `src`, with no intermediate "glitch" value; `TestDiamondEvaluatesOnce`
in `property_test.go` asserts this.

```go
var a, b, c core.Property[int]
a.Set(1)
b.Bind(func() int { return a.Get() + 1 })
c.Bind(func() int { return b.Get() + 1 })
a.Set(10)
core.FlushBindings()
c.Get() // 12
```

### Binding loops

A binding that reads its own property (`a.Bind(func() int { return a.Get() + 1 })`)
is detected on evaluation; the message `core: binding loop detected; keeping
previous value` goes to stderr and the value is left alone, as QML does.
Mutually dependent bindings that would oscillate forever inside one flush
are cut off after 64 evaluations of the same binding (`maxFlushEvals`) with
a similar message; the next external `Set` retries. Unrelated bindings keep
working after a loop has been broken.

### Revision counter

`core.Revision()` returns a process-wide `uint64` that is bumped on every
real property change. Two equal readings mean no property changed in
between, which is how a frame loop skips rebuilding an identical view (the
`ui/shell` loop does exactly this, together with `AnimationsRunning`).
`core.BumpRevision()` bumps it by hand for mutations that live outside
`Property` values but must still invalidate revision-keyed caches.

### Read-set tracking

`core.TrackReads(fn)` runs `fn` and returns a `*ReadSet` recording every
property it read (outside binding evaluation). Later, `rs.Stale()` reports in
O(reads) whether any of them changed since, without re-running `fn`. This is
the basis for per-subtree caching in the markup engine; most applications do
not need it directly. The surface:

| Function / method | Purpose |
|-------------------|---------|
| `TrackReads(fn func()) *ReadSet` | Record the reads of `fn`. Scopes nest: an inner set becomes a nested child of the outer one. |
| `(*ReadSet).Stale() bool` | True if any recorded property (or any nested set's) changed, memoised per revision. |
| `(*ReadSet).OwnStale() bool` | Like `Stale` but ignoring nested sets. |
| `(*ReadSet).Untracked() bool` | True if the computation read state the property system cannot stamp. |
| `ReplayReads(rs *ReadSet)` | Adopt `rs` into the currently active read-set (what a cache hit owes its parent). |
| `MarkUntrackedRead()` | Poison the active read-set: its result can never be proven fresh. `GFX_UNTRACKED_TRACE=1` counts the call sites; `UntrackedSites()` returns and clears them. |
| `(*ReadSet).ReplaceSub`, `Invalidate`, `EachSub`, `Summary` | Patch or inspect nested sets (tooling). |

## Signal[T]

`Signal[T any]` is a typed multicast delegate: the signal half of Qt's
signal/slot mechanism. Any `func(T)` is a slot. The zero value is ready to
use.

```go
var s core.Signal[int]
var got []int
s.Connect(func(v int) { got = append(got, v) })
c := s.Connect(func(v int) { got = append(got, v*2) })
s.Emit(3)      // got == [3, 6]
c.Disconnect()
c.Disconnect() // idempotent
s.Len()        // 1
```

| Method | Behaviour |
|--------|-----------|
| `Connect(fn func(T)) Connection` | Registers `fn`; slots run in connection order. |
| `Connection.Disconnect()` | Removes the slot. Safe to call twice and safe during an emit (the slot does not run again in that emit). |
| `Emit(v T)` | Calls every slot connected before the emit started. A slot connected during an emit first runs on the next one. |
| `Len() int` | Number of live slots. |

Properties expose their change signal through `Changed()`:

```go
var p core.Property[int]
p.Changed().Connect(func(v int) { fmt.Println("now", v) })
p.Set(1)
p.Set(1) // no change, no emit
p.Set(2)
```

## Threading: one owner, `Loop.Post` for everyone else

All properties, signals and UI objects belong to the goroutine that runs the
event loop, which is Qt's main-thread rule. `Get`, `Set`, `Connect` and
`Emit` take no locks and must not be called from any other goroutine. The
one cross-goroutine entry point is `Loop.Post(fn)`, the equivalent of a Qt
queued connection: `fn` runs on the loop goroutine at the start of the next
`Step`.

```go
l := core.NewLoop()
go func() {
    result := compute()
    l.Post(func() { status.Set(result) }) // the only safe way in
}()
```

`DESIGN.md` §3 sketches an `EmitAsync` helper on `Signal`; it does not exist.
Use `Post` and call `Emit` inside the posted closure.

## The event loop

`core.Loop` does no pacing or vsync of its own. The `app`/`ui/shell` layer
calls `Step` from the platform's frame callback, and headless tests call it
directly for deterministic single-frame control.

| Method | Behaviour |
|--------|-----------|
| `NewLoop() *Loop` | A loop with a 256-entry posted-work queue. |
| `Post(fn func())` | Schedule `fn` on the loop goroutine before the next frame. Blocks if the queue is full; returns without running `fn` after `Quit`. |
| `Frame() *Signal[time.Duration]` | Emitted once per `Step` with the time since the previous frame; connect per-frame "process" callbacks here. |
| `Step(dt time.Duration)` | One frame: drain posted work, emit `Frame`, then `FlushBindings()`. |
| `Run(hz int)` | Fallback driver for servers and tests: ticks `Step` at `hz` (default 60) until `Quit`. |
| `Quit()` / `Quitting() bool` | Stop `Run` after the current frame and unblock pending `Post`s. |

```go
l := core.NewLoop()
var a, b core.Property[int]
b.Bind(func() int { return a.Get() })
l.Post(func() { a.Set(7) })
l.Frame().Connect(func(dt time.Duration) { /* per-frame work */ })
l.Step(16 * time.Millisecond) // runs the posted Set, emits Frame, flushes b
```

`Step` does not advance animations. The frame driver is expected to call
`core.TickAnimations(dt)` before `Step` (or before `FlushBindings`), which is
what `ui/shell` does every frame.

## Animation

`anim.go` is the engine under `Behavior`-style animations: a **tween** drives
a property toward a target over a duration through an easing curve; a
**spring** drives it with a damped physical model. Both write through
`Property.Set` each tick, so bindings and `Changed` fire as for any other
write. The frame loop advances them with `core.TickAnimations(dt)`.

Animatable property types are `~int | ~float32 | ~float64` (the `Animatable`
constraint). Interpolation is done in `float32` and converted back, so an
`int` tween steps through truncated intermediate values.

### Tweens

```go
var bar core.Property[float32]
core.Tween(&bar, 1, 400*time.Millisecond, core.EaseOutCubic)
```

| Function | Behaviour |
|----------|-----------|
| `Tween(p, target, d, easing)` | Animate `p` from its current value to `target` over `d`. A `nil` easing means `EaseInOutQuad`. A zero or negative `d` sets the value immediately. Starting a new tween on a property that is already tweening retargets from the current value, so rapid updates glide instead of jumping. |
| `TweenLoop(p, target, d, easing, mode)` | As `Tween` but repeating: `core.LoopForever` restarts each cycle, `core.PingPong` alternates direction. `core.Once` is the plain mode. `d` is clamped to at least 1 ms. |
| `StopTween(p)` | Cancel any tween or spring on `p`, leaving its current value. |
| `TweenRunning(p) bool` | Whether a tween or spring is currently driving `p`; for a control that follows an animated value and takes it back once it settles. |

From `anim_test.go`:

```go
var p core.Property[float32]
core.Tween(&p, 100, 100*time.Millisecond, core.EaseLinear)
core.TickAnimations(50 * time.Millisecond)               // p == 50
core.Tween(&p, 0, 100*time.Millisecond, core.EaseLinear) // retarget from 50
core.TickAnimations(50 * time.Millisecond)               // p == 25
```

### Easing curves

`type Easing func(t float32) float32` maps linear progress in `[0,1]` to
eased progress. All of QML's `QEasingCurve` families are present, each a
real curve (`TestEasingCurvesAreDistinct` checks they start at 0, end at 1,
and differ):

| Family | Functions |
|--------|-----------|
| Linear | `EaseLinear` |
| Quad | `EaseInQuad`, `EaseOutQuad`, `EaseInOutQuad` |
| Cubic | `EaseInCubic`, `EaseOutCubic`, `EaseInOutCubic` |
| Quart | `EaseInQuart`, `EaseOutQuart`, `EaseInOutQuart` |
| Quint | `EaseInQuint`, `EaseOutQuint`, `EaseInOutQuint` |
| Sine | `EaseInSine`, `EaseOutSine`, `EaseInOutSine` |
| Expo | `EaseInExpo`, `EaseOutExpo`, `EaseInOutExpo` |
| Circ | `EaseInCirc`, `EaseOutCirc`, `EaseInOutCirc` |
| Back (overshoot 1.70158) | `EaseInBack`, `EaseOutBack`, `EaseInOutBack` |
| Elastic (amplitude 1, period 0.3) | `EaseInElastic`, `EaseOutElastic`, `EaseInOutElastic` |
| Bounce | `EaseInBounce`, `EaseOutBounce`, `EaseInOutBounce` |

Any `func(float32) float32` works as a custom curve.

### Springs

A spring keeps velocity, so retargeting mid-flight glides instead of
snapping (the SwiftUI/GPUI feel).

| Function | Behaviour |
|----------|-----------|
| `SpringTo(p, target)` | Spring `p` toward `target` with stiffness 170 and damping 26 (a snappy, slightly damped response). Calling it again retargets the live spring in place, preserving velocity. |
| `SpringConfig(p, target, stiffness, damping)` | `SpringTo` with explicit constants. |

The integrator is semi-implicit Euler with the step clamped to 1/30 s so a
slow frame cannot blow it up; the spring is removed once position and
velocity are both within 0.0005 of rest, and the property is set to the
exact target. The toggle in `examples/animation` is the canonical use:

```go
var on bool
var knob core.Property[float32] // 0..1 position of the toggle knob

func flip() {
    on = !on
    to := float32(0)
    if on {
        to = 1
    }
    core.SpringTo(&knob, to) // springs toward the new end, keeping velocity
}
```

### Ticking and the animation clock

| Function | Behaviour |
|----------|-----------|
| `TickAnimations(dt) bool` | Advance every tween and spring by `dt`; reports whether any is still running (so a damage-driven loop knows to keep rendering). A finished tween settles exactly on its target. |
| `AnimationsRunning() bool` | Whether any tween or spring is live. |
| `SetExternalAnimation(key any, on bool)` / `ExternalAnimating() bool` | Register an animation driver that lives outside this package (a markup document's animators) so a loop that renders only on damage keeps going. |
| `AnimClock() time.Duration` | Total animation time the frame loop has advanced: a monotonic clock for anything that must tick in step with the tweens (headless captures advance it in fixed slices). |
| `SetVirtualClock(on bool)` / `VirtualClock() bool` | When on, the animation clock is the only clock: deterministic captures ignore wall time. |

The `ui/shell` runner calls `TickAnimations` once per frame, so an
application built on `shell.RunUI` only ever calls `Tween`/`SpringTo`. The
view function reads the animated property like any other:

```go
var pulse core.Property[float32]

func init() {
    core.TweenLoop(&pulse, 1, 900*time.Millisecond, core.EaseInOutQuad, core.PingPong)
}

func view() ui.Element {
    dot := ui.Div().Size(40, 40).Bg(ui.RGB(0x4dabf7)).Rounded(20).
        Opacity(0.35 + 0.65*pulse.Get())
    // ...
}
```

## Math

All math types are `float32` value types; methods return new values and
never take pointers. `Mat4` is column-major (WGSL convention, element
`(row r, col c)` at index `c*4+r`), transforms are right-handed, and
projection matrices map depth to WebGPU's `[0,1]` clip range, not OpenGL's
`[-1,1]`.

### Vectors

| Type | Constructor | Methods |
|------|-------------|---------|
| `Vec2{X, Y}` | `core.V2(x, y)` | `Add`, `Sub`, `Mul` (component-wise), `Scale(s)`, `Dot`, `Length`, `Lerp(b, t)`, `Normalize` |
| `Vec3{X, Y, Z}` | `core.V3(x, y, z)` | `Add`, `Sub`, `Mul`, `Scale`, `Neg`, `Dot`, `Cross`, `Length`, `Lerp`, `Normalize` |
| `Vec4{X, Y, Z, W}` | `core.V4(x, y, z, w)` | `Add`, `Sub`, `Scale`, `Dot`, `XYZ() Vec3` |

`Normalize` of a zero vector returns the zero vector rather than NaN. In
GPU-shared structs a `Vec3` field must be padded to 16 bytes to satisfy WGSL
alignment; the `gfx` code generator enforces this.

### Mat4

| Function | Result |
|----------|--------|
| `Mat4Identity()` | identity |
| `Mat4Translate(t Vec3)`, `Mat4Scale(s Vec3)` | translation / non-uniform scale |
| `Mat4RotateX(rad)`, `Mat4RotateY(rad)`, `Mat4RotateZ(rad)` | axis rotations |
| `Mat4Perspective(fovyRad, aspect, znear, zfar)` | right-handed perspective, depth in `[0,1]` |
| `Mat4Ortho(left, right, bottom, top, znear, zfar)` | right-handed orthographic. For 2D UI use `Mat4Ortho(0, w, h, 0, -1, 1)`: y grows downward. |
| `Mat4LookAt(eye, center, up)` | view matrix; the target lands on `-Z` |

| Method | Result |
|--------|--------|
| `(Mat4).Mul(b)` | `a*b`: apply `b` first, then `a` |
| `(Mat4).MulVec4(v)` | transform a homogeneous vector |
| `(Mat4).MulPoint(p Vec3)` | transform a position with `w=1`, dropping `w` |
| `(Mat4).Transpose()` | transpose |
| `(Mat4).Inverse() (Mat4, bool)` | inverse by cofactor expansion; `ok=false` and identity for a singular matrix. Used to unproject screen rays. |

```go
// Scale then translate: T*S applied to (1,1,1) -> (12,2,2).
m := core.Mat4Translate(core.V3(10, 0, 0)).Mul(core.Mat4Scale(core.V3(2, 2, 2)))
m.MulPoint(core.V3(1, 1, 1))
```

### Quat

`Quat{X, Y, Z, W}` with `X, Y, Z` imaginary and `W` real.

| Function / method | Result |
|-------------------|--------|
| `QuatIdentity()` | `Quat{W: 1}` |
| `QuatAxisAngle(axis Vec3, rad)` | rotation about `axis` (normalised for you) |
| `(Quat).Mul(b)` | `a*b`: rotation `b` followed by `a` |
| `(Quat).Conjugate()`, `(Quat).Normalize()` | conjugate; normalise (identity for a zero quaternion) |
| `(Quat).Rotate(v Vec3)` | rotate a vector |
| `(Quat).Slerp(b, t)` | spherical interpolation, taking the short way round and falling back to lerp when nearly parallel |
| `(Quat).Mat4()` | the rotation matrix of a unit quaternion |

```go
q := core.QuatAxisAngle(core.V3(0, 0, 1), math.Pi/2)
q.Rotate(core.V3(1, 0, 0))          // (0, 1, 0)
q.Mat4().MulPoint(core.V3(1, 0, 0)) // the same
```

### Geometry queries

`geom.go` holds the intersection primitives shared by picking and physics:

| Function | Result |
|----------|--------|
| `RayAABB(origin, dir, bmin, bmax Vec3) (t float32, hit bool)` | slab test; `t` is the entry distance (0 when starting inside) |
| `RayTriangle(origin, dir, a, b, c Vec3) (t, hit)` | Möller–Trumbore, both-sided, with a hair of slack so a ray on a shared edge hits one of the two triangles |
| `RaySphere(origin, dir, center Vec3, r) (t, hit)` | nearest positive distance (0 when inside) |
| `ClosestOnSegment(p, a, b Vec3) Vec3` | closest point on segment `ab` |
| `ClosestOnTriangle(p, a, b, c Vec3) Vec3` | closest point on a triangle (Ericson, *Real-Time Collision Detection* §5.1.5) |

## A complete example

`examples/counter` is the smallest real program: one property, a view
function that reads it, and two buttons that write it. The shell runs the
loop, ticks animations and flushes bindings; the application never touches
`Loop` directly.

```go
package main

import (
    "fmt"

    "github.com/sushvibez/gfx/core"
    "github.com/sushvibez/gfx/ui"
    "github.com/sushvibez/gfx/ui/shell"
)

func button(label string, onClick func()) ui.Element {
    return ui.Div().Size(72, 40).Bg(ui.RGB(0x3b5bdb)).Rounded(8).
        HoverBg(ui.RGB(0x4c6ef5)).PressBg(ui.RGB(0x2f44ad)).
        JustifyCenter().ItemsCenter().OnClick(onClick).
        Children(ui.Text(label, ui.RGB(0xffffff)))
}

func main() {
    var count core.Property[int]
    view := func() ui.Element {
        return ui.Div().Col().P(24).Gap(16).Bg(ui.RGB(0x16161e)).Children(
            ui.Text(fmt.Sprintf("count: %d", count.Get()), ui.RGB(0xe8e8f0)),
            ui.Div().Row().Gap(12).Children(
                button("-", func() { count.Set(count.Get() - 1) }),
                button("+", func() { count.Set(count.Get() + 1) }),
            ),
        )
    }
    if err := shell.RunUI(shell.Options{Title: "counter", Width: 420, Height: 240}, view); err != nil {
        panic(err)
    }
}
```

See [ui.md](ui.md) for the toolkit and the shell.
