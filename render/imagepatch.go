package render

import "math"

// Image drawing beyond one stretched quad: a sub-rectangle, tiling, and the
// 9-patch a QML BorderImage draws. Sizes in source pixels are given against
// the image's SOURCE size (srcW, srcH), not the decoded texture's, so a
// texture decoded smaller than its source still tiles and slices at the
// source's scale.

// DrawImageRegion draws the sub-rectangle (u0,v0)-(u1,v1) of img (0..1 texture
// coordinates) stretched to the pixel box.
func (r *Renderer2D) DrawImageRegion(img *Image, x, y, w, h, u0, v0, u1, v1 float32, c RGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	r.quadUV(img.bind, x, y, w, h, u0, v0, u1, v1, c)
}

// DrawImageTiled covers the box with copies of img, each tileW×tileH pixels,
// centred on the box (QML's default alignment for Image.Tile) and clipped to
// it. A tile as wide as the box tiles vertically only, and the other way.
func (r *Renderer2D) DrawImageTiled(img *Image, x, y, w, h, tileW, tileH float32, c RGBA) {
	if w <= 0 || h <= 0 || tileW <= 0 || tileH <= 0 {
		return
	}
	// At most a few thousand tiles: a 1-pixel tile over a window would
	// queue millions of quads; past the cap the tiles grow to fit.
	const maxTiles = 4096
	for (w/tileW+2)*(h/tileH+2) > maxTiles {
		tileW, tileH = tileW*2, tileH*2
	}
	x0 := x + w/2 - tileW*float32(math.Ceil(float64(w/2/tileW)))
	y0 := y + h/2 - tileH*float32(math.Ceil(float64(h/2/tileH)))
	r.PushClip(x, y, w, h)
	for ty := y0; ty < y+h; ty += tileH {
		for tx := x0; tx < x+w; tx += tileW {
			r.quadUV(img.bind, tx, ty, tileW, tileH, 0, 0, 1, 1, c)
		}
	}
	r.PopClip()
}

// PatchMode is how a 9-patch fills its edges and middle (QML's
// BorderImage.TileMode).
type PatchMode int

const (
	PatchStretch PatchMode = iota // scale the piece to the length
	PatchRepeat                   // tile at source size, the last tile cut
	PatchRound                    // tile a whole number of times, scaled to fit
)

// patchSeg is one piece along an axis: where it goes and how much of the
// source span it shows (1 = all of it; less for a cut last tile).
type patchSeg struct{ pos, size, frac float32 }

// patchSegs lays a source span of length tile over a box span of length
// span from start, by mode.
func patchSegs(start, span, tile float32, mode PatchMode) []patchSeg {
	if span <= 0 {
		return nil
	}
	if tile <= 0 || mode == PatchStretch {
		return []patchSeg{{start, span, 1}}
	}
	n := span / tile
	if n > 1024 {
		return []patchSeg{{start, span, 1}} // a degenerate tile: stretch
	}
	if mode == PatchRound {
		k := float32(math.Max(1, math.Round(float64(n))))
		segs := make([]patchSeg, int(k))
		for i := range segs {
			segs[i] = patchSeg{start + span*float32(i)/k, span / k, 1}
		}
		return segs
	}
	var segs []patchSeg
	for p := float32(0); p < span; p += tile {
		s := min(tile, span-p)
		segs = append(segs, patchSeg{start + p, s, s / tile})
	}
	return segs
}

// DrawNinePatch draws img as a 9-patch over the box: the borders (left,
// top, right, bottom, in source pixels of an srcW×srcH source) keep their
// size, the corners are drawn as they are, and the edges and the middle
// fill by hMode (horizontally) and vMode (vertically). A box smaller than
// two opposite borders shrinks them in proportion.
func (r *Renderer2D) DrawNinePatch(img *Image, x, y, w, h float32, srcW, srcH, left, top, right, bottom float32,
	hMode, vMode PatchMode, c RGBA) {
	if w <= 0 || h <= 0 || srcW <= 0 || srcH <= 0 {
		return
	}
	left, right = clampBorders(left, right, srcW)
	top, bottom = clampBorders(top, bottom, srcH)
	// The box's borders: the source's, shrunk if they do not fit.
	bl, br := left, right
	if s := bl + br; s > w {
		bl, br = bl*w/s, br*w/s
	}
	bt, bb := top, bottom
	if s := bt + bb; s > h {
		bt, bb = bt*h/s, bb*h/s
	}
	// Columns and rows: source span (texture coordinates) and box spans.
	type span struct {
		u0, u1 float32
		segs   []patchSeg
	}
	midW, midH := srcW-left-right, srcH-top-bottom
	cols := []span{
		{0, left / srcW, []patchSeg{{x, bl, 1}}},
		{left / srcW, (srcW - right) / srcW, patchSegs(x+bl, w-bl-br, midW, hMode)},
		{(srcW - right) / srcW, 1, []patchSeg{{x + w - br, br, 1}}},
	}
	rows := []span{
		{0, top / srcH, []patchSeg{{y, bt, 1}}},
		{top / srcH, (srcH - bottom) / srcH, patchSegs(y+bt, h-bt-bb, midH, vMode)},
		{(srcH - bottom) / srcH, 1, []patchSeg{{y + h - bb, bb, 1}}},
	}
	for _, row := range rows {
		if row.u1 <= row.u0 {
			continue
		}
		for _, ry := range row.segs {
			if ry.size <= 0 {
				continue
			}
			v1 := row.u0 + (row.u1-row.u0)*ry.frac
			for _, col := range cols {
				if col.u1 <= col.u0 {
					continue
				}
				for _, cx := range col.segs {
					if cx.size <= 0 {
						continue
					}
					u1 := col.u0 + (col.u1-col.u0)*cx.frac
					r.quadUV(img.bind, cx.pos, ry.pos, cx.size, ry.size, col.u0, row.u0, u1, v1, c)
				}
			}
		}
	}
}

// clampBorders keeps two opposite borders inside the source.
func clampBorders(a, b, size float32) (float32, float32) {
	a, b = max(a, 0), max(b, 0)
	if s := a + b; s > size {
		a, b = a*size/s, b*size/s
	}
	return a, b
}
