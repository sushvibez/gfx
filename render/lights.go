package render

// Punctual lights and per-frame environment settings for the 3D renderer
// (DESIGN.md §9). The directional light with its shadow map stays where it
// was — on Begin — and this adds the local lights a level needs (lamps,
// muzzle flashes, neon) plus the frame-wide look: ambient, exposure, tone
// map, height fog and a sky gradient the fog fades into.
//
// Up to maxLights point + spot lights are uploaded per frame into one
// storage block and evaluated by every material draw. They cast no shadows.

import (
	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/core"
)

// maxLights is the per-frame punctual light budget (matching the WGSL
// array). Lights beyond it are dropped, nearest-first being the caller's
// job.
const maxLights = 16

// PointLight is an omnidirectional light with a smooth range cutoff.
type PointLight struct {
	Pos    core.Vec3
	Color  RGBA
	Energy float32 // brightness multiplier (zero = 1)
	Range  float32 // distance at which the light reaches zero (zero = 10)
}

// SpotLight is a PointLight restricted to a cone.
type SpotLight struct {
	Pos    core.Vec3
	Dir    core.Vec3 // cone axis, pointing away from Pos
	Color  RGBA
	Energy float32 // zero = 1
	Range  float32 // zero = 10
	Angle  float32 // half-angle of the cone in radians (zero = 0.6)
	// AngleAttenuation softens the cone edge: 0 is a hard edge, larger
	// values fade further in (zero = 0.2).
	AngleAttenuation float32
	// Shadow makes the light cast shadows: the first MaxSpotShadows such
	// lights of a frame get a shadow map each (material draws, unskinned
	// casters); the rest light without one.
	Shadow bool
}

// MaxSpotShadows is how many spot lights can cast shadows in one frame.
const MaxSpotShadows = 4

// spotShadowSize is the side of one spot light's shadow map.
const spotShadowSize = 1024

// FrameSettings is the per-frame environment: ambient light, camera
// exposure and tone map, fog and the sky gradient distant fog fades into.
// The zero value is not used directly — SetFrameSettings normalises it, so
// leaving Exposure at 0 still gives exposure 1.
type FrameSettings struct {
	// Ambient is the constant environment term (colour premultiplied by
	// energy); it is modulated by a material's AO channel.
	Ambient RGBA
	// Sun is the directional light's radiance: colour times energy (zero =
	// white at 2.6, the renderer's original sun). A low golden sun is
	// something like {3.4, 2.3, 1.2}. Material draws only.
	Sun RGBA
	// SunSoftness makes the sun's shadow soft the way a real one is (PCSS):
	// crisp where it touches the ground, wider with distance from what
	// casts it. It is the tangent of the light's apparent half-angle,
	// scaled for look: 0 keeps the hard single-tap shadow, 0.02 is a
	// pleasing sun, 0.06 an overcast-soft one. Material draws only.
	SunSoftness float32
	// SkyReflection (0…1) makes surfaces mirror the sky (SkyTop, SkyHorizon
	// and the sun's glow): by Fresnel, sharp on glossy materials and smeared
	// on rough ones. It is the environment's specular light; scene geometry
	// is not reflected. 0 (the default) leaves materials as they were.
	SkyReflection float32
	// SceneReflection (0…1) makes glossy surfaces mirror the scene itself:
	// where the reflected ray meets something on screen, that replaces the
	// sky's reflection (screen-space, traced by render.Post through the
	// depth buffer, so it needs SkyReflection, TonemapHDR and a Post; what
	// is off screen or hidden is not reflected). Opaque material draws
	// carry their mirror strength in the HDR target's alpha.
	SceneReflection float32
	// Wetness (0…1) wets every material surface: damp surfaces darken and
	// gloss over, and standing water gathers in puddles on level ground
	// (placed by a noise over world x and z, more of them as it rises).
	// With SceneReflection the puddles mirror the scene.
	Wetness float32
	// Rain (0…1) is rain falling on the wet: rings spread across the
	// puddles and their mirror ripples (see Atmosphere.Rain for the
	// streaks in the air).
	Rain float32
	// Time, in seconds, moves what animates in the shader (Material.Water).
	Time float32
	// Exposure scales linear radiance before the tone map (zero = 1).
	Exposure float32
	// Tonemap: 0 = none (clamp), 1 = filmic, 2 = ACES, TonemapHDR (3) =
	// linear radiance for a Post to display (material draws only: the
	// legacy Draw* calls always write display colour).
	Tonemap int

	Fog struct {
		Enabled bool
		Color   RGBA
		// Density is the extinction per world unit at the fog height.
		Density float32
		// Height is the world Y at which Density applies, and
		// HeightDensity how fast the fog thins going up (0 = uniform).
		Height        float32
		HeightDensity float32
		// AerialPerspective blends the fog colour toward the sky
		// gradient in the view direction (0 = flat fog colour, 1 = full
		// sky).
		AerialPerspective float32
	}

	// SkyTop / SkyHorizon define the gradient used for aerial perspective
	// (and available to callers as a matching clear colour).
	SkyTop, SkyHorizon RGBA
}

// DefaultFrameSettings is what a Renderer3D starts with: neutral ambient,
// exposure 1, ACES tone map, no fog.
func DefaultFrameSettings() FrameSettings {
	fs := FrameSettings{
		Sun:        RGBA{R: sunIntensity, G: sunIntensity, B: sunIntensity, A: 1},
		Ambient:    RGBA{R: 0.05, G: 0.055, B: 0.07, A: 1},
		Exposure:   1,
		Tonemap:    2,
		SkyTop:     RGBA{R: 0.20, G: 0.34, B: 0.60, A: 1},
		SkyHorizon: RGBA{R: 0.62, G: 0.70, B: 0.82, A: 1},
	}
	fs.Fog.Color = RGBA{R: 0.60, G: 0.66, B: 0.76, A: 1}
	return fs
}

// SetFrameSettings installs the environment used by material draws from the
// next Begin. Call it once at setup, or per frame for time-of-day changes.
func (r *Renderer3D) SetFrameSettings(fs FrameSettings) {
	if fs.Exposure <= 0 {
		fs.Exposure = 1
	}
	if fs.Sun.R == 0 && fs.Sun.G == 0 && fs.Sun.B == 0 {
		fs.Sun = RGBA{R: sunIntensity, G: sunIntensity, B: sunIntensity, A: 1}
	}
	if fs.Tonemap < 0 || fs.Tonemap > TonemapHDR {
		fs.Tonemap = 0
	}
	r.settings = fs
}

// FrameSettingsOf returns the current environment settings.
func (r *Renderer3D) FrameSettingsOf() FrameSettings { return r.settings }

// SetPointLights replaces the frame's point lights (up to maxLights total
// across point and spot lights). Call before Begin, which packs them.
func (r *Renderer3D) SetPointLights(ls []PointLight) {
	r.points = append(r.points[:0], ls...)
}

// SetSpotLights replaces the frame's spot lights; they share the budget
// with SetPointLights.
func (r *Renderer3D) SetSpotLights(ls []SpotLight) {
	r.spots = append(r.spots[:0], ls...)
}

// gpuLight mirrors the WGSL Light struct (64 bytes).
type gpuLight struct {
	PosRange core.Vec4 // xyz position, w range
	Color    core.Vec4 // rgb colour * energy, w kind (0 point, 1 spot)
	DirCos   core.Vec4 // xyz cone axis, w cos(outer half-angle)
	Params   core.Vec4 // x cos(inner half-angle), y shadow slot + 1 (0 = none), z world size of a shadow texel per metre of distance
}

// lightBlock mirrors the WGSL Lights storage block.
type lightBlock struct {
	Items  [maxLights]gpuLight
	SpotVP [MaxSpotShadows]core.Mat4 // the shadow-casting spots' view-projections, by slot
}

// packLights flattens the frame's point and spot lights into the GPU block
// and returns how many were written.
func (r *Renderer3D) packLights(dst *lightBlock) int {
	n := 0
	r.spotShadows = 0
	put := func(l gpuLight) {
		if n < maxLights {
			dst.Items[n] = l
			n++
		}
	}
	for _, p := range r.points {
		e, rng := p.Energy, p.Range
		if e == 0 {
			e = 1
		}
		if rng <= 0 {
			rng = 10
		}
		put(gpuLight{
			PosRange: core.V4(p.Pos.X, p.Pos.Y, p.Pos.Z, rng),
			Color:    core.V4(p.Color.R*e, p.Color.G*e, p.Color.B*e, 0),
			DirCos:   core.V4(0, -1, 0, -1),
			Params:   core.V4(-1, 0, 0, 0),
		})
	}
	for _, s := range r.spots {
		e, rng := s.Energy, s.Range
		if e == 0 {
			e = 1
		}
		if rng <= 0 {
			rng = 10
		}
		ang := s.Angle
		if ang <= 0 {
			ang = 0.6
		}
		soft := s.AngleAttenuation
		if soft <= 0 {
			soft = 0.2
		}
		d := s.Dir
		if d.Length() < 1e-6 {
			d = core.V3(0, -1, 0)
		}
		d = d.Normalize()
		outer := math32.Cos(ang)
		inner := math32.Cos(max(ang*(1-soft), 0))
		var slot, texel float32
		if s.Shadow && r.spotShadows < MaxSpotShadows && n < maxLights {
			// The map covers the whole cone: a square frustum just wider
			// than it, from close under the lamp out to its range.
			fov := min(2*ang+0.08, 2.9)
			up := core.V3(0, 1, 0)
			if d.Y > 0.99 || d.Y < -0.99 {
				up = core.V3(1, 0, 0)
			}
			vp := core.Mat4Perspective(fov, 1, max(rng*0.01, 0.05), rng).Mul(core.Mat4LookAt(s.Pos, s.Pos.Add(d), up))
			dst.SpotVP[r.spotShadows] = vp
			r.spotShadows++
			slot, texel = float32(r.spotShadows), 2*math32.Tan(fov/2)/spotShadowSize
		}
		put(gpuLight{
			PosRange: core.V4(s.Pos.X, s.Pos.Y, s.Pos.Z, rng),
			Color:    core.V4(s.Color.R*e, s.Color.G*e, s.Color.B*e, 1),
			DirCos:   core.V4(d.X, d.Y, d.Z, outer),
			Params:   core.V4(inner, slot, texel, 0),
		})
	}
	for i := n; i < maxLights; i++ {
		dst.Items[i] = gpuLight{}
	}
	return n
}
