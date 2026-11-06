package pdf

// Colour spaces. Device spaces convert directly; an Indexed space looks
// its components up in a palette; ICCBased and CalRGB/CalGray behave as
// their device equivalents (which is what viewers do without a colour
// engine); a Separation or DeviceN runs its tint transform if it is a
// function we evaluate, else it darkens like ink.

import "github.com/sushvibez/gfx/render"

// colorSpace converts component values to a colour.
type colorSpace struct {
	kind    Name
	n       int    // component count
	palette []byte // Indexed: RGB triples
	base    *colorSpace
	hival   int
	fn      *function // Separation/DeviceN tint transform
}

// initial is the colour a space starts at when it is selected (black, or
// the first palette entry).
func (cs *colorSpace) initial() render.RGBA {
	if cs == nil {
		return render.RGBA{A: 1}
	}
	switch cs.kind {
	case "Indexed":
		if c, ok := cs.color([]Object{int64(0)}); ok {
			return c
		}
	}
	return render.RGBA{A: 1}
}

// color converts the operands of an sc/scn operator.
func (cs *colorSpace) color(args []Object) (render.RGBA, bool) {
	if cs == nil {
		// No space selected: PDF says the operands are device values, and
		// their count says which space.
		return byComponentCount(args)
	}
	vals := make([]float32, 0, len(args))
	for _, a := range args {
		if v, ok := numOf(a); ok {
			vals = append(vals, float32(v))
		}
	}
	switch cs.kind {
	case "DeviceGray", "CalGray", "G":
		if len(vals) >= 1 {
			g := clamp01f(vals[len(vals)-1])
			return render.RGBA{R: g, G: g, B: g, A: 1}, true
		}
	case "DeviceRGB", "CalRGB", "Lab", "RGB":
		if len(vals) >= 3 {
			v := vals[len(vals)-3:]
			return render.RGBA{R: clamp01f(v[0]), G: clamp01f(v[1]), B: clamp01f(v[2]), A: 1}, true
		}
	case "DeviceCMYK", "CMYK":
		if len(vals) >= 4 {
			v := vals[len(vals)-4:]
			return cmyk(v[0], v[1], v[2], v[3]), true
		}
	case "ICCBased":
		switch {
		case cs.n >= 4 && len(vals) >= 4:
			v := vals[len(vals)-4:]
			return cmyk(v[0], v[1], v[2], v[3]), true
		case cs.n == 3 && len(vals) >= 3:
			v := vals[len(vals)-3:]
			return render.RGBA{R: clamp01f(v[0]), G: clamp01f(v[1]), B: clamp01f(v[2]), A: 1}, true
		case len(vals) >= 1:
			g := clamp01f(vals[len(vals)-1])
			return render.RGBA{R: g, G: g, B: g, A: 1}, true
		}
	case "Indexed":
		if len(vals) >= 1 {
			return cs.indexed(int(vals[len(vals)-1])), true
		}
	case "Separation", "DeviceN":
		if len(vals) >= 1 {
			if cs.fn != nil && cs.base != nil {
				if out := cs.fn.eval(vals); len(out) > 0 {
					objs := make([]Object, len(out))
					for i, v := range out {
						objs[i] = float64(v)
					}
					if c, ok := cs.base.color(objs); ok {
						return c, true
					}
				}
			}
			// A tint of 1 is full ink, which is black on paper.
			t := clamp01f(vals[len(vals)-1])
			g := 1 - t
			return render.RGBA{R: g, G: g, B: g, A: 1}, true
		}
	case "Pattern":
		// A pattern's own colour is unknowable without running it; mid grey
		// reads as "something is here" rather than a hole.
		return render.RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1}, true
	}
	return byComponentCount(args)
}

// indexed looks a palette entry up.
func (cs *colorSpace) indexed(i int) render.RGBA {
	base := cs.base
	comps := 3
	if base != nil && base.n > 0 {
		comps = base.n
	}
	if i < 0 {
		i = 0
	}
	off := i * comps
	if off+comps > len(cs.palette) {
		return render.RGBA{A: 1}
	}
	vals := make([]Object, comps)
	for j := 0; j < comps; j++ {
		vals[j] = float64(cs.palette[off+j]) / 255
	}
	if base != nil {
		if c, ok := base.color(vals); ok {
			return c
		}
	}
	c, _ := byComponentCount(vals)
	return c
}

// byComponentCount reads device colour operands when no space is known.
func byComponentCount(args []Object) (render.RGBA, bool) {
	vals := make([]float32, 0, len(args))
	for _, a := range args {
		if v, ok := numOf(a); ok {
			vals = append(vals, clamp01f(float32(v)))
		}
	}
	switch len(vals) {
	case 1:
		return render.RGBA{R: vals[0], G: vals[0], B: vals[0], A: 1}, true
	case 3:
		return render.RGBA{R: vals[0], G: vals[1], B: vals[2], A: 1}, true
	case 4:
		return cmyk(vals[0], vals[1], vals[2], vals[3]), true
	}
	return render.RGBA{A: 1}, false
}

// colorSpace resolves a named or inline colour space.
func (rd *Renderer) colorSpace(res Dict, name Name) *colorSpace {
	switch name {
	case "DeviceGray", "DeviceRGB", "DeviceCMYK", "Pattern", "G", "RGB", "CMYK":
		return &colorSpace{kind: name, n: componentsOf(name)}
	}
	return rd.colorSpaceObj(rd.doc.Resolve(dictGet(rd.doc, res, "ColorSpace", name)), res, 0)
}

// colorSpaceObj resolves a colour space object (a name or an array).
func (rd *Renderer) colorSpaceObj(o Object, res Dict, depth int) *colorSpace {
	if depth > 8 {
		return nil
	}
	switch v := rd.doc.Resolve(o).(type) {
	case Name:
		switch v {
		case "DeviceGray", "DeviceRGB", "DeviceCMYK", "Pattern", "G", "RGB", "CMYK":
			return &colorSpace{kind: v, n: componentsOf(v)}
		}
		// A named space defined in the resources.
		if next := dictGet(rd.doc, res, "ColorSpace", v); next != nil {
			return rd.colorSpaceObj(next, res, depth+1)
		}
		return nil
	case Array:
		if len(v) == 0 {
			return nil
		}
		kind, _ := rd.doc.Resolve(v[0]).(Name)
		cs := &colorSpace{kind: kind, n: componentsOf(kind)}
		switch kind {
		case "ICCBased":
			if s, ok := rd.doc.Resolve(v[1]).(*Stream); ok && len(v) > 1 {
				cs.n = intOr(rd.doc, s.Dict["N"], 3)
			}
		case "Indexed", "I":
			cs.kind = "Indexed"
			if len(v) >= 4 {
				cs.base = rd.colorSpaceObj(v[1], res, depth+1)
				cs.hival = intOr(rd.doc, v[2], 0)
				switch p := rd.doc.Resolve(v[3]).(type) {
				case String:
					cs.palette = []byte(p)
				case *Stream:
					if b, err := p.Decode(); err == nil {
						cs.palette = b
					}
				}
			}
			cs.n = 1
		case "Separation":
			cs.n = 1
			if len(v) >= 4 {
				cs.base = rd.colorSpaceObj(v[2], res, depth+1)
				cs.fn = rd.function(v[3])
			}
		case "DeviceN":
			if names, ok := rd.doc.Resolve(v[1]).(Array); ok {
				cs.n = len(names)
			}
			if len(v) >= 4 {
				cs.base = rd.colorSpaceObj(v[2], res, depth+1)
				cs.fn = rd.function(v[3])
			}
		case "CalRGB", "Lab":
			cs.n = 3
		case "CalGray":
			cs.n = 1
		case "Pattern":
			cs.n = 1
		}
		return cs
	}
	return nil
}

func componentsOf(n Name) int {
	switch n {
	case "DeviceGray", "CalGray", "G":
		return 1
	case "DeviceCMYK", "CMYK":
		return 4
	}
	return 3
}

// shadingColor is a representative colour for a shading: the midpoint of
// its function, which is what a flat fill of a gradient should look like.
func (rd *Renderer) shadingColor(res Dict, name Name) (render.RGBA, bool) {
	sh := rd.doc.Resolve(dictGet(rd.doc, res, "Shading", name))
	var dict Dict
	switch v := sh.(type) {
	case Dict:
		dict = v
	case *Stream:
		dict = v.Dict
	default:
		return render.RGBA{}, false
	}
	cs := rd.colorSpaceObj(dict["ColorSpace"], res, 0)
	fn := rd.function(dict["Function"])
	if fn == nil {
		return render.RGBA{}, false
	}
	out := fn.eval([]float32{0.5})
	objs := make([]Object, len(out))
	for i, v := range out {
		objs[i] = float64(v)
	}
	if cs != nil {
		return cs.color(objs)
	}
	return byComponentCount(objs)
}
