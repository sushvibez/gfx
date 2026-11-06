package gltf

// glTF animations: keyframed node TRS channels. Parsed into Animation
// clips; Apply samples every channel at a time and writes the targeted
// nodes' decomposed pose (T/R/S), recomputing their Local matrices —
// render.AnimationPlayer drives this against a Model's node copy.
// Skinning (joint matrices to the vertex shader) is the next stage.

import (
	"fmt"

	"github.com/sushvibez/gfx/core"
)

// AnimPath is which TRS component a channel drives.
type AnimPath int

const (
	PathTranslation AnimPath = iota
	PathRotation
	PathScale
)

// AnimChannel is one keyframe track targeting one node component.
type AnimChannel struct {
	Node  int
	Path  AnimPath
	Times []float32   // ascending keyframe times, seconds
	Vec   []core.Vec3 // translation/scale values (len == len(Times))
	Rot   []core.Quat // rotation values (len == len(Times))
	Step  bool        // STEP interpolation (else linear/slerp)
}

// Animation is a named clip of channels.
type Animation struct {
	Name     string
	Duration float32
	Channels []AnimChannel
}

// Apply samples the clip at time t (clamped to [0, Duration]) and writes
// the targeted nodes' pose, recomputing their Local matrices.
func (a *Animation) Apply(t float32, nodes []Node) {
	for i := range a.Channels {
		c := &a.Channels[i]
		if c.Node < 0 || c.Node >= len(nodes) || len(c.Times) == 0 {
			continue
		}
		n := &nodes[c.Node]
		if !n.HasTRS {
			// Matrix-only node: adopt identity pose as the animation base
			// (glTF says animated TRS replaces the matrix).
			n.T, n.R, n.S, n.HasTRS = core.Vec3{}, core.QuatIdentity(), core.V3(1, 1, 1), true
		}
		k, f := c.segment(t)
		switch c.Path {
		case PathTranslation:
			n.T = lerpV3(c.Vec[k], c.Vec[min(k+1, len(c.Vec)-1)], f)
		case PathScale:
			n.S = lerpV3(c.Vec[k], c.Vec[min(k+1, len(c.Vec)-1)], f)
		case PathRotation:
			n.R = c.Rot[k].Slerp(c.Rot[min(k+1, len(c.Rot)-1)], f)
		}
		n.RecomputeLocal()
	}
}

// segment finds the keyframe pair bracketing t and the blend fraction
// (0 for STEP or at/before the first key; times past the end clamp).
func (c *AnimChannel) segment(t float32) (int, float32) {
	times := c.Times
	if t <= times[0] || len(times) == 1 {
		return 0, 0
	}
	last := len(times) - 1
	if t >= times[last] {
		return last, 0
	}
	// Binary search for the segment with times[k] <= t < times[k+1].
	lo, hi := 0, last
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if times[mid] <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	if c.Step {
		return lo, 0
	}
	span := times[lo+1] - times[lo]
	if span <= 0 {
		return lo, 0
	}
	return lo, (t - times[lo]) / span
}

func lerpV3(a, b core.Vec3, f float32) core.Vec3 {
	return core.V3(a.X+(b.X-a.X)*f, a.Y+(b.Y-a.Y)*f, a.Z+(b.Z-a.Z)*f)
}

// parseAnimations reads the animations array into clips.
func (p *parser) parseAnimations() ([]Animation, error) {
	var out []Animation
	for ai, ja := range p.j.Animations {
		anim := Animation{Name: ja.Name}
		for _, ch := range ja.Channels {
			if ch.Sampler < 0 || ch.Sampler >= len(ja.Samplers) {
				return nil, fmt.Errorf("gltf: animation %d: sampler %d out of range", ai, ch.Sampler)
			}
			var path AnimPath
			switch ch.Target.Path {
			case "translation":
				path = PathTranslation
			case "rotation":
				path = PathRotation
			case "scale":
				path = PathScale
			default:
				continue // "weights" (morph targets): not supported yet
			}
			s := ja.Samplers[ch.Sampler]
			times, err := p.floats(s.Input, 1)
			if err != nil {
				return nil, fmt.Errorf("gltf: animation %d input: %w", ai, err)
			}
			comps := 3
			if path == PathRotation {
				comps = 4
			}
			vals, err := p.floats(s.Output, comps)
			if err != nil {
				return nil, fmt.Errorf("gltf: animation %d output: %w", ai, err)
			}
			// CUBICSPLINE stores in-tangent, value, out-tangent per key:
			// keep the value, sample linearly (an accepted degradation).
			if s.Interpolation == "CUBICSPLINE" && len(vals) == 3*len(times)*comps {
				picked := make([]float32, 0, len(times)*comps)
				for k := 0; k < len(times); k++ {
					base := (k*3 + 1) * comps
					picked = append(picked, vals[base:base+comps]...)
				}
				vals = picked
			}
			if len(vals) < len(times)*comps {
				return nil, fmt.Errorf("gltf: animation %d: %d values for %d keys", ai, len(vals)/comps, len(times))
			}
			c := AnimChannel{
				Node:  ch.Target.Node,
				Path:  path,
				Times: times,
				Step:  s.Interpolation == "STEP",
			}
			if path == PathRotation {
				c.Rot = make([]core.Quat, len(times))
				for k := range c.Rot {
					c.Rot[k] = core.Quat{X: vals[k*4], Y: vals[k*4+1], Z: vals[k*4+2], W: vals[k*4+3]}.Normalize()
				}
			} else {
				c.Vec = vec3s(vals[:len(times)*3])
			}
			if last := times[len(times)-1]; last > anim.Duration {
				anim.Duration = last
			}
			anim.Channels = append(anim.Channels, c)
		}
		out = append(out, anim)
	}
	return out, nil
}

// ---- pose sampling and blending ----
//
// Apply writes a clip straight into the node array, which is all a single
// playing clip needs. Crossfading needs two clips evaluated independently
// and mixed per joint, so sampling also has a buffer form: Pose is a
// per-node TRS snapshot that SampleInto fills, Blend mixes and Write pushes
// back into the nodes. All three reuse their destination's storage, so a
// player that holds its Pose buffers blends without allocating per frame.

// Pose is a per-node decomposed transform snapshot. Touched[i] marks nodes
// an animation actually drives — Write only writes those, so matrix-only
// nodes that no channel targets keep their authored Local.
type Pose struct {
	T       []core.Vec3
	R       []core.Quat
	S       []core.Vec3
	Touched []bool
}

// NewPose snapshots the nodes' current TRS as a rest reference (nodes
// without TRS get identity; none are marked touched).
func NewPose(nodes []Node) Pose {
	p := Pose{
		T:       make([]core.Vec3, len(nodes)),
		R:       make([]core.Quat, len(nodes)),
		S:       make([]core.Vec3, len(nodes)),
		Touched: make([]bool, len(nodes)),
	}
	for i := range nodes {
		if nodes[i].HasTRS {
			p.T[i], p.R[i], p.S[i] = nodes[i].T, nodes[i].R, nodes[i].S
		} else {
			p.T[i], p.R[i], p.S[i] = core.Vec3{}, core.QuatIdentity(), core.V3(1, 1, 1)
		}
	}
	return p
}

// Len reports the node count the pose covers.
func (p Pose) Len() int { return len(p.T) }

// Reset copies rest into p (same length) and clears the touched marks; the
// rest values are the baseline untouched joints blend towards.
func (p Pose) Reset(rest Pose) {
	copy(p.T, rest.T)
	copy(p.R, rest.R)
	copy(p.S, rest.S)
	for i := range p.Touched {
		p.Touched[i] = false
	}
}

// SampleInto evaluates the clip at time t into dst, which the caller must
// have Reset to the rest pose first. Channels write their node's component
// and mark it touched. No allocation.
func (a *Animation) SampleInto(t float32, dst Pose) {
	for i := range a.Channels {
		c := &a.Channels[i]
		if c.Node < 0 || c.Node >= dst.Len() || len(c.Times) == 0 {
			continue
		}
		k, f := c.segment(t)
		switch c.Path {
		case PathTranslation:
			dst.T[c.Node] = lerpV3(c.Vec[k], c.Vec[min(k+1, len(c.Vec)-1)], f)
		case PathScale:
			dst.S[c.Node] = lerpV3(c.Vec[k], c.Vec[min(k+1, len(c.Vec)-1)], f)
		case PathRotation:
			dst.R[c.Node] = c.Rot[k].Slerp(c.Rot[min(k+1, len(c.Rot)-1)], f)
		}
		dst.Touched[c.Node] = true
	}
}

// BlendPose writes a→b at weight w into dst (w=0 is a, w=1 is b).
// Translation and scale lerp; rotation slerps along the short arc (which
// degrades to a normalized lerp for nearly parallel keys, so a crossfade
// never divides by ~0). A node touched by either source is touched in dst —
// a joint only one clip drives still fades against the other's rest value.
func BlendPose(dst, a, b Pose, w float32) {
	if w <= 0 {
		w = 0
	} else if w >= 1 {
		w = 1
	}
	n := min(min(dst.Len(), a.Len()), b.Len())
	for i := 0; i < n; i++ {
		dst.T[i] = lerpV3(a.T[i], b.T[i], w)
		dst.S[i] = lerpV3(a.S[i], b.S[i], w)
		dst.R[i] = a.R[i].Slerp(b.R[i], w)
		dst.Touched[i] = a.Touched[i] || b.Touched[i]
	}
}

// Write pushes the touched nodes' pose back into the hierarchy and
// recomputes their Local matrices.
func (p Pose) Write(nodes []Node) {
	n := min(p.Len(), len(nodes))
	for i := 0; i < n; i++ {
		if !p.Touched[i] {
			continue
		}
		nd := &nodes[i]
		nd.T, nd.R, nd.S, nd.HasTRS = p.T[i], p.R[i], p.S[i], true
		nd.RecomputeLocal()
	}
}
