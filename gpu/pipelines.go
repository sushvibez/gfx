package gpu

import (
	"runtime"
	"sync"
)

// CreateRenderPipelines makes several pipelines at once, each on a
// goroutine of its own (at most one per CPU). Creating a pipeline is the
// slow part of a renderer's start — the driver compiles the shader for
// it, seconds for a big WGSL module on a cold shader cache — and the
// compiles are independent, so they run in parallel; on js/wasm, with one
// thread, they run one after another as before. The result is in the
// order of descs; the first error is returned with whatever was made
// (nil where a pipeline failed).
func (d *Device) CreateRenderPipelines(descs []*RenderPipelineDesc) ([]*RenderPipeline, error) {
	out := make([]*RenderPipeline, len(descs))
	errs := make([]error, len(descs))
	sem := make(chan struct{}, max(runtime.NumCPU(), 1))
	var wg sync.WaitGroup
	for i, rp := range descs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i], errs[i] = d.CreateRenderPipeline(rp)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
