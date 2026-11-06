package scene

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ecs"
)

// Tree is the 3D scene graph implemented exactly as DESIGN.md §4.1
// decided: nodes are ecs entities (handles), their data lives in component
// columns, and parent→child world transforms propagate in one linear pass
// over the contiguous transform column — no recursive tree walk.
type Tree struct {
	World *ecs.World

	transforms *ecs.Component[Transform]
	links      *ecs.Component[link]
}

// Transform is a node's local TRS plus its cached world matrix, laid out
// in one column.
type Transform struct {
	Pos   core.Vec3
	Rot   core.Quat
	Scale core.Vec3
	world core.Mat4
}

// link is the structural component: parent handle + depth for ordering.
type link struct {
	parent ecs.Entity
	depth  int32
}

func NewTree() *Tree {
	w := ecs.NewWorld()
	return &Tree{
		World:      w,
		transforms: ecs.NewComponent[Transform](w),
		links:      ecs.NewComponent[link](w),
	}
}

// Node is a scene handle — an entity with transform + link components.
type Node = ecs.Entity

// Spawn creates a node under parent (zero Entity = root).
func (t *Tree) Spawn(parent Node) Node {
	e := t.World.Spawn()
	depth := int32(0)
	if t.World.Alive(parent) {
		if l := t.links.Get(parent); l != nil {
			depth = l.depth + 1
		} else {
			depth = 1
		}
	}
	t.transforms.Add(e, Transform{Rot: core.QuatIdentity(), Scale: core.V3(1, 1, 1)})
	t.links.Add(e, link{parent: parent, depth: depth})
	return e
}

// SetLocal updates a node's local transform.
func (t *Tree) SetLocal(n Node, pos core.Vec3, rot core.Quat, scale core.Vec3) {
	if tr := t.transforms.Get(n); tr != nil {
		tr.Pos, tr.Rot, tr.Scale = pos, rot, scale
	}
}

// Local returns a pointer to the node's transform for in-place mutation.
func (t *Tree) Local(n Node) *Transform { return t.transforms.Get(n) }

// World returns the node's world matrix as of the last Update.
func (t *Tree) WorldMatrix(n Node) core.Mat4 {
	if tr := t.transforms.Get(n); tr != nil {
		return tr.world
	}
	return core.Mat4Identity()
}

// Update recomputes world matrices. Children sort after parents by depth,
// so a single pass over rows in depth order resolves the whole tree;
// within the column this is sequential, cache-friendly work.
func (t *Tree) Update() {
	// v0: depths are small; iterate by increasing depth. Rows are visited
	// per depth level over the contiguous column.
	maxDepth := int32(0)
	t.links.Each(func(_ ecs.Entity, l *link) {
		maxDepth = max(maxDepth, l.depth)
	})
	for d := int32(0); d <= maxDepth; d++ {
		col := t.transforms.Column()
		for i := range col {
			e := t.transforms.EntityAt(i)
			l := t.links.Get(e)
			if l == nil || l.depth != d {
				continue
			}
			local := trs(&col[i])
			if l.depth == 0 || !t.World.Alive(l.parent) {
				col[i].world = local
				continue
			}
			parent := t.transforms.Get(l.parent)
			col[i].world = parent.world.Mul(local)
		}
	}
}

func trs(tr *Transform) core.Mat4 {
	m := tr.Rot.Mat4()
	// Scale the rotation columns, then set translation.
	for c := range 3 {
		s := [3]float32{tr.Scale.X, tr.Scale.Y, tr.Scale.Z}[c]
		m[c*4+0] *= s
		m[c*4+1] *= s
		m[c*4+2] *= s
	}
	m[12], m[13], m[14] = tr.Pos.X, tr.Pos.Y, tr.Pos.Z
	return m
}
