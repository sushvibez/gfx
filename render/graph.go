package render

import (
	"fmt"

	"github.com/sushvibez/gfx/gpu"
)

// Graph is the render graph (DESIGN.md §9, Unreal-RDG style, declared per
// frame): passes name what they read and write, the graph orders them by
// those dependencies, culls passes that never reach a presented target,
// pools transient textures across frames, and records everything into one
// command submission.
//
//	g := render.NewGraph(device, queue)
//	scene := g.Texture("scene", w, h)
//	g.Pass("scene", scene, clear, func(p *gpu.RenderPass) { ... })
//	g.PassToTarget("final", clear, func(p *gpu.RenderPass) { ... }, scene)
//	g.Execute(surfaceView)
type Graph struct {
	device *gpu.Device
	queue  *gpu.Queue
	pool   map[texKey][]*gpu.Texture // transient textures reused frame to frame
	texes  []*GraphTexture
	passes []*graphPass
}

type texKey struct {
	w, h   int
	format gpu.TextureFormat
}

// GraphTexture is a transient render-graph resource. Its GPU texture is
// only valid during Execute, after allocation.
type GraphTexture struct {
	name   string
	key    texKey
	tex    *gpu.Texture
	view   *gpu.TextureView
	writer int // index of the pass that renders it, -1 if none
}

// Texture reports the resolved GPU texture during pass execution.
func (t *GraphTexture) Texture() *gpu.Texture { return t.tex }

// View reports the resolved texture view during pass execution.
func (t *GraphTexture) View() *gpu.TextureView { return t.view }

type graphPass struct {
	name   string
	target *GraphTexture // nil = the presented target
	clear  gpu.Color
	reads  []*GraphTexture
	fn     func(*gpu.RenderPass)
}

func NewGraph(device *gpu.Device, queue *gpu.Queue) *Graph {
	return &Graph{device: device, queue: queue, pool: map[texKey][]*gpu.Texture{}}
}

// Reset clears the frame's declarations; call once per frame before
// declaring passes (Execute does it on completion as well).
func (g *Graph) Reset() {
	g.texes = g.texes[:0]
	g.passes = g.passes[:0]
}

// Texture declares a transient RGBA8 render target for this frame.
func (g *Graph) Texture(name string, w, h int) *GraphTexture {
	t := &GraphTexture{name: name, key: texKey{w, h, gpu.FormatRGBA8Unorm}, writer: -1}
	g.texes = append(g.texes, t)
	return t
}

// Pass declares a pass rendering into a transient texture. reads are the
// graph textures the pass samples; they order it after their writers.
//
// Resource lifetime: GPU objects a pass callback records (bind groups,
// images) must stay alive until Execute returns — Execute submits all
// passes as one command buffer.
func (g *Graph) Pass(name string, target *GraphTexture, clear gpu.Color, fn func(*gpu.RenderPass), reads ...*GraphTexture) {
	if target != nil {
		target.writer = len(g.passes)
	}
	g.passes = append(g.passes, &graphPass{name: name, target: target, clear: clear, reads: reads, fn: fn})
}

// PassToTarget declares a pass rendering into the presented target (the
// texture view handed to Execute).
func (g *Graph) PassToTarget(name string, clear gpu.Color, fn func(*gpu.RenderPass), reads ...*GraphTexture) {
	g.Pass(name, nil, clear, fn, reads...)
}

// Execute culls, orders and records the declared passes into one
// submission targeting view, then resets the graph for the next frame.
func (g *Graph) Execute(view *gpu.TextureView) error {
	defer g.Reset()

	// Cull: keep passes that (transitively) feed a presented-target pass.
	keep := make([]bool, len(g.passes))
	var mark func(i int)
	mark = func(i int) {
		if i < 0 || keep[i] {
			return
		}
		keep[i] = true
		for _, r := range g.passes[i].reads {
			mark(r.writer)
		}
	}
	for i, p := range g.passes {
		if p.target == nil {
			mark(i)
		}
	}

	// Order: writers before readers. Declaration order is already almost
	// topological for typical graphs; sort by repeated relaxation to stay
	// dependency-correct regardless of declaration order.
	order := make([]int, 0, len(g.passes))
	done := make([]bool, len(g.passes))
	for len(order) < countTrue(keep) {
		progressed := false
		for i, p := range g.passes {
			if !keep[i] || done[i] {
				continue
			}
			ready := true
			for _, r := range p.reads {
				if r.writer >= 0 && keep[r.writer] && !done[r.writer] {
					ready = false
					break
				}
			}
			if ready {
				order = append(order, i)
				done[i] = true
				progressed = true
			}
		}
		if !progressed {
			return fmt.Errorf("render: pass dependency cycle")
		}
	}

	// Allocate transient textures for surviving writers from the pool.
	for _, t := range g.texes {
		if t.writer < 0 || !keep[t.writer] {
			continue
		}
		if free := g.pool[t.key]; len(free) > 0 {
			t.tex = free[len(free)-1]
			g.pool[t.key] = free[:len(free)-1]
		} else {
			t.tex = g.device.CreateTexture2D(t.key.w, t.key.h, t.key.format,
				gpu.TextureRenderAttachment|gpu.TextureBinding|gpu.TextureCopySrc)
		}
		t.view = t.tex.CreateView()
	}
	defer func() {
		for _, t := range g.texes {
			if t.tex != nil {
				t.view.Release()
				t.view = nil
				g.pool[t.key] = append(g.pool[t.key], t.tex) // recycle
				t.tex = nil
			}
		}
	}()

	enc := g.device.CreateCommandEncoder()
	for _, i := range order {
		p := g.passes[i]
		target := view
		if p.target != nil {
			target = p.target.view
		}
		rp := enc.BeginRenderPass(target, p.clear)
		p.fn(rp)
		rp.End()
	}
	g.queue.Submit(enc.Finish())
	return nil
}

// Release frees the pooled transient textures.
func (g *Graph) Release() {
	for _, list := range g.pool {
		for _, t := range list {
			t.Release()
		}
	}
	g.pool = map[texKey][]*gpu.Texture{}
}

func countTrue(b []bool) int {
	n := 0
	for _, v := range b {
		if v {
			n++
		}
	}
	return n
}
