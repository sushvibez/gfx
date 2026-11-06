package ecs

// GPUShared marks a component type as GPU-sharable (DESIGN.md §6.2):
// embed it in a POD struct and `gfx gen` (run via go generate) emits the
// matching WGSL struct declaration plus compile-time layout assertions,
// so the Go and shader views of the data cannot drift.
//
//	//go:generate go run github.com/sushvibez/gfx/script/cmd/gfx gen
//
//	type Particle struct {
//	    ecs.GPUShared
//	    Pos core.Vec2
//	    Vel core.Vec2
//	}
//
// The marker is zero-sized and adds nothing to the struct's layout.
type GPUShared struct{}
