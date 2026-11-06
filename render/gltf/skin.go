package gltf

// glTF skins: a joint set (node indices) with inverse bind matrices, plus
// per-vertex JOINTS_0/WEIGHTS_0 attributes on skinned primitives. The
// render package combines these with the animated node worlds into joint
// palettes and deforms vertices (CPU skinning v1; compute skinning is the
// recorded upgrade path).

import (
	"encoding/binary"
	"fmt"

	"github.com/sushvibez/gfx/core"
)

// Skin is a joint set: Joints are node indices; InverseBind[i] carries mesh
// space into joint i's bind space (identity when the asset omits them).
type Skin struct {
	Joints      []int
	InverseBind []core.Mat4
}

// parseSkins reads the skins array.
func (p *parser) parseSkins() ([]Skin, error) {
	var out []Skin
	for si, js := range p.j.Skins {
		s := Skin{Joints: js.Joints}
		if js.InverseBindMatrices != nil {
			f, err := p.floats(*js.InverseBindMatrices, 16)
			if err != nil {
				return nil, fmt.Errorf("gltf: skin %d: %w", si, err)
			}
			if len(f) < len(js.Joints)*16 {
				return nil, fmt.Errorf("gltf: skin %d: %d bind matrices for %d joints", si, len(f)/16, len(js.Joints))
			}
			s.InverseBind = make([]core.Mat4, len(js.Joints))
			for j := range s.InverseBind {
				copy(s.InverseBind[j][:], f[j*16:(j+1)*16])
			}
		} else {
			s.InverseBind = make([]core.Mat4, len(js.Joints))
			for j := range s.InverseBind {
				s.InverseBind[j] = core.Mat4Identity()
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// joints4 reads a JOINTS_0 accessor (u8 or u16 VEC4) as joint index quads.
func (p *parser) joints4(i int) ([][4]uint16, error) {
	data, stride, count, ct, typ, err := p.accessor(i)
	if err != nil {
		return nil, err
	}
	if typ != "VEC4" {
		return nil, fmt.Errorf("gltf: JOINTS_0 accessor %d: want VEC4, got %s", i, typ)
	}
	out := make([][4]uint16, count)
	for e := range count {
		base := e * stride
		for c := 0; c < 4; c++ {
			switch ct {
			case compUint16:
				out[e][c] = binary.LittleEndian.Uint16(data[base+c*2:])
			case compUint8:
				out[e][c] = uint16(data[base+c])
			default:
				return nil, fmt.Errorf("gltf: JOINTS_0 accessor %d: component type %d unsupported", i, ct)
			}
		}
	}
	return out, nil
}

// weights4 reads a WEIGHTS_0 accessor (float VEC4), renormalizing each quad
// (exporters routinely leave small error).
func (p *parser) weights4(i int) ([][4]float32, error) {
	f, err := p.floats(i, 4)
	if err != nil {
		return nil, err
	}
	out := make([][4]float32, len(f)/4)
	for e := range out {
		w := [4]float32{f[e*4], f[e*4+1], f[e*4+2], f[e*4+3]}
		sum := w[0] + w[1] + w[2] + w[3]
		if sum > 0 {
			for c := range w {
				w[c] /= sum
			}
		}
		out[e] = w
	}
	return out, nil
}
