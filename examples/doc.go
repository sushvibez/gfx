// Package examples holds the runnable samples, one directory per program,
// each a main package that runs natively, headlessly (-screenshot out.png)
// and, where the sample applies, on the web and Android.
//
// The examples are a separate Go module (github.com/sushvibez/gfx/examples)
// that replaces the framework with ../, so the framework's own go.mod
// carries no demo-only dependencies. Run them from this directory:
//
//	cd examples
//	go run ./kitchensink
//	go run ./kitchensink -screenshot out.png
//	go run ./qml -file qml/gpui/hello_world.gml
//
// docs/getting-started.md lists every example with what it demonstrates.
package examples
