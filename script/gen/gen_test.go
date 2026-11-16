package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateParticle(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "comp.go", `package demo

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ecs"
)

type Particle struct {
	ecs.GPUShared
	Pos core.Vec2
	Vel core.Vec2
}

type Material struct {
	ecs.GPUShared
	Tint      core.Vec4
	Roughness float32
	Metallic  float32
	ID        uint32
	Flags     int32
}

type NotShared struct{ X float32 }
`)
	names, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(names) != 2 || names[0] != "Material" || names[1] != "Particle" {
		t.Fatalf("names = %v", names)
	}
	out, err := os.ReadFile(filepath.Join(dir, "gfx_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	for _, want := range []string{
		"package demo",
		"const ParticleWGSL = `struct Particle {",
		"pos : vec2f,",
		"vel : vec2f,",
		"const MaterialWGSL = `struct Material {",
		"tint : vec4f,",
		"roughness : f32,",
		"unsafe.Offsetof(Particle{}.Vel) - 8",
		"unsafe.Sizeof(Particle{}) - 16",
		"unsafe.Sizeof(Material{}) - 32",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated file missing %q", want)
		}
	}
	if strings.Contains(src, "NotShared") {
		t.Error("unmarked struct leaked into output")
	}
}

func TestMisalignmentGetsActionableError(t *testing.T) {
	dir := t.TempDir()
	// f32 then vec2: WGSL wants the vec2 at 8, Go packs it at 4.
	write(t, dir, "comp.go", `package demo

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ecs"
)

type Bad struct {
	ecs.GPUShared
	Mass float32
	Pos  core.Vec2
}
`)
	_, err := Run(dir)
	if err == nil {
		t.Fatal("misaligned struct must fail generation")
	}
	msg := err.Error()
	if !strings.Contains(msg, "offset 8") || !strings.Contains(msg, "padding") {
		t.Fatalf("error not actionable: %v", err)
	}
}

func TestVec3Rejected(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "comp.go", `package demo

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ecs"
)

type Bad struct {
	ecs.GPUShared
	P core.Vec3
}
`)
	_, err := Run(dir)
	if err == nil || !strings.Contains(err.Error(), "vec3") {
		t.Fatalf("Vec3 should be rejected with guidance, got: %v", err)
	}
}

func TestTrailingPaddingError(t *testing.T) {
	dir := t.TempDir()
	// vec4 then f32: WGSL stride rounds to 32, Go size is 20.
	write(t, dir, "comp.go", `package demo

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ecs"
)

type Bad struct {
	ecs.GPUShared
	Tint core.Vec4
	Mass float32
}
`)
	_, err := Run(dir)
	if err == nil || !strings.Contains(err.Error(), "trailing padding") {
		t.Fatalf("want trailing padding error, got: %v", err)
	}
}

func TestImportAliasRespected(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "comp.go", `package demo

import (
	m "github.com/sushvibez/gfx/core"
	e "github.com/sushvibez/gfx/ecs"
)

type P struct {
	e.GPUShared
	Pos m.Vec2
	Vel m.Vec2
}
`)
	names, err := Run(dir)
	if err != nil {
		t.Fatalf("Run with aliases: %v", err)
	}
	if len(names) != 1 || names[0] != "P" {
		t.Fatalf("names = %v", names)
	}
}

// The generator reads the files the go tool would compile for the target,
// and no others: a file for another GOOS, one behind a tag that is not
// set, one marked ignore — each may declare the same type differently, or
// not be meant to compile at all.
func TestRunSelectsFilesLikeTheGoTool(t *testing.T) {
	dir := t.TempDir()
	const head = `package demo

import (
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ecs"
)

var _ core.Vec2
`
	write(t, dir, "comp.go", head+`
type Always struct {
	ecs.GPUShared
	X float32
}
`)
	write(t, dir, "plat_linux.go", head+`
type Plat struct {
	ecs.GPUShared
	OnLinux float32
}
`)
	write(t, dir, "plat_windows.go", head+`
type Plat struct {
	ecs.GPUShared
	OnWindows core.Vec4
}
`)
	write(t, dir, "plat_other.go", "//go:build !linux && !windows\n\n"+head+`
type Plat struct {
	ecs.GPUShared
	Elsewhere uint32
}
`)
	write(t, dir, "tagged.go", "//go:build special\n\n"+head+`
type Tagged struct {
	ecs.GPUShared
	T float32
}
`)
	// Not meant to compile: its field type is one the generator refuses.
	write(t, dir, "scratch.go", "//go:build ignore\n\n"+head+`
type Broken struct {
	ecs.GPUShared
	P *float32
}
`)
	for _, c := range []struct {
		target Target
		names  string
		field  string
	}{
		{Target{GOOS: "linux", GOARCH: "amd64"}, "Always Plat", "onLinux : f32"},
		{Target{GOOS: "windows", GOARCH: "amd64"}, "Always Plat", "onWindows : vec4f"},
		{Target{GOOS: "js", GOARCH: "wasm"}, "Always Plat", "elsewhere : u32"},
		{Target{GOOS: "linux", GOARCH: "amd64", Tags: []string{"special"}}, "Always Plat Tagged", "t : f32"},
	} {
		names, err := RunFor(dir, c.target)
		if err != nil {
			t.Errorf("%+v: %v", c.target, err)
			continue
		}
		if got := strings.Join(names, " "); got != c.names {
			t.Errorf("%+v: components %q, want %q", c.target, got, c.names)
		}
		out, err := os.ReadFile(filepath.Join(dir, "gfx_gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), c.field) {
			t.Errorf("%+v: the generated file has no %q:\n%s", c.target, c.field, out)
		}
	}
	// Run is RunFor with the tool's own target.
	if _, err := Run(dir); err != nil {
		t.Errorf("Run: %v", err)
	}
}
