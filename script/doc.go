// Package script hosts the scripting strategy: XGo (formerly Go+) as the
// high-level language — compiled to Go for release builds, interpreted for
// development-time hot reload — plus the `gfx gen` code generator.
//
// Codegen uses only standard Go machinery (decided, see /DESIGN.md §6.2):
// it is invoked via `//go:generate gfx gen`, type-level markers are embedded
// marker types (scene.Node3D, ecs.GPUShared), and field metadata lives in
// `gfx:"..."` struct tags. No custom comment dialect, no XGo fork.
//
// Operator overloading comes from XGo's user-defined operator support.
// Blueprint-style gameplay scripting (event lifecycle + frame-synced latent
// tasks) is a code-first API in the scene module; see /DESIGN.md §6.3.
package script
