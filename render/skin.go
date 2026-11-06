package render

// Joint palettes from the animated node worlds, and the CPU skinner that
// applies them to bind-pose vertices.
//
// jointPalette is shared by both skinning paths. skinVertices — deforming
// on the CPU and re-uploading the vertex buffer — is now the FALLBACK path
// (render.GPUSkinning = false, or GFX_CPU_SKIN=1); by default the same
// palette goes to the GPU and the vertex shader does the blending, see
// gpuskin.go. It is kept because it is the reference implementation the
// GPU path is tested against, and the only one that needs no second
// pipeline family.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render/gltf"
)

// jointPalette builds skinning matrices: world(joint_j) * inverseBind[j].
// Palette matrices carry mesh bind space into posed world space, so skinned
// items draw with an identity model transform.
func jointPalette(skin *gltf.Skin, worlds []core.Mat4, dst []core.Mat4) []core.Mat4 {
	dst = dst[:0]
	for j, node := range skin.Joints {
		w := core.Mat4Identity()
		if node >= 0 && node < len(worlds) {
			w = worlds[node]
		}
		dst = append(dst, w.Mul(skin.InverseBind[j]))
	}
	return dst
}

// skinMat is a joint matrix packed to its 3x4 affine part (column-major,
// three basis columns then the translation). Skinning never needs the
// bottom row, and 48 bytes per joint keeps the hot palette in cache.
type skinMat [12]float32

// packPalette flattens a joint palette into skinMats, reusing dst.
func packPalette(dst []skinMat, palette []core.Mat4) []skinMat {
	if cap(dst) < len(palette) {
		dst = make([]skinMat, len(palette))
	}
	dst = dst[:len(palette)]
	for j := range palette {
		m := &palette[j]
		dst[j] = skinMat{
			m[0], m[1], m[2],
			m[4], m[5], m[6],
			m[8], m[9], m[10],
			m[12], m[13], m[14],
		}
	}
	return dst
}

// skinVertices deforms base (bind pose) into dst by the palette: each
// vertex blends its four joint MATRICES by weight and applies the result
// once (linear blend skinning), rather than transforming the position four
// times — the same result for the affine joint matrices a rig produces, at
// roughly a third of the arithmetic. Normals use the blended matrix's
// rotation (exact for rigid/uniform-scale joints).
//
// pack is reusable scratch for the packed palette (pass the previous return
// value); the routine allocates nothing once it has grown.
func skinVertices(dst, base []Vertex3D, joints [][4]uint16, weights [][4]float32,
	palette []core.Mat4, pack []skinMat) []skinMat {
	pack = packPalette(pack, palette)
	n := len(base)
	if len(dst) < n || len(joints) < n || len(weights) < n {
		return pack
	}
	np := len(pack)
	for i := 0; i < n; i++ {
		w := &weights[i]
		j := &joints[i]
		var m skinMat
		for c := 0; c < 4; c++ {
			wc := w[c]
			if wc == 0 {
				continue
			}
			jc := int(j[c])
			if jc >= np {
				continue
			}
			p := &pack[jc]
			m[0] += p[0] * wc
			m[1] += p[1] * wc
			m[2] += p[2] * wc
			m[3] += p[3] * wc
			m[4] += p[4] * wc
			m[5] += p[5] * wc
			m[6] += p[6] * wc
			m[7] += p[7] * wc
			m[8] += p[8] * wc
			m[9] += p[9] * wc
			m[10] += p[10] * wc
			m[11] += p[11] * wc
		}
		v := &base[i]
		px, py, pz := v.Pos.X, v.Pos.Y, v.Pos.Z
		nx, ny, nz := v.Normal.X, v.Normal.Y, v.Normal.Z
		d := &dst[i]
		d.U, d.V = v.U, v.V
		d.Pos = core.Vec3{
			X: m[0]*px + m[3]*py + m[6]*pz + m[9],
			Y: m[1]*px + m[4]*py + m[7]*pz + m[10],
			Z: m[2]*px + m[5]*py + m[8]*pz + m[11],
		}
		tx := m[0]*nx + m[3]*ny + m[6]*nz
		ty := m[1]*nx + m[4]*ny + m[7]*nz
		tz := m[2]*nx + m[5]*ny + m[8]*nz
		if l := math32.Sqrt(tx*tx + ty*ty + tz*tz); l > 0 {
			inv := 1 / l
			tx, ty, tz = tx*inv, ty*inv, tz*inv
		}
		d.Normal = core.Vec3{X: tx, Y: ty, Z: tz}
	}
	return pack
}
