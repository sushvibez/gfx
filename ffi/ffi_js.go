//go:build js

package ffi

import "errors"

// On the web there are no native libraries to load: platform and GPU access
// go through syscall/js in the app and gpu modules. This stub exists so code
// that conditionally probes for native libraries still compiles.

var errUnsupported = errors.New("ffi: native libraries are not available on js/wasm")

type Lib struct{}

func Open(names ...string) (*Lib, error)        { return nil, errUnsupported }
func (l *Lib) Name() string                     { return "" }
func (l *Lib) Sym(name string) (uintptr, error) { return 0, errUnsupported }
func (l *Lib) Bind(fptr any, name string)       { panic(errUnsupported) }
func (l *Lib) Close() error                     { return nil }

func Callback(fn any) uintptr { panic(errUnsupported) }
