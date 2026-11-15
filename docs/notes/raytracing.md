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
| [x] | 7. Triangle meshes in the trace scene: `TraceMesh` and `TraceInstance.Mesh`. Each distinct mesh gets its own BVH over its triangles (built once however many instances use it), reached from the instance's leaf in the mesh's own space; triangles are two-sided. The passes that need the nearest hit are built twice, with and without the mesh traversal, and the scene picks | models and characters cast and receive | `TestTracedMeshShadow`, `TestTraceBuild`. Harbor has no meshes and costs the same with the code in (golden traced at 1334×750: 33 ms a frame). Rays a second through a real model: not measured yet |
| [x] | 8. Traced haze: `FrameSettings.TracedHaze`. The shafts' march (`post_atmos.go`, half resolution, 20 steps) asks the trace scene about the sun on one step in five and about each lamp in reach on one in four, the steps rotating with the frame so the temporal filter covers them all. Dear: air rays toward a low sun cross the whole scene. Off in Harbor unless asked (`-rthaze`) | shafts at any distance, a beam cut for every lamp | `TestTracedHaze`; Harbor at 1334×750 traced: golden 35 to 55 ms a frame, night 30 to 44 ms (asking on every other step: golden 33 to 90 ms). The picture: stacks shade the air more in golden, night is close to the maps' |
| [x] | 9. A reference path tracer: `render.PathTracer` (`pathtrace.go`), shown through `View3D.Reference`. One path a pixel a frame (the eye's ray, up to four matt bounces, a shadow ray to the sun at each, the sky where it leaves), averaged while the camera, light and scene stay as they are. It draws what the trace scene holds: plain-coloured matt shapes. No textures, lamps, mirrors or air yet, so it is ground truth for the sun's shadows, occlusion and bounce, not for the whole picture | ground truth to compare the rest against | `TestPathTracer` (shadow lit by the sky, colour from a red wall on the floor beside it, the mean settles, a moved camera starts again). Harbor `-reference` (P): 21 ms a frame at 1334×750 traced at half the pixels each way; the first run of the new shader kept the device. Open: dark faces in Harbor show a regular dotted pattern that does not change with the random numbers, so it is not noise; cause not found (near-coplanar detail shapes are the suspect) |
| [ ] | 10. Hardware ray queries on native, when wgpu-native exposes them | the same picture, several times the rays | measured |

## The device lost on a first run (2026-11-15, found and fixed)

The device losses recorded in rows 5 and 6 had one cause. On Intel
integrated graphics (D3D12), the first run of a newly built shader that
walked the hierarchy for the nearest hit (`trace_nearest`: the bounce and
reflection passes, and before them the material shader) hung the GPU:
Windows logged `LiveKernelEvent 141` (a GPU engine that stopped answering),
reset the driver, and the device was gone by the third frame. The second
run of the same build never failed, which is why it looked like chance.
Making a shader new to the driver (changing any constant in it) reproduced
it every time, at any resolution, with any one of the passes on.

What did not help: tiny first frames, waiting for each early frame, a
pause, shorter rays, leaving the mesh code out. What did: bounding the
walk. `trace_nearest`, `trace_any` and `trace_mesh` now stop after
`TRACE_STEPS` nodes and read at most `TRACE_LEAF` entries of a leaf, bounds
a walk over a well-formed hierarchy never reaches. Five cold runs in a row
then survived (golden, night, rain; with and without reflections) where
every one before had not. Why the unbounded loop ran away only on a first
run is not known: the same shader on the same data ends on every later
run, so the driver's first compilation of it is the suspect, and that is
a guess.

## Rules

- A loop in a shader has a bound that does not depend on the data it
  reads (see above).

- Each row lands with its own test and a measured cost in this note.
- A traced term replaces its rasterised twin only when switched on; both
  stay working.
- Nothing here may break the browser build: no feature the WebGPU standard
  lacks, and WGSL the browser's stricter validator accepts.
