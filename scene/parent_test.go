package scene

import (
	"testing"

	"github.com/sushvibez/gfx/core"
)

// Tree.World is public, so a parent can be a live entity that was never
// spawned through the tree: it has no transform to inherit, and its
// children are roots.
func TestParentWithoutNodeComponentsIsARoot(t *testing.T) {
	tr := NewTree()
	parent := tr.World.Spawn()
	child := tr.Spawn(parent)
	grand := tr.Spawn(child)
	tr.SetLocal(child, core.V3(1, 2, 3), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.SetLocal(grand, core.V3(10, 0, 0), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.Update()
	if got := tr.WorldMatrix(child).MulPoint(core.Vec3{}); !vecNear(got, core.V3(1, 2, 3)) {
		t.Errorf("child of a non-node = %v, want its local (1,2,3)", got)
	}
	if got := tr.WorldMatrix(grand).MulPoint(core.Vec3{}); !vecNear(got, core.V3(11, 2, 3)) {
		t.Errorf("grandchild = %v, want (11,2,3)", got)
	}
}

// A handle that was already dead when the node was spawned is no parent.
func TestStaleParentIsARoot(t *testing.T) {
	tr := NewTree()
	gone := tr.Spawn(Node{})
	tr.World.Despawn(gone)
	child := tr.Spawn(gone)
	tr.SetLocal(child, core.V3(4, 5, 6), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.Update()
	if got := tr.WorldMatrix(child).MulPoint(core.Vec3{}); !vecNear(got, core.V3(4, 5, 6)) {
		t.Errorf("child of a stale handle = %v, want its local", got)
	}
}

// Parents resolve before children whatever order the rows sit in: a
// despawn swaps the last row into the hole, which can put a child ahead
// of its parent in the column.
func TestUpdateOrderSurvivesRowSwaps(t *testing.T) {
	tr := NewTree()
	filler := tr.Spawn(Node{})
	root := tr.Spawn(Node{})
	mid := tr.Spawn(root)
	leaf := tr.Spawn(mid)
	tr.World.Despawn(filler) // leaf's row moves to the front
	tr.SetLocal(root, core.V3(1, 0, 0), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.SetLocal(mid, core.V3(0, 2, 0), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.SetLocal(leaf, core.V3(0, 0, 3), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.Update()
	if got := tr.WorldMatrix(leaf).MulPoint(core.Vec3{}); !vecNear(got, core.V3(1, 2, 3)) {
		t.Errorf("leaf = %v, want (1,2,3)", got)
	}
}

// A deep chain resolves from the root down.
func TestUpdateDeepChain(t *testing.T) {
	tr, leaf := chainTree(300)
	tr.Update()
	if got := tr.WorldMatrix(leaf).MulPoint(core.Vec3{}); !vecNear(got, core.V3(300, 0, 0)) {
		t.Errorf("leaf of a 300-node chain = %v, want (300,0,0)", got)
	}
}

// chainTree is the worst case for a per-depth scan: every node one level
// below the last, each one unit along X from its parent.
func chainTree(n int) (*Tree, Node) {
	tr := NewTree()
	parent := Node{}
	for range n {
		parent = tr.Spawn(parent)
		tr.SetLocal(parent, core.V3(1, 0, 0), core.QuatIdentity(), core.V3(1, 1, 1))
	}
	return tr, parent
}

// broadTree is the usual shape: a few levels, many siblings.
func broadTree(roots, children int) *Tree {
	tr := NewTree()
	for range roots {
		r := tr.Spawn(Node{})
		for range children {
			c := tr.Spawn(r)
			tr.Spawn(c)
		}
	}
	return tr
}

func BenchmarkUpdateChain2000(b *testing.B) {
	tr, _ := chainTree(2000)
	b.ReportAllocs()
	for b.Loop() {
		tr.Update()
	}
}

func BenchmarkUpdateBroad10k(b *testing.B) {
	tr := broadTree(100, 50) // 100 + 5000 + 5000 nodes, depth 2
	b.ReportAllocs()
	for b.Loop() {
		tr.Update()
	}
}
