package pdf

// Font resources: what a Tf operator selects. A font tells the
// interpreter three things — how to turn string bytes into codes, how wide
// each code is (so the pen lands correctly), and which face to draw with.
//
// The face is the embedded font program when we can parse it (TrueType and
// OpenType/CFF both go through the renderer's own loader), otherwise the
// caller's fallback face. The widths always come from the PDF, so text
// lands where the document says even when the face substitutes.

import (
	"github.com/sushvibez/gfx/render"
)

// fontRes is a resolved font resource.
type fontRes struct {
	face     *render.Font // nil until a size is asked for
	ttf      []byte       // the embedded program, if we could use it
	widths   map[int]float32
	defWidth float32
	toRune   map[int]rune // code -> text, for drawing with any face
	twoByte  bool         // a Type0 font with a 2-byte encoding
	cidRange []cidRange   // Type0 mixed-width ranges
	vertical bool
	sizes    map[int]*render.Font
}

// cidRange is one /W entry: codes lo..hi are w wide.
type cidRange struct {
	lo, hi int
	w      float32
}

// SetFallbackFont sets the face used when a page's font is not embedded (or
// cannot be parsed). Without one, text is skipped rather than drawn in the
// wrong face.
func (rd *Renderer) SetFallbackFont(f *render.Font) { rd.fallback = f }

// font resolves a named font resource, caching by name and by the object it
// points at (a document shares one font across every page that uses it).
func (rd *Renderer) font(res Dict, name Name) *fontRes {
	obj := dictGet(rd.doc, res, "Font", name)
	key := string(name)
	if r, ok := obj.(Ref); ok {
		key = "obj:" + itoa(r.Num)
	}
	if f, ok := rd.fonts[key]; ok {
		return f
	}
	dict, _ := rd.doc.Resolve(obj).(Dict)
	f := rd.fontFromDict(dict, key)
	rd.fonts[key] = f
	return f
}

// fontFromDict builds a font resource from its dictionary.
func (rd *Renderer) fontFromDict(dict Dict, key string) *fontRes {
	f := &fontRes{widths: map[int]float32{}, defWidth: 500, sizes: map[int]*render.Font{}}
	if dict == nil {
		return f
	}
	sub, _ := rd.doc.Resolve(dict["Subtype"]).(Name)
	if sub == "Type0" {
		rd.readType0(f, dict)
	} else {
		rd.readSimple(f, dict)
	}
	if f.toRune == nil {
		f.toRune = map[int]rune{}
	}
	if cmap := rd.toUnicode(dict); cmap != nil {
		for code, r := range cmap { // an explicit ToUnicode wins
			f.toRune[code] = r
		}
	}
	return f
}

// readSimple reads a Type1 / TrueType / Type3 font: single-byte codes,
// /Widths indexed from /FirstChar, and an encoding that maps codes to
// glyph names (hence to runes).
func (rd *Renderer) readSimple(f *fontRes, dict Dict) {
	first := intOr(rd.doc, dict["FirstChar"], 0)
	if arr, ok := rd.doc.Resolve(dict["Widths"]).(Array); ok {
		for i, o := range arr {
			if w, ok := numOf(rd.doc.Resolve(o)); ok && w != 0 {
				f.widths[first+i] = float32(w)
			}
		}
	}
	base, _ := rd.doc.Resolve(dict["BaseFont"]).(Name)
	f.toRune = encodingMap(rd.doc, dict, base)
	desc, _ := rd.doc.Resolve(dict["FontDescriptor"]).(Dict)
	if desc != nil {
		if v, ok := numOf(rd.doc.Resolve(desc["MissingWidth"])); ok {
			f.defWidth = float32(v)
		}
		f.ttf = rd.embeddedFace(desc)
	}
	if len(f.widths) == 0 {
		// No /Widths: a standard-14 font, whose metrics the reader has to
		// know. Courier is fixed-pitch; the others use their ASCII table.
		f.widths, f.defWidth = standardWidths(base)
	}
}

// readType0 reads a composite font: multi-byte codes through a CMap, with
// widths from the descendant CIDFont's /W array.
func (rd *Renderer) readType0(f *fontRes, dict Dict) {
	f.twoByte = true // Identity-H and the predefined CMaps this reader sees
	enc, _ := rd.doc.Resolve(dict["Encoding"]).(Name)
	if enc == "Identity-V" || enc == "V" {
		f.vertical = true
	}
	desc0, _ := rd.doc.Resolve(dict["DescendantFonts"]).(Array)
	if len(desc0) == 0 {
		return
	}
	cid, _ := rd.doc.Resolve(desc0[0]).(Dict)
	if cid == nil {
		return
	}
	f.defWidth = 1000
	if v, ok := numOf(rd.doc.Resolve(cid["DW"])); ok {
		f.defWidth = float32(v)
	}
	// /W is a mix of "c [w1 w2 …]" and "c1 c2 w" forms.
	if arr, ok := rd.doc.Resolve(cid["W"]).(Array); ok {
		for i := 0; i < len(arr); {
			start := intOr(rd.doc, arr[i], 0)
			if i+1 >= len(arr) {
				break
			}
			switch next := rd.doc.Resolve(arr[i+1]).(type) {
			case Array:
				for j, o := range next {
					if w, ok := numOf(rd.doc.Resolve(o)); ok {
						f.widths[start+j] = float32(w)
					}
				}
				i += 2
			default:
				if i+2 >= len(arr) {
					i = len(arr)
					break
				}
				end := intOr(rd.doc, next, start)
				if w, ok := numOf(rd.doc.Resolve(arr[i+2])); ok {
					f.cidRange = append(f.cidRange, cidRange{lo: start, hi: end, w: float32(w)})
				}
				i += 3
			}
		}
	}
	if desc, ok := rd.doc.Resolve(cid["FontDescriptor"]).(Dict); ok {
		f.ttf = rd.embeddedFace(desc)
	}
}

// embeddedFace returns the bytes of an embedded font program our loader can
// use: TrueType (/FontFile2) or OpenType/CFF (/FontFile3 with subtype
// OpenType, or a bare CFF the loader accepts). A Type1 program (/FontFile)
// is not a format the loader reads, so it falls back.
func (rd *Renderer) embeddedFace(desc Dict) []byte {
	for _, key := range []Name{"FontFile2", "FontFile3"} {
		s, ok := rd.doc.Resolve(desc[key]).(*Stream)
		if !ok {
			continue
		}
		data, err := s.Decode()
		if err != nil || len(data) == 0 {
			continue
		}
		return data
	}
	return nil
}

// faceFor picks the face to draw one rune with: the embedded program when
// it has that glyph, else the fallback. A PDF's fonts are usually subsets
// whose own cmap covers only the glyphs the document uses — and often no
// cmap at all, since the PDF's encoding is what maps codes to glyphs — so
// the embedded face cannot be trusted to draw arbitrary text.
func (rd *Renderer) faceFor(f *fontRes, sizePx float32, r rune) *render.Font {
	face := rd.faceAt(f, sizePx)
	if face == nil {
		return nil
	}
	if len(f.ttf) > 0 && !face.Covers(r) {
		if rd.fallback != nil {
			return rd.fallback.AtSize(float64(sizePx))
		}
	}
	return face
}

// faceAt returns the face to draw this font with at a device pixel size.
func (rd *Renderer) faceAt(f *fontRes, sizePx float32) *render.Font {
	if f == nil || sizePx < 1 {
		return nil
	}
	key := int(sizePx*4 + 0.5) // quarter-pixel buckets: enough for text
	if face, ok := f.sizes[key]; ok {
		return face
	}
	size := float64(sizePx)
	var face *render.Font
	if len(f.ttf) > 0 {
		if built, err := rd.r.NewFont(f.ttf, size); err == nil {
			face = built
		} else {
			f.ttf = nil // do not retry a program the loader rejected
		}
	}
	if face == nil && rd.fallback != nil {
		face = rd.fallback.AtSize(size)
	}
	f.sizes[key] = face
	return face
}

// widthOf is a code's advance in 1/1000 em.
func (f *fontRes) widthOf(code int) float32 {
	if w, ok := f.widths[code]; ok {
		return w
	}
	for _, r := range f.cidRange {
		if code >= r.lo && code <= r.hi {
			return r.w
		}
	}
	return f.defWidth
}

// codes splits a string into character codes: two bytes at a time for a
// composite font, one for a simple one.
func (f *fontRes) codes(s String) []int {
	if f != nil && f.twoByte {
		out := make([]int, 0, len(s)/2)
		for i := 0; i+1 < len(s); i += 2 {
			out = append(out, int(s[i])<<8|int(s[i+1]))
		}
		if len(s)%2 == 1 { // a stray trailing byte: keep it rather than drop it
			out = append(out, int(s[len(s)-1]))
		}
		return out
	}
	out := make([]int, len(s))
	for i, b := range s {
		out[i] = int(b)
	}
	return out
}

// runeOf is the text a code stands for (0 when unknown).
func (f *fontRes) runeOf(code int) rune {
	if f == nil {
		return 0
	}
	if r, ok := f.toRune[code]; ok {
		return r
	}
	if !f.twoByte && code >= 32 && code < 127 {
		return rune(code) // an unencoded simple font is ASCII often enough
	}
	return 0
}
