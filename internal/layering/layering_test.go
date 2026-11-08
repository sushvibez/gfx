// Package layering enforces the framework's downward-only dependency rule.
//
// The framework used to be one Go module per layer, which made the module
// graph itself the enforcement: a package could not import a layer above
// it without that layer's module requiring it back. Now that the framework
// is a single module, this test is the fence. It asks `go list` for every
// package's imports and fails on any import of a layer with an equal or
// higher rank, so `ui` can use `render` but `render` can never reach for
// `ui`.
package layering

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/sushvibez/gfx/"

// ranks orders the layers (DESIGN.md §2). A package may import only
// packages whose top-level directory has a strictly lower rank, or its own.
var ranks = map[string]int{
	"core":     0,
	"mem":      0,
	"ffi":      0,
	"app":      1, // windowing over ffi
	"physics":  1, // pure math over core
	"audio":    1, // decoders + backends over ffi
	"gpu":      2, // gpu/plat makes surfaces from app windows
	"ecs":      3, // GPU-shared columns
	"render":   3,
	"scene":    4,
	"ui":       4,
	"md":       5, // markdown parser, layout and ui element
	"script":   6, // the .gml engine
	"internal": 7, // this test
}

// skipDirs are repository directories that hold no framework packages:
// examples/ is its own module and the rest carry no Go code. Everything
// else at the top level must be in the ranks table.
var skipDirs = map[string]bool{"build": true, "docs": true, "scripts": true, "examples": true}

func TestDownwardOnly(t *testing.T) {
	// List the layers explicitly rather than ./..., which would also walk
	// build/ — where a concurrent `go test` under GOTMPDIR creates and
	// deletes directories while this test runs.
	entries, err := os.ReadDir("../..")
	if err != nil {
		t.Fatal(err)
	}
	var patterns []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || skipDirs[name] {
			continue
		}
		if _, ok := ranks[name]; !ok {
			t.Errorf("top-level directory %q has no rank; add it to the table (or to skipDirs)", name)
			continue
		}
		patterns = append(patterns, "./"+name+"/...")
	}
	args := append([]string{"list", "-f", "{{.ImportPath}} {{join .Imports \" \"}} {{join .TestImports \" \"}}"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = "../.."
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		from, fromTop := fields[0], top(fields[0])
		fromRank, ok := ranks[fromTop]
		if !ok {
			t.Errorf("%s: top-level directory %q has no rank; add it to the table", from, fromTop)
			continue
		}
		seen[fromTop] = true
		for _, imp := range fields[1:] {
			if !strings.HasPrefix(imp, modulePath) {
				continue
			}
			toTop := top(imp)
			if toTop == fromTop {
				continue
			}
			toRank, ok := ranks[toTop]
			if !ok {
				t.Errorf("%s imports %s: top-level directory %q has no rank", from, imp, toTop)
				continue
			}
			if toRank >= fromRank {
				t.Errorf("%s (layer %d) imports %s (layer %d): dependencies point downward only", from, fromRank, imp, toRank)
			}
		}
	}
	for layer := range ranks {
		if !seen[layer] {
			t.Errorf("layer %q is in the table but has no packages", layer)
		}
	}
}

func top(importPath string) string {
	rest := strings.TrimPrefix(importPath, modulePath)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}
