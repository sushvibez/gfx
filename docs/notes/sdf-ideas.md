# sdf — signed distance fields subproject

**Status: one built, the rest an idea collection.** The MSDF glyph atlas
— the first item under *Rendering & text* below — shipped as
`render/msdf/` and `render/msdftext.go`; see [`TODO.md`](TODO.md) for
what it does and what it measured. Everything else here is unbuilt. This
file gathers everything SDF-shaped we might build so that scope stays a
selection problem, not a research problem.

## What the framework already has (SDF-adjacent)

- The 2D renderer IS an SDF renderer: rounded rects/borders/dashes are
  analytic SDF quads in WGSL (`render/renderer2d.go`, adapted from GPUI).
- WGSL compute passes work on native + web (`gpu` compute API, used by tests).
- `ui.Viewport3D` / `render.View3D` can host any offscreen pass — including a
  full-screen raymarcher — inside a normal UI layout.
- DESIGN.md already earmarks **SDF text** as the successor to the glyph atlas.

## Ideas

### Rendering & text
- ✅ **SDF glyph atlas** — multi-channel (MSDF) glyphs: crisp text at every
  scale/rotation, cheap outlines/glow/soft-shadow; replaces the per-size
  atlases in `render.Font`. BUILT — `render/msdf/`, and the biggest
  practical win in this list as predicted.
- **MSDF icon set** — resolution-independent UI icons (tool rails, gml
  `Image { source: "x.svg" }` could rasterize to MSDF instead of tessellating).
- **Soft shadows / rounded everything in 2D** — we already do this; extend to
  arbitrary path SDFs (approximate a `render.Path` by a coarse SDF texture via
  jump flooding) → blur-free glows, insets, outlines for any shape.

### 3D raymarching
- **Raymarched SDF viewport** — a shadertoy-style scene (primitives, smooth
  min, domain repetition, soft shadows, AO) as a WGSL fragment pass inside
  `ui.Viewport3D`; camera shared with the orbit controller.
- **SDF CSG sculpting toy** — interactive union/subtract/blend of primitive
  SDFs with gml sliders; the "instant CSG" complement to a B-rep CAD kernel
  (concept modeling before precise modeling; Womp-style UX).
- **Hybrid render** — raymarched SDF objects depth-composited with the
  rasterized `Renderer3D` scene (write ray depth to the shared depth buffer).

### Meshing & interop
- **Marching cubes / dual contouring** — SDF → triangle mesh, feeding
  `Renderer3D` meshes, STL export, and (long game) a CAD kernel as a lattice/organic
  geometry source Parasolid-style kernels are bad at.
- **Mesh → SDF baking** — 3D texture distance fields from glTF meshes
  (compute pass; useful for collision below).

### Simulation & gameplay
- **SDF collision fields** — games: sample a world SDF for
  obstacle avoidance and line-of-sight instead of per-mesh tests; gradients
  give repulsion vectors for free (swarm steering).
- **2D destructible terrain** — SDF texture painted/carved at runtime
  (union/subtract brushes), rendered with the existing quad pipeline.

### Compute techniques (enablers)
- **Jump flooding (JFA)** — GPU SDF generation from any silhouette in
  O(log n) passes; the workhorse for baked 2D fields and selection outlines.
- **8SSEDT** — CPU fallback for wasm-without-compute contexts.
- **Selection outline pass** — JFA outline around picked 3D objects (listed
  a CAD kernel would build this).

## Candidate first demo
The **raymarched SDF viewport** (shadertoy scene in `ui.Viewport3D` with gml
sliders driving primitive params) — zero new infrastructure, maximally
visible, and it exercises Viewport3D with a non-rasterized source.

## References
- Inigo Quilez's distance-function articles (canonical SDF primitive/operator zoo).
- Valve, "Improved Alpha-Tested Magnification" (SDF text, SIGGRAPH 2007).
- Chlumsky, msdfgen (multi-channel SDF glyphs).
- Rong & Tan, "Jump Flooding in GPU" (JFA).
