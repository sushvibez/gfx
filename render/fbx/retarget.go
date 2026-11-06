package fbx

import "github.com/sushvibez/gfx/render/gltf"

// Retarget rewrites every animation channel in src so it drives dst's nodes,
// matched by BaseName (namespace-stripped bone name). This lets a clip
// authored on one Mixamo rig ("mixamorig6:Hips") animate a model rigged as
// "mixamorig:Hips" — the Mixamo case, where the character mesh and its animation
// clips ship as separate FBX files.
//
// It is NOT a plain index remap. The two rigs render the same world rest pose
// but decompose bones into different local frames (Mixamo mirrors right-side
// limb bones with a 180° roll, and the 6100 mesh vs 7700 clip exporters differ
// besides), so a bone's rest local rotation in the clip need not match ours.
// Applying the clip's absolute local rotations directly would distort exactly
// those bones. Instead each keyframe is converted into a rest-relative delta
// and re-expressed against dst's rest:
//
//	R'   = restDst · inverse(restSrc) · animR      (rotation)
//	T'   = restDst_T + (animT - restSrc_T)         (translation)
//
// At a rest frame (animR == restSrc) this reproduces dst's own rest exactly,
// so the retargeted clip starts from the model's real bind pose. Channels
// whose bone has no match in dst are dropped.
//
// Retarget must run before the clip is played, while dst still holds its
// reconstructed bind pose (T/R/S) as the rest reference.
func Retarget(dst, src *gltf.Document) {
	byName := make(map[string]int, len(dst.Nodes))
	for i, n := range dst.Nodes {
		byName[BaseName(n.Name)] = i
	}
	for ai := range src.Animations {
		clip := &src.Animations[ai]
		kept := clip.Channels[:0]
		for _, ch := range clip.Channels {
			if ch.Node < 0 || ch.Node >= len(src.Nodes) {
				continue
			}
			di, ok := byName[BaseName(src.Nodes[ch.Node].Name)]
			if !ok {
				continue
			}
			restSrc := src.Nodes[ch.Node]
			restDst := dst.Nodes[di]
			switch ch.Path {
			case gltf.PathRotation:
				// frame = restDst · inverse(restSrc); R' = frame · animR
				frame := restDst.R.Mul(restSrc.R.Conjugate())
				for k := range ch.Rot {
					ch.Rot[k] = frame.Mul(ch.Rot[k]).Normalize()
				}
			case gltf.PathTranslation:
				// Re-base onto dst's rest translation (delta from src rest).
				off := restDst.T.Sub(restSrc.T)
				for k := range ch.Vec {
					ch.Vec[k] = ch.Vec[k].Add(off)
				}
			}
			ch.Node = di
			kept = append(kept, ch)
		}
		clip.Channels = kept
	}
}
