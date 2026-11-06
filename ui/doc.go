// Package ui is the 2D user interface toolkit. Its architecture is the
// GPUI hybrid (decided, /DESIGN.md §10), with QML's property system
// underneath:
//
//   - State is retained: view objects hold core.Property values, so
//     bindings, signals and animation work exactly as in QML.
//
//   - The UI itself is immediate: a render function rebuilds a lightweight
//     element tree every frame from that state. No element identity to
//     manage, no invalidation protocol to get wrong.
//
//   - Elements use a Tailwind-style fluent API:
//
//     ui.Div().Row().Gap(8).P(16).Bg(ui.RGB(0x1e1e28)).Children(
//     ui.Text("Hello", ui.RGB(0xffffff)),
//     )
//
// Layout is a flexbox subset (direction, gap, padding, fixed sizes, grow
// factors, justify/align), painting goes through render.Renderer2D, and
// hit testing walks the laid-out tree topmost-first. Because property
// reads during render are observable (§3 dependency tracking), ui/shell
// skips the per-frame rebuild when core.Revision() and the window are
// unchanged — retained-mode efficiency without retained-mode bookkeeping.
package ui
