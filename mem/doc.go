// Package mem provides allocation strategies that keep the Go GC out of
// the frame loop: chunked arena allocators with scoped lifetimes
// (Frame / Scene / App), typed pools, and generation-checked handles that
// replace pointers in long-lived data structures.
//
// Design notes (see /DESIGN.md §7):
//   - Arenas hand out POD-only memory carved from large []byte chunks and
//     are reset wholesale at the end of a frame or request. Objects inside
//     an arena must not hold Go pointers; cross-references use Handle[T].
//   - Deterministic destruction (reference counting) is reserved for GPU
//     resources, not general memory.
//   - Hot paths are guarded by 0 allocs/op benchmarks in CI rather than by
//     fighting or forking the runtime.
package mem
