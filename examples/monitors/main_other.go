//go:build !linux && !windows

// Monitor enumeration and window placement are a desktop concern; this
// example builds (and does nothing) elsewhere so `go build ./...` stays green
// for every GOOS, wasm included.
package main

import "fmt"

func main() { fmt.Println("monitors: desktop-only example (linux/windows)") }
