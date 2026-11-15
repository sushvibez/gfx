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
| [x] | 3. A temporal filter: `PostSettings.Temporal` reprojects and averages the lit picture, `FrameSettings.Temporal` turns the noise each frame (traced occlusion then sends one ray, not three) | one ray a pixel without the grain | `TestPostTemporalSettlesGrain`; Harbor golden at 1600×900 in one run: raster 57 ms, traced with the filter 79 ms, traced without it 97 ms (at 65 % scale 35, 54 and 86). Not yet looked at in motion |
| [x] | 4. Traced shadows for the lamps: `FrameSettings.TracedLightShadows` sends a ray to every point and spot light that reaches a surface (to a small disc at the light when the temporal filter is on) | every light shadowed, not four | `TestTracedLightShadow`; Harbor night at 1600×900 in one run: raster 58 ms, traced 76 ms (38 and 67 at 65 % scale) |
| [x] | 5. One bounce of diffuse light: `FrameSettings.TracedBounce`. Post's bounce pass (`post_bounce.go`) traces it at a quarter of the resolution from the depth buffer (`trace_nearest`, `TraceInstance.Color`); the material shader adds it a frame later. It was first written as rays in the material shader, where in Harbor's scene on Intel integrated graphics (D3D12) it lost the GPU device most runs: bisected to the nearest-hit traversal in that shader (with `trace_any` standing in the same frames ran clean; fewer rays did not help, so not load); cause not found. As its own pass it has not lost the device in any run | colour bleeding, light under overhangs | `TestTracedBounce`; Harbor golden at 1334×750 (the traced pixel budget): 32 ms a frame with it, 31 ms without |
| [x] | 6. Traced reflections: `FrameSettings.TracedReflections`. Post's reflection pass (`post_reflect.go`, half resolution, mirror pixels only) traces the mirrored ray through the trace scene; the combine uses it wherever the screen-space ray found nothing (off screen, hidden). What it shows is the shape's plain colour under the sun and sky, not its material or the lamps. The combine now takes the sky's reflection out with a floor at zero and adds what was met, for both kinds of reflection | puddles and water that mirror what is off screen | `TestTracedReflection`; Harbor traced rain and golden-by-the-ship ran without device loss |
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
