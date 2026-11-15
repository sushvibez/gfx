# Ray tracing — roadmap

Traced lighting for `render`, kept beside the rasteriser and switched per
frame. This note is the order of work and its status; update the table in
the commit that changes a row.

## What it is, and is not

- **Rays are traced by ordinary GPU shaders** (WGSL), against a bounding
  volume hierarchy built on the CPU and held in storage buffers. That runs
  wherever WebGPU does, browsers included.
- **Not hardware ray tracing.** wgpu-native's C header (v29) has no
  acceleration structures or ray queries, and the WebGPU standard has none
  at all. The traversal sits behind one WGSL function (`trace_any`, later
  `trace_nearest`), so a hardware ray query can replace it on native when
  the C API has one.
- **Hybrid, not a path tracer.** The camera's view is rasterised as before
  (so every material carries over); what is traced is light: who the sun
  reaches, how open a point is to the sky, what light bounces in. A fully
  path-traced picture is the last row, as a reference mode.
- **Opt-in and switchable.** `FrameSettings.TracedShadows` and friends turn
  each traced term on for a frame; with them off nothing changes and
  nothing is paid.

## Measured (2026-11-15)

Intel integrated graphics, Harbor's 9,586 shapes as boxes, one compute
kernel (`games/harbor/cmd/rtbench`): 150 to 330 million rays a second. At
1600×900, per frame: primary rays 7.8 ms; with a sun shadow ray 9.5 ms;
with shadow and one occlusion ray 13 ms; with shadow and one diffuse
bounce 31 ms (8.5 ms at 800×450). Boxes are the cheapest shape there is:
triangle meshes are not measured yet.

## Order of work

| ✓ | step | what it gives | verify |
| --- | --- | --- | --- |
| [x] | 0. Measure before building | the numbers above | `go run ./cmd/rtbench` in `games/harbor` |
| [x] | 1. The trace scene and traced sun shadows: `Renderer3D.SetTraceScene` (boxes, cylinders, wedges under a BVH), `FrameSettings.TracedShadows`; the material shader shoots the shadow ray itself | shadows exact to the shape at any distance, no map, no acne or peter-panning; soft by jitter | `TestTracedShadow`, `TestTraceBuild`; Harbor golden at 1600×900 on Intel integrated graphics: 27 ms a frame traced, 22 ms raster |
| [x] | 2. Traced ambient occlusion in the material shader (`FrameSettings.TracedAO`, three short rays a pixel) | occlusion from real geometry, off screen included; grainy until step 3 | `TestTracedAO`; with step 1, Harbor golden at 1600×900: 55 ms a frame traced against 35 ms raster in the same run, 32 against 23 at 65 % scale |
| [ ] | 3. A temporal filter (reprojection and accumulation) for the traced terms | one ray a pixel without the grain | screenshot in motion |
| [ ] | 4. Traced shadows for the lamps (spot and point lights) | every light shadowed, not four | test |
| [ ] | 5. One bounce of diffuse light at half resolution, filtered | colour bleeding, light under overhangs | Harbor golden, with and without |
| [ ] | 6. Traced reflections for glossy surfaces (what is off screen included) | puddles and water that mirror everything | Harbor rain |
| [ ] | 7. Triangle meshes in the trace scene (a BVH per mesh under the instance BVH) | models and characters cast and receive | measured rays a second |
| [ ] | 8. Traced haze: light shafts and lamp beams from rays, not shadow maps | shafts at any distance | Harbor golden |
| [ ] | 9. A reference path tracer: progressive, everything traced | ground truth to compare the rest against | converged picture |
| [ ] | 10. Hardware ray queries on native, when wgpu-native exposes them | the same picture, several times the rays | measured |

## Rules

- Each row lands with its own test and a measured cost in this note.
- A traced term replaces its rasterised twin only when switched on; both
  stay working.
- Nothing here may break the browser build: no feature the WebGPU standard
  lacks, and WGSL the browser's stricter validator accepts.
