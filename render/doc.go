// Package render builds the actual renderers on top of gpu:
//
//   - A render graph: passes declare the resources they read/write, the
//     graph orders passes, allocates transient attachments, and inserts
//     the few barriers WebGPU leaves to the user (Unreal RDG style).
//   - 2D: retained command lists with batching (sorted by pipeline,
//     texture atlas, clip), glyph-atlas text first with an SDF/compute
//     rasterization path later.
//   - 3D: clustered forward+ PBR to start; shadows and a post-processing
//     stack come after the basics are solid.
//
// See /DESIGN.md §9.
package render
