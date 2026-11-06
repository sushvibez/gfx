# The 2D path, against Zed's "We rebuilt the UI like a videogame"

Reviewed 2026-09-22, closed 2026-09-23. Zed's post
(<https://zed.dev/blog/videogame>) describes how GPUI draws its UI. We
checked our 2D path against every technique in it. Most of it we already
did — in several cases from the same source, since our renderer was
adapted from GPUI's — and the three real gaps were all in text. They are
closed; this page is the record of what was found and what it cost,
kept out of the TODO because none of it is open.

## Already doing it their way

- **A shader per primitive**, not a general-purpose 2D library: eleven
  pipelines (rounded quad, shadow, sprite, colorize, premul, stencil,
  premul-add, subpixel text, distance-field text, ShaderEffect,
  composite).
- **Rounded rectangles as an SDF** — the same shrink-by-radius,
  `length(max(0, p)) + min(0, max(p.x, p.y)) - radius` construction, in
  the quad shader and again in every pipeline's clip.
- **Drop shadows by Evan Wallace's closed form** — the erf convolution
  along x and a finite slide along y, ported from GPUI's
  `blur_along_x` / `gaussian` / `erf`, bounds expanded by 3·blur.
- **Glyphs tinted by multiplication** rather than re-rasterised per
  colour.
- **Sub-pixel positional variants** of each glyph, keyed by phase.
- **Instanced draws** for rectangles and shadows.
- **Layers and a painter's-algorithm order** through the blend-group and
  clip machinery.

## Two deliberate differences

Recorded as choices rather than gaps.

- **We keep submission order** instead of sorting into
  shadows-then-rects-then-glyphs, because an arbitrary overlap has to
  composite correctly.
- **Our layout is measure/arrange**, `measure(ctx) → intrinsic` plus
  `layout(ctx, rect)`, where they take Flutter's constraints-down /
  sizes-up. The same information flowing the same way, in the shape Qt
  and WPF use, with the measure pass memoised per frame.

## The three gaps, all in text, all closed

Measured on a page of sixty lines, each measured for layout and then
drawn — what a retained-mode frame does:

| | before | after |
|---|---|---|
| distance-field page | 707 µs | **195 µs** |
| raster page | 753 µs | **216 µs** |
| one 15-glyph run | 1.9–2.1 µs | **1.0–1.1 µs** |

### A shaped-run cache (`render/shape.go`)

Shaping — which face carries a rune, where the pen stands before it —
costs a UTF-8 decode, a zero-width test, an emoji-range test, a
fallback-coverage lookup, a kern lookup and an advance lookup per glyph,
and none of it depends on where the run lands. Cached on (faces, string)
and swept when no frame has asked for it in 240. `MeasureText` shares
it, so a run that is measured and then drawn is walked once between them
rather than once each; its own `mcache` is gone.

**Neither the tracking nor the field path's size multiplier is in the
key** — the pen offsets are stored unscaled and unspaced and the caller
applies both — because a size that slides would otherwise make an entry
a frame, which is the trap `DrawTextSize` exists to avoid. There is a
test for exactly that.

### Alpha-only glyph sheets

`gpu.FormatR8Unorm` (there was no one-byte format), `Texture.bpt` so an
upload knows its row stride, `NewAlphaImage`, and an `fs_glyph` that
takes coverage from the red channel. A sheet is 1 MB instead of 4, on
the CPU and again on the GPU; the example's raster column went from
68 MB to 17. Colour emoji are pixels rather than coverage, so they moved
to a sheet of their own, allocated only if one is ever drawn.

### Instanced glyph draws

One 16-float record a quad — four corners in clip space, the uv rect,
the colour — against the 48 floats six vertices took. Text has its own
instance buffer rather than a change to the sprite one, because paths
share that and emit arbitrary triangles.

Needed `VertexLayout.StepMode` in `gpu`, which did not exist. The enum
is Undefined 0, Vertex 1, Instance 2, and Undefined defaults to Vertex —
which is why every layout written before this still steps per vertex.

## Two more things that came out of the same pass

### A bucketed shelf packer (`render/atlaspack.go`)

A shelf's height is the glyph's rounded up to four texels, and a glyph
goes on a shelf of its own bucket, so a comma no longer sits in a row
made cap-height by an 'H'. Measured by the number that matters — glyphs
a sheet holds before it overflows — **+17 % at 13 px, +31 % at 20, +21 %
at 34, +25 % at 48**.

Not etagere's free list: nothing here ever frees a glyph. Two things
this taught: best-fit across all shelves (letting a short glyph settle
on a tall one) is *worse*, 0–13 % against 17–31 %, because it fills the
tall shelves and starves the short ones; and "fewer rows for four
hundred glyphs" is the wrong measure, since a hundred glyphs occupy
three per cent of a sheet however they are arranged.

### Sheets are paged (`render/atlassheet.go`)

A full sheet opens another instead of dropping the glyph and leaving a
permanent hole — which is what the old code did, with a comment saying
multi-page atlases would come with the real batcher. They could come:
the batcher already breaks a batch when the bind group changes, so a
second sheet costs one draw call on the frames that use both. All three
kinds (coverage, colour emoji, LCD) share the one page type.

### Sixteen sub-pixel phases, not four

The residual is half a phase, and it shows as a glyph landing off where
layout put it: measured on a single glyph across 32 offsets, **0.36 px
of spread at four phases and 0.18 at sixteen**. Not the fourfold the
arithmetic suggests — the 3× raster is hinted and quantises too. A run
averages its glyphs' residuals away and looks fine either way, which is
why the first version of that test could not tell them apart. Sixteen
rasterisations of every glyph is sixteen times the sheet, so a heading
face now fills one on its own — which is what the pager is for.
