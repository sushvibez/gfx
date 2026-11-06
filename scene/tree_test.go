package scene

import (
	"math"
	"testing"

	"github.com/sushvibez/gfx/core"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func vecNear(a, b core.Vec3) bool { return near(a.X, b.X) && near(a.Y, b.Y) && near(a.Z, b.Z) }

func TestWorldMatrixPropagation(t *testing.T) {
	tr := NewTree()
	parent := tr.Spawn(Node{})
	child := tr.Spawn(parent)
	grand := tr.Spawn(child)

	tr.SetLocal(parent, core.V3(10, 0, 0), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.SetLocal(child, core.V3(0, 5, 0), core.QuatIdentity(), core.V3(2, 2, 2))
	tr.SetLocal(grand, core.V3(0, 0, 1), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.Update()

	if got := tr.WorldMatrix(parent).MulPoint(core.V3(0, 0, 0)); !vecNear(got, core.V3(10, 0, 0)) {
		t.Fatalf("parent origin = %v", got)
	}
	if got := tr.WorldMatrix(child).MulPoint(core.V3(0, 0, 0)); !vecNear(got, core.V3(10, 5, 0)) {
		t.Fatalf("child origin = %v", got)
	}
	// Grandchild local (0,0,1) under child's scale 2: world z = 2.
	if got := tr.WorldMatrix(grand).MulPoint(core.V3(0, 0, 0)); !vecNear(got, core.V3(10, 5, 2)) {
		t.Fatalf("grand origin = %v", got)
	}
}

func TestRotationPropagation(t *testing.T) {
	tr := NewTree()
	parent := tr.Spawn(Node{})
	child := tr.Spawn(parent)
	// Parent rotated 90° around Z: child's +X offset becomes world +Y.
	tr.SetLocal(parent, core.V3(0, 0, 0), core.QuatAxisAngle(core.V3(0, 0, 1), math.Pi/2), core.V3(1, 1, 1))
	tr.SetLocal(child, core.V3(1, 0, 0), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.Update()
	if got := tr.WorldMatrix(child).MulPoint(core.V3(0, 0, 0)); !vecNear(got, core.V3(0, 1, 0)) {
		t.Fatalf("rotated child origin = %v", got)
	}
}

func TestDespawnedParentFallsBackToLocal(t *testing.T) {
	tr := NewTree()
	parent := tr.Spawn(Node{})
	child := tr.Spawn(parent)
	tr.SetLocal(parent, core.V3(100, 0, 0), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.SetLocal(child, core.V3(1, 2, 3), core.QuatIdentity(), core.V3(1, 1, 1))
	tr.World.Despawn(parent)
	tr.Update()
	if got := tr.WorldMatrix(child).MulPoint(core.V3(0, 0, 0)); !vecNear(got, core.V3(1, 2, 3)) {
		t.Fatalf("orphaned child = %v, want local", got)
	}
}
