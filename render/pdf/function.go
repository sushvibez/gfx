package pdf

// PDF functions, the four kinds that turn a tint or a shading parameter
// into colour components: sampled (0), exponential (2), stitching (3),
// PostScript calculator (4), and an array of functions (one per output).

// function evaluates a PDF function.
type function struct {
	kind    int
	domain  []float32
	rng     []float32
	c0, c1  []float32 // type 2
	n       float32   // type 2 exponent
	funcs   []*function
	bounds  []float32 // type 3
	encode  []float32
	samples []byte // type 0
	size    []int
	bps     int
	outputs int
	prog    []psTok // type 4
}

// function builds a function from its object (a dict, a stream, or an
// array of functions — one per output component).
func (rd *Renderer) function(o Object) *function {
	switch v := rd.doc.Resolve(o).(type) {
	case Array:
		f := &function{kind: -1}
		for _, el := range v {
			if sub := rd.function(el); sub != nil {
				f.funcs = append(f.funcs, sub)
			}
		}
		if len(f.funcs) == 0 {
			return nil
		}
		return f
	case Dict:
		return rd.functionFrom(v, nil)
	case *Stream:
		data, err := v.Decode()
		if err != nil {
			data = nil
		}
		return rd.functionFrom(v.Dict, data)
	}
	return nil
}

func (rd *Renderer) functionFrom(d Dict, data []byte) *function {
	f := &function{kind: intOr(rd.doc, d["FunctionType"], -1)}
	f.domain = rd.floats(d["Domain"])
	f.rng = rd.floats(d["Range"])
	switch f.kind {
	case 0:
		f.samples = data
		for _, v := range rd.floats(d["Size"]) {
			f.size = append(f.size, int(v))
		}
		f.bps = intOr(rd.doc, d["BitsPerSample"], 8)
		f.encode = rd.floats(d["Encode"])
		f.outputs = len(f.rng) / 2
	case 2:
		f.c0 = rd.floats(d["C0"])
		f.c1 = rd.floats(d["C1"])
		if len(f.c0) == 0 {
			f.c0 = []float32{0}
		}
		if len(f.c1) == 0 {
			f.c1 = []float32{1}
		}
		f.n = 1
		if v, ok := numOf(rd.doc.Resolve(d["N"])); ok {
			f.n = float32(v)
		}
	case 3:
		if arr, ok := rd.doc.Resolve(d["Functions"]).(Array); ok {
			for _, el := range arr {
				f.funcs = append(f.funcs, rd.function(el))
			}
		}
		f.bounds = rd.floats(d["Bounds"])
		f.encode = rd.floats(d["Encode"])
	case 4:
		// A calculator function: a small PostScript program in the stream.
		f.outputs = len(f.rng) / 2
		f.prog = parsePostScript(data)
		if len(f.prog) == 0 {
			return nil
		}
	default:
		return nil
	}
	return f
}

// floats reads a numeric array.
func (rd *Renderer) floats(o Object) []float32 {
	arr, _ := rd.doc.Resolve(o).(Array)
	out := make([]float32, 0, len(arr))
	for _, el := range arr {
		out = append(out, floatOr(rd.doc, el, 0))
	}
	return out
}

// eval evaluates the function for the given inputs.
func (f *function) eval(in []float32) []float32 {
	if f == nil || len(in) == 0 {
		return nil
	}
	x := in[0]
	if len(f.domain) >= 2 {
		x = clampf(x, f.domain[0], f.domain[1])
	}
	switch f.kind {
	case -1: // an array of functions, one output each
		out := make([]float32, 0, len(f.funcs))
		for _, sub := range f.funcs {
			v := sub.eval(in)
			if len(v) == 0 {
				return nil
			}
			out = append(out, v[0])
		}
		return out
	case 2:
		n := len(f.c0)
		if len(f.c1) < n {
			n = len(f.c1)
		}
		out := make([]float32, n)
		t := powf(x, f.n)
		for i := 0; i < n; i++ {
			out[i] = f.c0[i] + t*(f.c1[i]-f.c0[i])
		}
		return out
	case 3:
		// Stitching: pick the sub-function whose sub-domain holds x, then
		// map x into that function's encode range.
		lo, hi := float32(0), float32(1)
		if len(f.domain) >= 2 {
			lo, hi = f.domain[0], f.domain[1]
		}
		i := 0
		for i < len(f.bounds) && x >= f.bounds[i] {
			i++
		}
		if i >= len(f.funcs) {
			i = len(f.funcs) - 1
		}
		if i < 0 || len(f.funcs) == 0 {
			return nil
		}
		subLo, subHi := lo, hi
		if i > 0 {
			subLo = f.bounds[i-1]
		}
		if i < len(f.bounds) {
			subHi = f.bounds[i]
		}
		e0, e1 := float32(0), float32(1)
		if len(f.encode) >= 2*i+2 {
			e0, e1 = f.encode[2*i], f.encode[2*i+1]
		}
		return f.funcs[i].eval([]float32{interp(x, subLo, subHi, e0, e1)})
	case 0:
		return f.sample(x)
	case 4:
		return f.calculate(in)
	}
	return nil
}

// calculate runs a type-4 program: every input is clipped to its own
// domain, and every output to its range (the spec requires both).
func (f *function) calculate(in []float32) []float32 {
	args := make([]float64, 0, len(in))
	for i, v := range in {
		if len(f.domain) >= 2*i+2 {
			v = clampf(v, f.domain[2*i], f.domain[2*i+1])
		}
		args = append(args, float64(v))
	}
	raw := runPostScript(f.prog, args, f.outputs)
	if len(raw) == 0 {
		return nil
	}
	out := make([]float32, len(raw))
	for i, v := range raw {
		out[i] = float32(v)
		if len(f.rng) >= 2*i+2 {
			out[i] = clampf(out[i], f.rng[2*i], f.rng[2*i+1])
		}
	}
	return out
}

// sample reads a type-0 (sampled) function at x, with linear interpolation
// between the two nearest samples of the first input dimension.
func (f *function) sample(x float32) []float32 {
	if len(f.size) == 0 || f.outputs == 0 || len(f.samples) == 0 {
		return nil
	}
	size := f.size[0]
	if size < 1 {
		return nil
	}
	lo, hi := float32(0), float32(1)
	if len(f.domain) >= 2 {
		lo, hi = f.domain[0], f.domain[1]
	}
	e0, e1 := float32(0), float32(size-1)
	if len(f.encode) >= 2 {
		e0, e1 = f.encode[0], f.encode[1]
	}
	pos := clampf(interp(x, lo, hi, e0, e1), 0, float32(size-1))
	i0 := int(pos)
	i1 := min(i0+1, size-1)
	frac := pos - float32(i0)
	out := make([]float32, f.outputs)
	for j := 0; j < f.outputs; j++ {
		v0 := f.sampleAt(i0*f.outputs + j)
		v1 := f.sampleAt(i1*f.outputs + j)
		v := v0 + frac*(v1-v0)
		if len(f.rng) >= 2*j+2 {
			v = f.rng[2*j] + v*(f.rng[2*j+1]-f.rng[2*j])
		}
		out[j] = v
	}
	return out
}

// sampleAt reads sample number i as a 0..1 value, at whatever bit depth
// the function declares.
func (f *function) sampleAt(i int) float32 {
	switch f.bps {
	case 8:
		if i < len(f.samples) {
			return float32(f.samples[i]) / 255
		}
	case 16:
		if 2*i+1 < len(f.samples) {
			return float32(uint16(f.samples[2*i])<<8|uint16(f.samples[2*i+1])) / 65535
		}
	case 1, 2, 4:
		bit := i * f.bps
		byteIdx := bit / 8
		if byteIdx < len(f.samples) {
			shift := 8 - f.bps - bit%8
			maskv := byte(1<<f.bps - 1)
			v := f.samples[byteIdx] >> shift & maskv
			return float32(v) / float32(maskv)
		}
	case 32:
		if 4*i+3 < len(f.samples) {
			v := uint32(f.samples[4*i])<<24 | uint32(f.samples[4*i+1])<<16 |
				uint32(f.samples[4*i+2])<<8 | uint32(f.samples[4*i+3])
			return float32(float64(v) / 4294967295)
		}
	}
	return 0
}

func interp(x, xmin, xmax, ymin, ymax float32) float32 {
	if xmax == xmin {
		return ymin
	}
	return ymin + (x-xmin)*(ymax-ymin)/(xmax-xmin)
}

func clampf(v, lo, hi float32) float32 {
	if lo > hi {
		lo, hi = hi, lo
	}
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

// powf is x^n for the small positive exponents PDF functions use.
func powf(x, n float32) float32 {
	switch {
	case n == 1 || x <= 0:
		return x
	case n == 2:
		return x * x
	case n == 3:
		return x * x * x
	}
	// A general exponent: repeated multiplication for the integer part,
	// then a square-root refinement for a half, which covers N: 0.5.
	out := float32(1)
	i := int(n)
	for k := 0; k < i; k++ {
		out *= x
	}
	if frac := n - float32(i); frac > 0 {
		out *= sqrtf(x)
	}
	return out
}

// sqrtf is a few Newton steps — enough for a colour component.
func sqrtf(x float32) float32 {
	if x <= 0 {
		return 0
	}
	g := x
	for i := 0; i < 12; i++ {
		g = 0.5 * (g + x/g)
	}
	return g
}
