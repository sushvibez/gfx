//go:build js

package gpu

import "errors"

// errNoReadback is what GPU readback returns in the browser: WebGPU there
// maps buffers asynchronously, and the blocking readback the native side
// offers (for screenshots, goldens and tests) cannot be built on it from
// a single-threaded wasm program.
var errNoReadback = errors.New("gpu: readback is native-only (screenshots and tests run natively)")

// ReadTexture is native-only; on the web it returns an error, so code with
// a screenshot path builds for the browser too.
func (d *Device) ReadTexture(q *Queue, t *Texture, width, height int) ([]byte, error) {
	return nil, errNoReadback
}
