// Gfx is the framework's build-time tool (DESIGN.md §6.2), driven by
// standard go generate:
//
//	//go:generate go run github.com/sushvibez/gfx/script/cmd/gfx gen
//
// `gfx gen` scans the package for structs embedding ecs.GPUShared and
// writes gfx_gen.go: WGSL struct declarations plus compile-time layout
// assertions.
package main

import (
	"fmt"
	"os"

	"github.com/sushvibez/gfx/script/gen"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "gen" {
		fmt.Fprintln(os.Stderr, "usage: gfx gen [dir]")
		os.Exit(2)
	}
	dir := "."
	if len(os.Args) > 2 {
		dir = os.Args[2]
	}
	names, err := gen.Run(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("gfx gen: %v -> gfx_gen.go\n", names)
}
