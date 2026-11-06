//go:build !js

package render

import (
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/gpu"
)

// A quad in the XY plane with standard UVs must get a +X tangent with
// positive handedness: without a tangent frame every normal map is wrong.
func TestGenerateTangentsFromUVs(t *testing.T) {
	n := core.V3(0, 0, 1)
	verts := []Vertex3D{
		{Pos: core.V3(0, 0, 0), Normal: n, U: 0, V: 0},
		{Pos: core.V3(1, 0, 0), Normal: n, U: 1, V: 0},
		{Pos: core.V3(1, 1, 0), Normal: n, U: 1, V: 1},
		{Pos: core.V3(0, 1, 0), Normal: n, U: 0, V: 1},
	}
	generateTangents(verts, []uint32{0, 1, 2, 0, 2, 3})
	for i, v := range verts {
		tan := core.V3(v.Tan.X, v.Tan.Y, v.Tan.Z)
		if d := tan.Dot(core.V3(1, 0, 0)); d < 0.99 {
			t.Errorf("vertex %d tangent = %v, want +X (dot %.3f)", i, tan, d)
		}
		if math.Abs(float64(tan.Length()-1)) > 1e-4 {
			t.Errorf("vertex %d tangent not normalized: %v", i, tan)
		}
		if v.Tan.W != 1 {
			t.Errorf("vertex %d handedness = %v, want +1", i, v.Tan.W)
		}
		if d := tan.Dot(v.Normal); math.Abs(float64(d)) > 1e-4 {
			t.Errorf("vertex %d tangent not orthogonal to the normal (dot %.4f)", i, d)
		}
	}
}

// Mirrored UVs (V running backwards) must flip the handedness, or the
// bitangent — and so the normal map's green channel — points the wrong way.
func TestGenerateTangentsHandedness(t *testing.T) {
	n := core.V3(0, 0, 1)
	verts := []Vertex3D{
		{Pos: core.V3(0, 0, 0), Normal: n, U: 0, V: 1},
		{Pos: core.V3(1, 0, 0), Normal: n, U: 1, V: 1},
		{Pos: core.V3(1, 1, 0), Normal: n, U: 1, V: 0},
	}
	generateTangents(verts, []uint32{0, 1, 2})
	if verts[0].Tan.W != -1 {
		t.Errorf("handedness = %v, want -1 for a V-flipped parametrisation", verts[0].Tan.W)
	}
}

// Geometry with no UVs (the unit cube) has no UV-derived tangent frame at
// all; the generator must leave a zero tangent rather than a NaN, so the
// shader can fall back to a screen-space frame.
func TestGenerateTangentsDegenerateUVs(t *testing.T) {
	n := core.V3(0, 0, 1)
	verts := []Vertex3D{
		{Pos: core.V3(0, 0, 0), Normal: n},
		{Pos: core.V3(1, 0, 0), Normal: n},
		{Pos: core.V3(1, 1, 0), Normal: n},
	}
	generateTangents(verts, []uint32{0, 1, 2})
	for i, v := range verts {
		if v.Tan != (core.Vec4{}) {
			t.Errorf("vertex %d tangent = %v, want zero for degenerate UVs", i, v.Tan)
		}
	}
}

// The mip chain must go all the way down to 1x1 (the material shader reads
// the last level as the texture's average for the hex-tiling blend) and
// halve each level, non-square textures included.
func TestMipChainDimensions(t *testing.T) {
	for _, c := range []struct{ w, h, want int }{
		{1, 1, 1}, {2, 2, 2}, {256, 256, 9}, {1024, 1024, 11}, {64, 16, 7}, {5, 3, 3},
	} {
		if got := gpu.MipLevels(c.w, c.h); got != c.want {
			t.Errorf("MipLevels(%d,%d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}

	// A 4x2 image of two flat halves box-filters to the same two halves at
	// 2x1, then to their average at 1x1.
	src := make([]byte, 4*2*4)
	for i := range 8 {
		v := byte(0)
		if i%4 >= 2 {
			v = 200
		}
		src[i*4], src[i*4+1], src[i*4+2], src[i*4+3] = v, v, v, 255
	}
	lvl1, w, h := downsampleRGBA(src, 4, 2)
	if w != 2 || h != 1 || len(lvl1) != 2*1*4 {
		t.Fatalf("level 1 = %dx%d (%d bytes), want 2x1", w, h, len(lvl1))
	}
	if lvl1[0] != 0 || lvl1[4] != 200 {
		t.Errorf("level 1 = %d,%d, want 0,200", lvl1[0], lvl1[4])
	}
	lvl2, w, h := downsampleRGBA(lvl1, w, h)
	if w != 1 || h != 1 {
		t.Fatalf("level 2 = %dx%d, want 1x1", w, h)
	}
	if lvl2[0] != 100 {
		t.Errorf("level 2 = %d, want the 100 average", lvl2[0])
	}
}

// The material WGSL must sample with EXPLICIT derivatives everywhere:
// hex tiling and triplanar run inside non-uniform control flow, where an
// implicit-LOD textureSample is invalid — and a violation only shows up as
// a shader-compile failure on some backends (js/Dawn in particular).
func TestMaterialShaderUsesExplicitLOD(t *testing.T) {
	body := shaderMaterial
	for _, want := range []string{"fn hex_grid", "fn hex_sample", "fn tri_sample", "fn tri_normal", "textureSampleGrad", "textureSampleLevel"} {
		if !strings.Contains(body, want) {
			t.Errorf("material shader is missing %q", want)
		}
	}
	// textureSample( with no suffix is the implicit-derivative form.
	for _, line := range strings.Split(body, "\n") {
		i := strings.Index(line, "textureSample(")
		if i >= 0 {
			t.Errorf("implicit-LOD sampling in the material shader: %s", strings.TrimSpace(line))
		}
	}
}

// groundQuad builds a 40x40 floor quad in the XZ plane at y=0 with UVs
// running 0..1 across it (the shared unit cube carries no texcoords, so a
// UV-space material test needs real geometry).
func groundQuad(r *Renderer3D) *Mesh {
	const h = 20
	n := core.V3(0, 1, 0)
	verts := []Vertex3D{
		{Pos: core.V3(-h, 0, -h), Normal: n, U: 0, V: 0},
		{Pos: core.V3(h, 0, -h), Normal: n, U: 1, V: 0},
		{Pos: core.V3(h, 0, h), Normal: n, U: 1, V: 1},
		{Pos: core.V3(-h, 0, h), Normal: n, U: 0, V: 1},
	}
	return r.NewMesh(verts, []uint32{0, 2, 1, 0, 3, 2})
}

// matDevice is one headless device shared by every material test. Each
// scene here needs a Renderer3D (four pipelines and a 2048² shadow map), and
// standing up a fresh device per scene exhausts the driver's allocation
// budget partway through the package's tests.
var matDevice = sync.OnceValues(func() (*gpu.Device, *gpu.Queue) {
	inst, err := gpu.NewInstance(matLibPath)
	if err != nil {
		return nil, nil
	}
	adapter, err := inst.RequestAdapter(nil)
	if err != nil {
		return nil, nil
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		return nil, nil
	}
	return device, device.Queue()
})

var matLibPath string

// materialScene renders a textured ground quad seen from a low camera, with
// optional lights and fog, and returns the pixels. Everything it allocates
// is released before it returns, so the shared device survives the suite.
func materialScene(t *testing.T, setup func(r *Renderer3D), draw func(r *Renderer3D, ground *Mesh, m *Material)) []byte {
	t.Helper()
	matLibPath = wgpuLib(t)
	device, queue := matDevice()
	if device == nil {
		t.Skip("no headless device")
	}
	const W, H = testW, testW
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	depthTex := device.CreateDepthTexture(W, H)
	defer depthTex.Release()
	depthView := depthTex.CreateView()
	defer depthView.Release()

	r, err := New3D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New3D: %v", err)
	}
	defer r.Release()

	cam := core.V3(0, 1.6, 6)
	viewM := core.Mat4LookAt(cam, core.V3(0, 0, -4), core.V3(0, 1, 0))
	proj := core.Mat4Perspective(math.Pi/3, 1, 0.1, 200)
	vp := proj.Mul(viewM)

	// A checker, mipmapped, standing in for a real albedo map.
	const n = 8
	pix := make([]byte, n*n*4)
	for y := range n {
		for x := range n {
			v := byte(230)
			if (x/2+y/2)%2 == 0 {
				v = 60
			}
			i := (y*n + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = v, v, v, 255
		}
	}
	img := r.NewImage3DMipmapped(n, n, pix)
	defer img.Release()
	m := &Material{Albedo: img, UVScale: 12, Roughness: 0.8}
	defer m.Release()
	ground := groundQuad(r)
	defer ground.Release()

	setup(r)
	r.SetShadowArea(core.V3(0, 0, -4), 14)
	r.Begin(vp, core.V3(-0.35, -1, -0.4), cam)
	draw(r, ground, m)

	enc := device.CreateCommandEncoder()
	if err := r.Render(enc, view, depthView, gpu.Color{R: 0.1, G: 0.1, B: 0.1, A: 1}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	queue.Submit(enc.Finish())
	out, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	return out
}

func lum(pix []byte, x, y int) int {
	i := (y*testW + x) * 4
	return int(pix[i]) + int(pix[i+1]) + int(pix[i+2])
}

// A material draw must light up: textured, shaded, and nothing like black.
func TestMaterialDrawPlausiblePixels(t *testing.T) {
	model := core.Mat4Identity()
	pix := materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
		r.DrawMaterial(ground, model, m)
	})
	// The lower half of the frame is floor; it must be lit, not clear.
	lit := 0
	for y := 170; y < 230; y++ {
		for x := 20; x < 220; x += 4 {
			if l := lum(pix, x, y); l > 90 && l < 3*255 {
				lit++
			}
		}
	}
	if lit < 1000 {
		dumpPNG(t, pix, testW, testW, "material-plain")
		t.Fatalf("only %d lit floor samples — the material pass drew nothing plausible", lit)
	}
}

// A coloured point light must pool colour on the floor beneath it: the same
// scene with the light must be measurably redder there, and unchanged far
// away.
func TestMaterialPointLight(t *testing.T) {
	model := core.Mat4Identity()
	draw := func(r *Renderer3D, ground *Mesh, m *Material) { r.DrawMaterial(ground, model, m) }
	dark := FrameSettings{Exposure: 1, Tonemap: 2}
	base := materialScene(t, func(r *Renderer3D) { r.SetFrameSettings(dark) }, draw)
	lit := materialScene(t, func(r *Renderer3D) {
		r.SetFrameSettings(dark)
		r.SetPointLights([]PointLight{{
			Pos: core.V3(0, 1.0, -3), Color: RGBA{R: 1, A: 1}, Energy: 40, Range: 6,
		}})
	}, draw)

	// Straight under the light (screen centre-ish, on the floor).
	const px, py = 124, 126
	i := (py*testW + px) * 4
	if int(lit[i]) <= int(base[i])+20 {
		dumpPNG(t, lit, testW, testW, "material-pointlight")
		t.Fatalf("point light added no red under it: %d vs %d", lit[i], base[i])
	}
	if int(lit[i]) <= int(lit[i+2])+20 {
		t.Errorf("light pool is not red: r=%d b=%d", lit[i], lit[i+2])
	}
	// A screen corner far outside the 6-unit range must be untouched.
	j := (232*testW + 8) * 4
	if d := int(lit[j]) - int(base[j]); d > 2 || d < -2 {
		t.Errorf("light leaked beyond its range: delta %d", d)
	}
}

// Dense height fog must wash distant floor toward the fog colour while
// leaving the near floor alone.
func TestMaterialFog(t *testing.T) {
	model := core.Mat4Identity()
	draw := func(r *Renderer3D, ground *Mesh, m *Material) { r.DrawMaterial(ground, model, m) }
	base := materialScene(t, func(r *Renderer3D) {}, draw)
	fs := DefaultFrameSettings()
	fs.Fog.Enabled = true
	fs.Fog.Color = RGBA{R: 0.85, G: 0.2, B: 0.2, A: 1}
	fs.Fog.Density = 0.05
	fs.Fog.Height = 0
	fs.Fog.HeightDensity = 0.02
	fogged := materialScene(t, func(r *Renderer3D) { r.SetFrameSettings(fs) }, draw)

	// A distant sliver of floor near the horizon, and the near floor.
	far := (126*testW + 120) * 4
	near := (232*testW + 120) * 4
	dFar := int(fogged[far]) - int(fogged[far+2])
	dNear := int(base[near]) - int(fogged[near])
	if dFar < 30 {
		dumpPNG(t, fogged, testW, testW, "material-fog")
		t.Fatalf("distant floor did not take the red fog colour: r-b = %d", dFar)
	}
	if dNear > 40 || dNear < -40 {
		t.Errorf("near floor changed too much under fog: delta %d", dNear)
	}
}

// DrawInstanced must put every model matrix on screen from one draw call.
func TestMaterialInstancedDraw(t *testing.T) {
	var models []core.Mat4
	for i := range 24 {
		x := -5 + float32(i%8)*1.4
		z := -2 - float32(i/8)*2.2
		models = append(models, core.Mat4Translate(core.V3(x, 0.7, z)).Mul(core.Mat4Scale(core.V3(0.5, 1.4, 0.5))))
	}
	model := core.Mat4Identity()
	one := materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
		r.DrawMaterial(ground, model, m)
	})
	many := materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
		r.DrawMaterial(ground, model, m)
		r.DrawInstanced(r.CubeMesh(), models, &Material{
			Color: RGBA{R: 0.9, G: 0.35, B: 0.2, A: 1}, Roughness: 0.5,
		})
	})
	changed := 0
	for i := 0; i < len(one); i += 4 {
		if one[i] != many[i] || one[i+1] != many[i+1] {
			changed++
		}
	}
	if changed < 3000 {
		dumpPNG(t, many, testW, testW, "material-instanced")
		t.Fatalf("24 instances changed only %d pixels — instancing did not draw", changed)
	}
}

// AntiTile must actually change the shading (the hex-tiling path runs) and
// must not blow out to black or white.
func TestMaterialAntiTile(t *testing.T) {
	model := core.Mat4Identity()
	plain := materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
		r.DrawMaterial(ground, model, m)
	})
	hexed := materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
		m.AntiTile = true
		r.DrawMaterial(ground, model, m)
	})
	diff, sum, n := 0, 0, 0
	for y := 170; y < 230; y++ {
		for x := 20; x < 220; x++ {
			i := (y*testW + x) * 4
			if plain[i] != hexed[i] {
				diff++
			}
			sum += int(hexed[i])
			n++
		}
	}
	if diff < 500 {
		t.Errorf("AntiTile changed only %d floor pixels — hex tiling did not run", diff)
	}
	if avg := sum / n; avg < 20 || avg > 240 {
		dumpPNG(t, hexed, testW, testW, "material-antitile")
		t.Errorf("hex-tiled floor average %d is degenerate", avg)
	}
}

// Triplanar projection needs no UVs at all: the unit cube carries none, and
// must still come out textured rather than flat.
func TestMaterialTriplanar(t *testing.T) {
	model := core.Mat4Identity()
	pix := materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
		m.Triplanar = true
		m.UVScale = 0.5
		r.DrawMaterial(ground, model, m)
	})
	lo, hi := 255, 0
	for y := 180; y < 230; y++ {
		for x := 40; x < 200; x++ {
			v := int(pix[(y*testW+x)*4])
			lo, hi = min(lo, v), max(hi, v)
		}
	}
	if hi-lo < 20 {
		dumpPNG(t, pix, testW, testW, "material-triplanar")
		t.Errorf("triplanar floor is flat: range %d..%d", lo, hi)
	}
}

// A translucent material is drawn in the blended variant after the opaques;
// the surface behind it must still show through.
func TestMaterialAlphaBlend(t *testing.T) {
	model := core.Mat4Identity()
	opaque := materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
		r.DrawMaterial(ground, model, m)
	})
	glass := materialScene(t, func(r *Renderer3D) {}, func(r *Renderer3D, ground *Mesh, m *Material) {
		r.DrawMaterial(ground, model, m)
		r.DrawMaterial(r.CubeMesh(), core.Mat4Translate(core.V3(0, 1.5, -3)).Mul(core.Mat4Scale(core.V3(6, 3, 0.1))),
			&Material{Color: RGBA{R: 0.2, G: 0.9, B: 0.4, A: 1}, Alpha: 0.35, DoubleSided: true})
	})
	i := (110*testW + 120) * 4
	if glass[i+1] <= opaque[i+1] {
		dumpPNG(t, glass, testW, testW, "material-alpha")
		t.Fatalf("blended pane added no green: %d vs %d", glass[i+1], opaque[i+1])
	}
	if glass[i+1] == 255 && glass[i] == 0 {
		t.Errorf("blended pane rendered opaque, not blended")
	}
}
