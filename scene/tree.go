package scene

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ecs"
)

// Tree is the 3D scene graph implemented exactly as DESIGN.md §4.1
// decided: nodes are ecs entities (handles), their data lives in component
// columns, and parent→child world transforms propagate in one pass over
// the transform column's rows taken in depth order — no recursive tree
// walk.
type Tree struct {
	World *ecs.World

	transforms *ecs.Component[Transform]
	links      *ecs.Component[link]

	// Update's scratch, kept between calls: each transform row's link, the
	// rows in depth order, and where each depth starts in that order.
	rowLinks []link
	order    []int32
	levels   []int32
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

// Spawn creates a node under parent (zero Entity = root). Only a node of
// this tree is a parent: a dead handle, or a live entity of World that was
// not spawned here and so has no transform to inherit, makes a root.
func (t *Tree) Spawn(parent Node) Node {
	e := t.World.Spawn()
	depth := int32(0)
	if l := t.links.Get(parent); l != nil && t.transforms.Has(parent) {
		depth = l.depth + 1
	} else {
		parent = Node{}
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
// so a single pass over rows in depth order resolves the whole tree. The
// order is rebuilt on every call (a counting sort by depth, linear in the
// node count): rows move when nodes are despawned, so no order survives
// between calls. A node whose parent is gone, or has lost its transform,
// takes its local matrix as its world matrix.
func (t *Tree) Update() {
	col := t.transforms.Column()
	t.rowLinks = t.rowLinks[:0]
	t.levels = t.levels[:0]
	for i := range col {
		l := link{depth: -1} // a transform without a link is left alone
		if p := t.links.Get(t.transforms.EntityAt(i)); p != nil {
			l = *p
			for int(l.depth)+1 >= len(t.levels) {
				t.levels = append(t.levels, 0)
			}
			t.levels[l.depth+1]++
		}
		t.rowLinks = append(t.rowLinks, l)
	}
	for d := 1; d < len(t.levels); d++ {
		t.levels[d] += t.levels[d-1] // levels[d] is where depth d starts
	}
	if len(t.levels) == 0 {
		return
	}
	t.order = append(t.order[:0], make([]int32, t.levels[len(t.levels)-1])...)
	for i, l := range t.rowLinks {
		if l.depth >= 0 {
			t.order[t.levels[l.depth]] = int32(i)
			t.levels[l.depth]++
		}
	}
	for _, i := range t.order {
		local := trs(&col[i])
		l := &t.rowLinks[i]
		if parent := t.transforms.Get(l.parent); l.depth > 0 && parent != nil {
			col[i].world = parent.world.Mul(local)
		} else {
			col[i].world = local
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
