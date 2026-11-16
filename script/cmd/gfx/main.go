// Gfx is the framework's build-time tool (DESIGN.md §6.2), driven by
// standard go generate:
//
//	//go:generate go run github.com/sushvibez/gfx/script/cmd/gfx gen
//
// `gfx gen` scans the package for structs embedding ecs.GPUShared and
// writes gfx_gen.go: WGSL struct declarations plus compile-time layout
// assertions. It reads the files the go command would compile: for the
// GOOS and GOARCH in the environment, with the build tags given by -tags.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/sushvibez/gfx/script/gen"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "gen" {
		fmt.Fprintln(os.Stderr, "usage: gfx gen [-tags a,b] [dir]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	tags := fs.String("tags", "", "comma-separated build tags the package is compiled with")
	fs.Parse(os.Args[2:])
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	var target gen.Target
	if *tags != "" {
		target.Tags = strings.Split(*tags, ",")
	}
	names, err := gen.RunFor(dir, target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("gfx gen: %v -> gfx_gen.go\n", names)
}
