package pdf

// Images: an image XObject (or an inline image) becomes an RGBA buffer the
// renderer uploads once and draws through the current transform. JPEG and
// PNG-style raw samples both land here; JPX (JPEG 2000) has no decoder in
// the standard library, so those draw as their average colour rather than
// as a hole.

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/jpeg" // DCTDecode
	_ "image/png"  // some producers embed PNG as raw sample data

	"github.com/sushvibez/gfx/render"
)

// drawImageStream draws an image XObject under the current transform. The
// unit square of user space maps to the whole image, which is what the
// `cm` before a `Do` sets up.
func (rd *Renderer) drawImageStream(s *Stream, st gstate) {
	if s == nil {
		return
	}
	w := intOr(rd.doc, s.Dict["Width"], 0)
	h := intOr(rd.doc, s.Dict["Height"], 0)
	if w <= 0 || h <= 0 {
		return
	}
	// Where the unit square lands.
	box, ok := quadBounds(st.ctm, 0, 0, 1, 1)
	if !ok {
		return
	}
	box = intersect(box, st.clip)
	if box.W < 0.5 || box.H < 0.5 {
		return // scaled to nothing, or entirely clipped away
	}
	if isMask(rd.doc, s) {
		// A stencil mask paints the fill colour through its 1-bit shape.
		// Painting the whole box in the fill colour is the mask at its
		// coarsest; a real one needs per-pixel alpha, which is what the
		// RGBA path below does when the mask decodes.
		if img := rd.maskImage(s, st.fill, w, h); img != nil {
			rd.drawImage(img, box, st)
			return
		}
		return
	}
	img := rd.image(s, w, h)
	if img == nil {
		return
	}
	rd.drawImage(img, box, st)
}

// drawImage draws a decoded image into a device rect, respecting the clip
// and the fill alpha.
func (rd *Renderer) drawImage(img *render.Image, box Rect, st gstate) {
	tint := render.RGBA{R: 1, G: 1, B: 1, A: st.fillAlpha}
	rd.clippedBy(st, func() { rd.r.DrawImage(img, box.X, box.Y, box.W, box.H, tint) })
}

// isMask reports an /ImageMask stencil.
func isMask(doc *Document, s *Stream) bool {
	if b, ok := doc.Resolve(s.Dict["ImageMask"]).(bool); ok {
		return b
	}
	return false
}

// image decodes an image stream into a renderer image, cached by the
// stream's identity so a repeated logo uploads once.
func (rd *Renderer) image(s *Stream, w, h int) *render.Image {
	key := imageKey(s, w, h)
	if img, ok := rd.imgs[key]; ok {
		return img
	}
	rgba := rd.decodeImage(s, w, h)
	var img *render.Image
	if rgba != nil {
		img = rd.r.NewImage(w, h, rgba)
	}
	rd.imgs[key] = img
	return img
}

// imageKey identifies an image stream: its size plus a sample of its bytes
// (a full hash of a 10MB scan would cost more than the decode).
func imageKey(s *Stream, w, h int) string {
	b := s.Raw
	var head, tail []byte
	if len(b) > 64 {
		head, tail = b[:32], b[len(b)-32:]
	} else {
		head = b
	}
	return itoa(w) + "x" + itoa(h) + ":" + itoa(len(b)) + ":" + string(head) + string(tail)
}

// decodeImage turns an image stream into RGBA samples.
func (rd *Renderer) decodeImage(s *Stream, w, h int) []byte {
	data, codec, err := s.DecodeImage()
	if err != nil {
		return nil
	}
	switch codec {
	case "DCTDecode":
		return rd.decodeStdImage(data, w, h, s)
	case "JPXDecode":
		// No JPEG 2000 decoder in the standard library: a flat mid-grey
		// keeps the page's composition rather than leaving a hole.
		return flatRGBA(w, h, render.RGBA{R: 0.6, G: 0.6, B: 0.6, A: 1})
	}
	if isPNG(data) || isJPEG(data) {
		return rd.decodeStdImage(data, w, h, s)
	}
	return rd.decodeRaw(s, data, w, h)
}

// decodeStdImage decodes a JPEG or PNG through the standard library and
// converts it to RGBA at the size the PDF declares.
func (rd *Renderer) decodeStdImage(data []byte, w, h int, s *Stream) []byte {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	if sb.Dx() == w && sb.Dy() == h {
		draw.Draw(dst, dst.Bounds(), src, sb.Min, draw.Src)
	} else {
		// Sizes disagree (a subsampled JPEG, a wrong /Width): sample.
		for y := 0; y < h; y++ {
			sy := sb.Min.Y + y*sb.Dy()/h
			for x := 0; x < w; x++ {
				sx := sb.Min.X + x*sb.Dx()/w
				dst.Set(x, y, src.At(sx, sy))
			}
		}
	}
	if a := rd.softMask(s, w, h); a != nil {
		applyAlpha(dst.Pix, a)
	}
	return dst.Pix
}

// decodeRaw expands raw sample data (the /FlateDecode case) using the
// image's colour space and bit depth.
func (rd *Renderer) decodeRaw(s *Stream, data []byte, w, h int) []byte {
	bpc := intOr(rd.doc, s.Dict["BitsPerComponent"], 8)
	cs := rd.colorSpaceObj(s.Dict["ColorSpace"], nil, 0)
	comps := 1
	if cs != nil && cs.n > 0 {
		comps = cs.n
	}
	if cs != nil && cs.kind == "Indexed" {
		comps = 1
	}
	rowBytes := (w*comps*bpc + 7) / 8
	if rowBytes == 0 {
		return nil
	}
	out := make([]byte, w*h*4)
	maxVal := float32(int(1)<<bpc - 1)
	if maxVal == 0 {
		maxVal = 1
	}
	vals := make([]Object, comps)
	for y := 0; y < h; y++ {
		row := y * rowBytes
		for x := 0; x < w; x++ {
			for c := 0; c < comps; c++ {
				idx := x*comps + c
				v := sampleBits(data, row, idx, bpc)
				if cs != nil && cs.kind == "Indexed" {
					vals[c] = float64(v) // a palette index, not a fraction
				} else {
					vals[c] = float64(float32(v) / maxVal)
				}
			}
			var col render.RGBA
			if cs != nil {
				col, _ = cs.color(vals)
			} else {
				col, _ = byComponentCount(vals)
			}
			o := (y*w + x) * 4
			out[o] = byte(col.R*255 + 0.5)
			out[o+1] = byte(col.G*255 + 0.5)
			out[o+2] = byte(col.B*255 + 0.5)
			out[o+3] = 255
		}
	}
	if a := rd.softMask(s, w, h); a != nil {
		applyAlpha(out, a)
	}
	return out
}

// sampleBits reads component number idx of a row at the given bit depth.
func sampleBits(data []byte, rowStart, idx, bpc int) int {
	switch bpc {
	case 8:
		if i := rowStart + idx; i < len(data) {
			return int(data[i])
		}
	case 16:
		if i := rowStart + idx*2; i+1 < len(data) {
			return int(data[i])<<8 | int(data[i+1])
		}
	case 1, 2, 4:
		bit := idx * bpc
		i := rowStart + bit/8
		if i < len(data) {
			shift := 8 - bpc - bit%8
			return int(data[i]>>shift) & (1<<bpc - 1)
		}
	}
	return 0
}

// maskImage builds an RGBA image for a 1-bit stencil mask: the fill colour
// where the mask paints, transparent elsewhere.
func (rd *Renderer) maskImage(s *Stream, fill render.RGBA, w, h int) *render.Image {
	key := "mask:" + imageKey(s, w, h) + colorKey(fill)
	if img, ok := rd.imgs[key]; ok {
		return img
	}
	data, codec, err := s.DecodeImage()
	if err != nil || codec != "" {
		rd.imgs[key] = nil
		return nil
	}
	// /Decode [1 0] inverts which bit paints.
	invert := false
	if dec, ok := rd.doc.Resolve(s.Dict["Decode"]).(Array); ok && len(dec) >= 1 {
		if v, ok := numOf(rd.doc.Resolve(dec[0])); ok && v == 1 {
			invert = true
		}
	}
	rowBytes := (w + 7) / 8
	out := make([]byte, w*h*4)
	r8, g8, b8 := byte(fill.R*255+0.5), byte(fill.G*255+0.5), byte(fill.B*255+0.5)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*rowBytes + x/8
			bit := byte(0)
			if i < len(data) {
				bit = data[i] >> (7 - uint(x%8)) & 1
			}
			paint := bit == 0 // sample 0 paints, unless /Decode says otherwise
			if invert {
				paint = !paint
			}
			o := (y*w + x) * 4
			if paint {
				out[o], out[o+1], out[o+2], out[o+3] = r8, g8, b8, byte(fill.A*255+0.5)
			}
		}
	}
	img := rd.r.NewImage(w, h, out)
	rd.imgs[key] = img
	return img
}

// softMask reads an /SMask's grey samples as an alpha channel at the
// image's size (nil when there is none we can use).
func (rd *Renderer) softMask(s *Stream, w, h int) []byte {
	ms, ok := rd.doc.Resolve(s.Dict["SMask"]).(*Stream)
	if !ok {
		return nil
	}
	mw := intOr(rd.doc, ms.Dict["Width"], 0)
	mh := intOr(rd.doc, ms.Dict["Height"], 0)
	if mw <= 0 || mh <= 0 {
		return nil
	}
	data, codec, err := ms.DecodeImage()
	if err != nil {
		return nil
	}
	bpc := intOr(rd.doc, ms.Dict["BitsPerComponent"], 8)
	if codec != "" { // a JPEG-coded mask: decode it as an image first
		src, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		out := make([]byte, w*h)
		sb := src.Bounds()
		for y := 0; y < h; y++ {
			sy := sb.Min.Y + y*sb.Dy()/h
			for x := 0; x < w; x++ {
				sx := sb.Min.X + x*sb.Dx()/w
				r, _, _, _ := src.At(sx, sy).RGBA()
				out[y*w+x] = byte(r >> 8)
			}
		}
		return out
	}
	rowBytes := (mw*bpc + 7) / 8
	maxVal := float32(int(1)<<bpc - 1)
	if maxVal == 0 {
		maxVal = 1
	}
	out := make([]byte, w*h)
	for y := 0; y < h; y++ {
		sy := y * mh / h
		for x := 0; x < w; x++ {
			sx := x * mw / w
			v := sampleBits(data, sy*rowBytes, sx, bpc)
			out[y*w+x] = byte(float32(v) / maxVal * 255)
		}
	}
	return out
}

// applyAlpha multiplies an RGBA buffer by an alpha channel.
func applyAlpha(pix, alpha []byte) {
	n := min(len(pix)/4, len(alpha))
	for i := 0; i < n; i++ {
		a := alpha[i]
		o := i * 4
		pix[o] = byte(int(pix[o]) * int(a) / 255)
		pix[o+1] = byte(int(pix[o+1]) * int(a) / 255)
		pix[o+2] = byte(int(pix[o+2]) * int(a) / 255)
		pix[o+3] = a
	}
}

// flatRGBA is a solid buffer, for the formats we cannot decode.
func flatRGBA(w, h int, c render.RGBA) []byte {
	out := make([]byte, w*h*4)
	r8, g8, b8 := byte(c.R*255+0.5), byte(c.G*255+0.5), byte(c.B*255+0.5)
	for i := 0; i < w*h; i++ {
		o := i * 4
		out[o], out[o+1], out[o+2], out[o+3] = r8, g8, b8, 255
	}
	return out
}

func isPNG(b []byte) bool {
	return len(b) > 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G'
}

func isJPEG(b []byte) bool { return len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 }

func colorKey(c render.RGBA) string {
	return ":" + itoa(int(c.R*255)) + "," + itoa(int(c.G*255)) + "," + itoa(int(c.B*255))
}
