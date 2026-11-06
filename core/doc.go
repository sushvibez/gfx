// Package core provides the foundation shared by every other module:
// vector/matrix/quaternion math, the reactive property and signal/slot
// system, and the main event loop.
//
// Design notes (see /DESIGN.md §3):
//   - Property[T] supports QML-style declarative bindings with automatic
//     dependency tracking; re-evaluation is lazy and batched per frame.
//   - Signal[T] is a typed multicast delegate, emitted and received on the
//     owning goroutine only; another goroutine hands work to that loop
//     with Loop.Post (there is no queued-connection emission).
//   - All UI-facing state is owned by a single goroutine (the "main loop"),
//     which removes locking from the hot path.
package core
