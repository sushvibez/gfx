// Package gen implements `gfx gen`: the go-generate-driven code generator
// (DESIGN.md §6.2). It scans a package for structs embedding
// ecs.GPUShared, validates that their Go memory layout matches WGSL's
// alignment rules, and emits the WGSL struct declarations plus
// compile-time layout assertions.
//
// Standard Go machinery only: embedding marks types, `go generate` drives
// the tool, and the output is plain Go the compiler checks forever after.
package gen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// wgslType describes one supported field type in both layout systems.
type wgslType struct {
	wgsl      string
	size      int // identical in Go and WGSL for all supported types
	wgslAlign int
	goAlign   int
}

// types the generator accepts. WGSL alignments per the WGSL spec; Go
// alignments per the gc ABI (all 4 for these types).
var typeMap = map[string]wgslType{
	"float32":   {"f32", 4, 4, 4},
	"int32":     {"i32", 4, 4, 4},
	"uint32":    {"u32", 4, 4, 4},
	"core.Vec2": {"vec2f", 8, 8, 4},
	"core.Vec4": {"vec4f", 16, 16, 4},
	"core.Mat4": {"mat4x4f", 64, 16, 4},
}

// component is one discovered GPU-shared struct.
type component struct {
	name   string
	fields []field
	pos    token.Position
}

type field struct {
	goName  string
	typName string // key into typeMap
}

const generatedFile = "gfx_gen.go"

// Run generates gfx_gen.go for the package in dir. It returns the names
// of the components processed.
func Run(dir string) ([]string, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go") && fi.Name() != generatedFile
	}, 0)
	if err != nil {
		return nil, err
	}

	var pkgName string
	var comps []component
	for name, pkg := range pkgs {
		found, err := scan(fset, pkg)
		if err != nil {
			return nil, err
		}
		if len(found) > 0 {
			if pkgName != "" && pkgName != name {
				return nil, fmt.Errorf("gfx gen: GPUShared types in multiple packages (%s, %s)", pkgName, name)
			}
			pkgName = name
			comps = append(comps, found...)
		}
	}
	if len(comps) == 0 {
		return nil, fmt.Errorf("gfx gen: no structs embedding ecs.GPUShared in %s", dir)
	}
	sort.Slice(comps, func(i, j int) bool { return comps[i].name < comps[j].name })

	src, err := emit(pkgName, comps)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, generatedFile), []byte(src), 0o644); err != nil {
		return nil, err
	}
	names := make([]string, len(comps))
	for i, c := range comps {
		names[i] = c.name
	}
	return names, nil
}

// scan finds GPUShared structs in a parsed package, resolving the import
// aliases used for the ecs and core packages per file.
func scan(fset *token.FileSet, pkg *ast.Package) ([]component, error) {
	var out []component
	for _, file := range pkg.Files {
		ecsAlias, coreAlias := importAliases(file)
		if ecsAlias == "" {
			continue
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok || !embedsMarker(st, ecsAlias) {
					continue
				}
				c := component{name: ts.Name.Name, pos: fset.Position(ts.Pos())}
				for _, f := range st.Fields.List {
					if isMarker(f, ecsAlias) {
						continue
					}
					tn, err := typeName(f.Type, coreAlias)
					if err != nil {
						return nil, fmt.Errorf("%s: %s: %w", fset.Position(f.Pos()), c.name, err)
					}
					if len(f.Names) == 0 {
						return nil, fmt.Errorf("%s: %s: embedded fields other than ecs.GPUShared are not supported", fset.Position(f.Pos()), c.name)
					}
					for _, n := range f.Names {
						c.fields = append(c.fields, field{goName: n.Name, typName: tn})
					}
				}
				if len(c.fields) == 0 {
					return nil, fmt.Errorf("%s: %s has no fields besides the marker", c.pos, c.name)
				}
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func importAliases(file *ast.File) (ecsAlias, coreAlias string) {
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		switch path {
		case "github.com/sushvibez/gfx/ecs":
			ecsAlias = name
		case "github.com/sushvibez/gfx/core":
			coreAlias = name
		}
	}
	return
}

func isMarker(f *ast.Field, ecsAlias string) bool {
	if len(f.Names) != 0 {
		return false
	}
	sel, ok := f.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	base, ok := sel.X.(*ast.Ident)
	return ok && base.Name == ecsAlias && sel.Sel.Name == "GPUShared"
}

func embedsMarker(st *ast.StructType, ecsAlias string) bool {
	for _, f := range st.Fields.List {
		if isMarker(f, ecsAlias) {
			return true
		}
	}
	return false
}

func typeName(e ast.Expr, coreAlias string) (string, error) {
	switch t := e.(type) {
	case *ast.Ident:
		if _, ok := typeMap[t.Name]; ok {
			return t.Name, nil
		}
		return "", fmt.Errorf("unsupported field type %q (supported: float32, int32, uint32, core.Vec2, core.Vec4, core.Mat4)", t.Name)
	case *ast.SelectorExpr:
		base, ok := t.X.(*ast.Ident)
		if !ok || base.Name != coreAlias {
			return "", fmt.Errorf("unsupported field type")
		}
		name := "core." + t.Sel.Name
		if t.Sel.Name == "Vec3" {
			return "", fmt.Errorf("core.Vec3 is not GPU-sharable: WGSL aligns vec3 to 16 bytes, Go packs it to 12 — use core.Vec4 or a Vec2+float32 pair")
		}
		if _, ok := typeMap[name]; ok {
			return name, nil
		}
		return "", fmt.Errorf("unsupported field type %q", name)
	}
	return "", fmt.Errorf("unsupported field type (arrays, pointers and nested structs are not GPU-sharable in v0)")
}

func roundUp(v, align int) int { return (v + align - 1) / align * align }

// layout computes WGSL and Go offsets for every field and the strides,
// erroring with a concrete fix when they disagree.
func layout(c component) (wgslOffsets, goOffsets []int, stride int, err error) {
	wgslOff, goOff := 0, 0
	maxAlign := 4
	for _, f := range c.fields {
		t := typeMap[f.typName]
		w := roundUp(wgslOff, t.wgslAlign)
		g := roundUp(goOff, t.goAlign)
		if w != g {
			return nil, nil, 0, fmt.Errorf(
				"%s: %s.%s: WGSL puts it at offset %d but Go packs it at %d — insert %d bytes of explicit padding (e.g. `_ [%d]float32`) before this field",
				c.pos, c.name, f.goName, w, g, w-g, (w-g)/4)
		}
		wgslOffsets = append(wgslOffsets, w)
		goOffsets = append(goOffsets, g)
		wgslOff, goOff = w+t.size, g+t.size
		if t.wgslAlign > maxAlign {
			maxAlign = t.wgslAlign
		}
	}
	stride = roundUp(wgslOff, maxAlign)
	if stride != goOff {
		return nil, nil, 0, fmt.Errorf(
			"%s: %s: WGSL stride is %d but Go size is %d — add %d bytes of trailing padding (e.g. `_ [%d]float32`)",
			c.pos, c.name, stride, goOff, stride-goOff, (stride-goOff)/4)
	}
	return wgslOffsets, goOffsets, stride, nil
}

// lowerCamel converts a Go field name to its WGSL spelling.
func lowerCamel(s string) string {
	return strings.ToLower(s[:1]) + s[1:]
}

func emit(pkgName string, comps []component) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by gfx gen. DO NOT EDIT.\n\npackage %s\n\nimport \"unsafe\"\n", pkgName)

	for _, c := range comps {
		wgslOffs, _, stride, err := layout(c)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(&b, "\n// %sWGSL declares %s for WGSL shaders; layout verified below.\nconst %sWGSL = `struct %s {\n", c.name, c.name, c.name, c.name)
		for _, f := range c.fields {
			fmt.Fprintf(&b, "    %s : %s,\n", lowerCamel(f.goName), typeMap[f.typName].wgsl)
		}
		fmt.Fprintf(&b, "}`\n")

		fmt.Fprintf(&b, "\n// If %s's Go layout drifts from the WGSL layout, these constants\n// go negative and compilation fails.\nconst (\n", c.name)
		for i, f := range c.fields {
			fmt.Fprintf(&b, "\t_ = uint(unsafe.Offsetof(%s{}.%s) - %d)\n", c.name, f.goName, wgslOffs[i])
			fmt.Fprintf(&b, "\t_ = uint(%d - unsafe.Offsetof(%s{}.%s))\n", wgslOffs[i], c.name, f.goName)
		}
		fmt.Fprintf(&b, "\t_ = uint(unsafe.Sizeof(%s{}) - %d)\n", c.name, stride)
		fmt.Fprintf(&b, "\t_ = uint(%d - unsafe.Sizeof(%s{}))\n", stride, c.name)
		fmt.Fprintf(&b, ")\n")
	}
	return b.String(), nil
}
