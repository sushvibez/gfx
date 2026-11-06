// Package ecs implements an Entity/Component store whose component arrays
// are laid out to be GPU-sharable: a component pool is a contiguous slice
// of POD structs that can be bound directly as a WebGPU storage buffer and
// processed by compute shaders using stride and offset, with dirty-range
// tracking (one merged span per frame today) so only modified data uploads.
//
// The current storage is sparse-set — one Component[T] pool per type,
// dense slice + swap-remove, iterated via Each/Each2. The archetype
// grouping from /DESIGN.md §4 can replace the internals later without
// changing the API.
//
// Design notes (see /DESIGN.md §4):
//   - Entities are 32-bit index + generation handles, never pointers.
//   - Components chosen for GPU sharing must satisfy WGSL layout rules;
//     the gfx gen codegen (script/gen) emits the matching WGSL struct plus
//     compile-time layout assertions on the Go side.
//   - Planned, not yet built: systems that declare read/write component
//     sets, scheduled in parallel where sets do not conflict (serial
//     fallback on js/wasm).
package ecs
