package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// LoadImage decodes PNG/JPEG bytes into a drawable GPU image (GPUI
// image.rs). Pixels are stored straight (non-premultiplied) alpha to match
// the 2D pipeline's blend.
func (r *Renderer2D) LoadImage(data []byte) (*Image, error) {
	return r.LoadImageScaled(data, 0)
}

// LoadImageScaled is LoadImage with a maximum dimension: sources larger than
// maxDim on either axis are box-downscaled on the CPU before upload, so heavy
// minification doesn't alias at draw time (the GPU sampler has no mipmaps).
// maxDim <= 0 keeps the native size.
func (r *Renderer2D) LoadImageScaled(data []byte, maxDim int) (*Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("render: decoding image: %w", err)
	}
	b := img.Bounds()
	// NRGBA is straight-alpha; draw.Draw converts from any source model.
	nrgba := image.NewNRGBA(b)
	draw.Draw(nrgba, b, img, b.Min, draw.Src)
	nrgba = downscaleNRGBA(nrgba, maxDim)
	return r.NewImage(nrgba.Rect.Dx(), nrgba.Rect.Dy(), nrgba.Pix), nil
}

// downscaleNRGBA area-averages src down so max(w,h) <= maxDim, preserving
// aspect ratio. Returns src unchanged when maxDim <= 0 or already small
// enough. Box filtering is what heavy minification needs; magnification is
// left to the GPU's bilinear sampler.
func downscaleNRGBA(src *image.NRGBA, maxDim int) *image.NRGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	if maxDim <= 0 || (sw <= maxDim && sh <= maxDim) || sw == 0 || sh == 0 {
		return src
	}
	scale := float64(maxDim) / float64(max(sw, sh))
	dw, dh := max(int(float64(sw)*scale+0.5), 1), max(int(float64(sh)*scale+0.5), 1)
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for dy := 0; dy < dh; dy++ {
		y0, y1 := dy*sh/dh, (dy+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < dw; dx++ {
			x0, x1 := dx*sw/dw, (dx+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sr, sg, sb, sa, n uint32
			for y := y0; y < y1; y++ {
				row := src.Pix[y*src.Stride+x0*4 : y*src.Stride+x1*4]
				for x := 0; x < len(row); x += 4 {
					sr += uint32(row[x])
					sg += uint32(row[x+1])
					sb += uint32(row[x+2])
					sa += uint32(row[x+3])
					n++
				}
			}
			o := dy*dst.Stride + dx*4
			dst.Pix[o] = byte(sr / n)
			dst.Pix[o+1] = byte(sg / n)
			dst.Pix[o+2] = byte(sb / n)
			dst.Pix[o+3] = byte(sa / n)
		}
	}
	return dst
}

// LoadGIFFrames decodes an animated GIF into one drawable GPU image per
// frame, plus each frame's display delay in centiseconds. Frames are
// composited onto a persistent canvas honoring the GIF disposal method, so
// partial/optimized frames render correctly. Returns an error for non-GIF
// data or a single-frame (static) GIF — callers fall back to LoadImage.
func (r *Renderer2D) LoadGIFFrames(data []byte) (frames []*Image, delays []int, err error) {
	return r.LoadGIFFramesScaled(data, 0)
}

// LoadGIFFramesScaled is LoadGIFFrames with LoadImageScaled's maxDim
// downscale applied to every composited frame.
func (r *Renderer2D) LoadGIFFramesScaled(data []byte, maxDim int) (frames []*Image, delays []int, err error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("render: decoding gif: %w", err)
	}
	if len(g.Image) < 2 {
		return nil, nil, fmt.Errorf("render: gif has %d frames (not animated)", len(g.Image))
	}
	w, h := g.Config.Width, g.Config.Height
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	// Pixels no frame covers show the GIF's declared background color (the
	// logical screen descriptor) — matching image-rs/Zed; a transparent
	// background index stays transparent.
	if p, ok := g.Config.ColorModel.(color.Palette); ok && int(g.BackgroundIndex) < len(p) {
		if _, _, _, a := p[g.BackgroundIndex].RGBA(); a > 0 {
			draw.Draw(canvas, canvas.Rect, image.NewUniform(p[g.BackgroundIndex]), image.Point{}, draw.Src)
		}
	}
	frames = make([]*Image, 0, len(g.Image))
	for i, src := range g.Image {
		fb := src.Bounds()
		// Snapshot the canvas before drawing for DisposalPrevious restore.
		var prev *image.NRGBA
		if i < len(g.Disposal) && g.Disposal[i] == gif.DisposalPrevious {
			prev = image.NewNRGBA(canvas.Rect)
			copy(prev.Pix, canvas.Pix)
		}
		draw.Draw(canvas, fb, src, fb.Min, draw.Over)
		// Upload a straight-alpha copy of the composited canvas (downscaled
		// when the GIF is much larger than its display box).
		scaled := canvas
		if maxDim > 0 {
			scaled = downscaleNRGBA(canvas, maxDim)
		}
		if scaled == canvas {
			cp := image.NewNRGBA(canvas.Rect)
			copy(cp.Pix, canvas.Pix)
			scaled = cp
		}
		frames = append(frames, r.NewImage(scaled.Rect.Dx(), scaled.Rect.Dy(), scaled.Pix))
		delays = append(delays, g.Delay[i])
		// Apply disposal so the next frame composites onto the right base.
		if i < len(g.Disposal) {
			switch g.Disposal[i] {
			case gif.DisposalBackground:
				draw.Draw(canvas, fb, image.Transparent, image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				if prev != nil {
					copy(canvas.Pix, prev.Pix)
				}
			}
		}
	}
	return frames, delays, nil
}

// DrawImageFit draws img into a w×h box at (x,y), scaled to fit while
// preserving aspect ratio (letterboxed), tinted by c (White for none).
func (r *Renderer2D) DrawImageFit(img *Image, x, y, w, h float32, c RGBA) {
	dx, dy, dw, dh, ok := fitBox(img, x, y, w, h)
	if !ok {
		return
	}
	r.DrawImage(img, dx, dy, dw, dh, c)
}

// DrawImageFitColorized is DrawImageFit through the colorize path — the
// source's luminance in c (see Renderer2D.DrawImageColorized).
func (r *Renderer2D) DrawImageFitColorized(img *Image, x, y, w, h float32, c RGBA) {
	dx, dy, dw, dh, ok := fitBox(img, x, y, w, h)
	if !ok {
		return
	}
	r.DrawImageColorized(img, dx, dy, dw, dh, c)
}

// DrawImageCropped fills the box with img, keeping its aspect ratio and
// letting the overflow be clipped — QML's Image.PreserveAspectCrop, which
// is how a photo covers its frame instead of sitting letterboxed in it.
func (r *Renderer2D) DrawImageCropped(img *Image, x, y, w, h float32, c RGBA) {
	if img.W == 0 || img.H == 0 || w <= 0 || h <= 0 {
		return
	}
	ar := float32(img.W) / float32(img.H)
	dw, dh := w, h
	if w/h > ar {
		dh = w / ar // wider box: match the width and overflow vertically
	} else {
		dw = h * ar
	}
	r.PushClip(x, y, w, h)
	r.DrawImage(img, x+(w-dw)/2, y+(h-dh)/2, dw, dh, c)
	r.PopClip()
}

// fitBox centres img's aspect ratio inside the box, the geometry both fit
// draws share.
func fitBox(img *Image, x, y, w, h float32) (float32, float32, float32, float32, bool) {
	if img.W == 0 || img.H == 0 {
		return 0, 0, 0, 0, false
	}
	ar := float32(img.W) / float32(img.H)
	dw, dh := w, h
	if w/h > ar {
		dw = h * ar
	} else {
		dh = w / ar
	}
	return x + (w-dw)/2, y + (h-dh)/2, dw, dh, true
}
