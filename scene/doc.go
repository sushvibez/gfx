// Package scene is the public authoring layer (decided, see /DESIGN.md §4.1):
// a tree of nodes where node values are {id, generation} handles and node
// data lives in ecs component columns, so users get O(1) per-object get/set
// while render and compute passes bind the same columns as contiguous
// storage buffers.
//
// What exists today is the minimal Tree that Phase 3 needed: Transform and
// parent/depth columns plus a per-frame world-matrix propagation pass.
// Planned on top, not yet built (Phase 4+): typed node kinds (Node2D,
// Node3D, Control, ...), names and child order, lifecycle callbacks and
// signals, the Blueprint-style frame-synced Task coroutines for latent
// actions (/DESIGN.md §6.3), and a text scene format that loads into this
// tree (/DESIGN.md §9).
package scene
