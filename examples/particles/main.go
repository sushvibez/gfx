//go:build linux || windows || js

// Particles is the phase-2 exit demo (DESIGN.md §12): one million
// particles seeded through the ECS, integrated by a compute shader
// entirely on the GPU, and rendered as instanced quads pulled from the
// same storage buffer — zero per-frame CPU work on particle data. Move
// the mouse to attract, hold the button to repel.
//
//	go run ./particles
//	go run ./particles -n 2000000 -frames 600
//	go run ./particles -screenshot out.png
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"
	"unsafe"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ecs"
	"github.com/sushvibez/gfx/examples/internal/shot"
	"github.com/sushvibez/gfx/gpu"
	"github.com/sushvibez/gfx/gpu/plat"
)

//go:generate go run github.com/sushvibez/gfx/script/cmd/gfx gen

// Particle is the GPU-shared component: gfx gen emits ParticleWGSL and
// the layout assertions in gfx_gen.go from this declaration.
type Particle struct {
	ecs.GPUShared
	Pos core.Vec2
	Vel core.Vec2
}

// params mirrors the WGSL Params struct (32 bytes).
type params struct {
	DT       float32
	W, H     float32
	Strength float32
	MX, MY   float32
	XStride  float32
	_        float32
}

const computeWGSL = ParticleWGSL + `
struct Params { dt : f32, w : f32, h : f32, strength : f32, mouse : vec2f, xstride : f32, pad : f32 }
@group(0) @binding(0) var<storage, read_write> parts : array<Particle>;
@group(0) @binding(1) var<storage, read> P : Params;

@compute @workgroup_size(64)
fn step(@builtin(global_invocation_id) id : vec3u) {
    // Dispatch is a 2D grid: one dimension maxes out at 65535 workgroups.
    let i = id.y * u32(P.xstride) + id.x;
    if (i >= arrayLength(&parts)) { return; }
    var p = parts[i];
    let d = P.mouse - p.pos;
    let dist = max(length(d), 40.0);
    p.vel += (d / dist) * (P.strength * 8000.0 / dist) * P.dt;
    p.vel *= 0.995;
    p.pos += p.vel * P.dt;
    if (p.pos.x < 0.0) { p.pos.x = 0.0; p.vel.x = -p.vel.x; }
    if (p.pos.x > P.w) { p.pos.x = P.w; p.vel.x = -p.vel.x; }
    if (p.pos.y < 0.0) { p.pos.y = 0.0; p.vel.y = -p.vel.y; }
    if (p.pos.y > P.h) { p.pos.y = P.h; p.vel.y = -p.vel.y; }
    parts[i] = p;
}`

const renderWGSL = ParticleWGSL + `
struct Params { dt : f32, w : f32, h : f32, strength : f32, mouse : vec2f, xstride : f32, pad : f32 }
@group(0) @binding(0) var<storage, read> parts : array<Particle>;
@group(0) @binding(1) var<storage, read> P : Params;

struct VSOut {
    @builtin(position) pos : vec4f,
    @location(0) @interpolate(flat) speed : f32,
}

@vertex
fn vs(@builtin(vertex_index) vid : u32, @builtin(instance_index) iid : u32) -> VSOut {
    var units = array<vec2f, 6>(
        vec2f(0.0, 0.0), vec2f(1.0, 0.0), vec2f(1.0, 1.0),
        vec2f(0.0, 0.0), vec2f(1.0, 1.0), vec2f(0.0, 1.0),
    );
    let p = parts[iid];
    let corner = p.pos + (units[vid] - vec2f(0.5)) * 1.6;
    var out : VSOut;
    out.pos = vec4f(
        corner.x / P.w * 2.0 - 1.0,
        1.0 - corner.y / P.h * 2.0,
        0.0, 1.0);
    out.speed = length(p.vel);
    return out;
}

@fragment
fn fs(in : VSOut) -> @location(0) vec4f {
    let t = clamp(in.speed / 320.0, 0.0, 1.0);
    // slow: deep blue -> fast: hot orange
    let c = mix(vec3f(0.15, 0.3, 0.9), vec3f(1.0, 0.55, 0.1), t);
    return vec4f(c, 0.6);
}`

func main() {
	n := flag.Int("n", 1_000_000, "particle count")
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	screenshot := flag.String("screenshot", "", "simulate 60 frames headlessly and write the last as a PNG")
	flag.Parse()
	var err error
	if *screenshot != "" {
		const W, H = 1024, 768
		err = shot.Render(W, H, 60, *screenshot, func(d *gpu.Device, q *gpu.Queue, f gpu.TextureFormat) (func(*gpu.TextureView), error) {
			s, err := newSim(d, q, f, *n, W, H)
			if err != nil {
				return nil, err
			}
			// A fixed 60 Hz step with the pointer at the centre: the same
			// picture every run.
			return func(v *gpu.TextureView) { s.frame(v, W, H, 1.0/60, W/2, H/2, 1) }, nil
		})
	} else {
		err = run(*n, *frames)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "particles:", err)
		os.Exit(1)
	}
}

// sim is the particle system's GPU state: the ECS column the compute pass
// steps, the parameters, and the two pipelines.
type sim struct {
	device             *gpu.Device
	queue              *gpu.Queue
	n, gx, gy          int
	col                *ecs.GPUColumn[Particle]
	pbuf               *gpu.Buffer
	stepPipe           *gpu.ComputePipeline
	drawPipe           *gpu.RenderPipeline
	stepBind, drawBind *gpu.BindGroup
}

// newSim seeds n particles over a w×h area and builds the pipelines for a
// target of the given format.
func newSim(device *gpu.Device, queue *gpu.Queue, format gpu.TextureFormat, n, w, h int) (*sim, error) {
	s := &sim{device: device, queue: queue, n: n}
	// Seed the particles through the ECS; the column uploads once and the
	// GPU owns it from then on.
	world := ecs.NewWorld()
	parts := ecs.NewComponent[Particle](world)
	rng := rand.New(rand.NewSource(42))
	for range n {
		e := world.Spawn()
		parts.Add(e, Particle{
			Pos: core.V2(rng.Float32()*float32(w), rng.Float32()*float32(h)),
			Vel: core.V2((rng.Float32()-0.5)*60, (rng.Float32()-0.5)*60),
		})
	}
	s.col = ecs.NewGPUColumn(device, parts, 0)
	s.col.Sync(queue)

	s.pbuf = device.CreateBuffer(gpu.BufferStorage|gpu.BufferCopyDst, int(unsafe.Sizeof(params{})))

	cm, err := device.CreateShaderModuleWGSL("particles-step", computeWGSL)
	if err != nil {
		return nil, err
	}
	step, err := device.CreateComputePipeline(cm, "step")
	if err != nil {
		return nil, err
	}
	cm.Release()
	s.stepBind = device.CreateBindGroup(step.BindGroupLayout(0),
		gpu.BindGroupEntry{Binding: 0, Buffer: s.col.Buffer()},
		gpu.BindGroupEntry{Binding: 1, Buffer: s.pbuf},
	)
	s.stepPipe = step

	rm, err := device.CreateShaderModuleWGSL("particles-draw", renderWGSL)
	if err != nil {
		return nil, err
	}
	draw, err := device.CreateRenderPipeline(&gpu.RenderPipelineDesc{
		Shader: rm, VertexEntry: "vs", FragmentEntry: "fs",
		Format: format, AlphaBlend: true,
	})
	if err != nil {
		return nil, err
	}
	rm.Release()
	s.drawBind = device.CreateBindGroup(draw.BindGroupLayout(0),
		gpu.BindGroupEntry{Binding: 0, Buffer: s.col.Buffer()},
		gpu.BindGroupEntry{Binding: 1, Buffer: s.pbuf},
	)
	s.drawPipe = draw

	// 2D dispatch grid: WebGPU caps each dimension at 65535 workgroups.
	groups := (n + 63) / 64
	s.gx = min(groups, 65535)
	s.gy = (groups + s.gx - 1) / s.gx
	return s, nil
}

// frame steps the particles by dt towards (mx, my) (strength < 0 repels)
// and draws them into view.
func (s *sim) frame(view *gpu.TextureView, w, h int, dt, mx, my, strength float32) {
	p := params{DT: dt, W: float32(w), H: float32(h), Strength: strength, MX: mx, MY: my, XStride: float32(s.gx * 64)}
	s.queue.WriteBuffer(s.pbuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&p)), unsafe.Sizeof(p)))
	enc := s.device.CreateCommandEncoder()
	cp := enc.BeginComputePass()
	cp.SetPipeline(s.stepPipe)
	cp.SetBindGroup(0, s.stepBind)
	cp.Dispatch(s.gx, s.gy, 1)
	cp.End()
	rp := enc.BeginRenderPass(view, gpu.Color{R: 0.02, G: 0.02, B: 0.04, A: 1})
	rp.SetPipeline(s.drawPipe)
	rp.SetBindGroup(0, s.drawBind)
	rp.Draw(6, s.n, 0, 0)
	rp.End()
	s.queue.Submit(enc.Finish())
}

func (s *sim) release() {
	s.col.Release()
	s.pbuf.Release()
}

func run(n, maxFrames int) error {
	win, err := app.NewWindow(app.Options{Title: "graphics: particles", Width: 1024, Height: 768})
	if err != nil {
		return err
	}
	defer win.Destroy()
	inst, err := gpu.NewInstance(plat.LibPaths()...)
	if err != nil {
		return err
	}
	surface, err := plat.NewSurface(inst, win)
	if err != nil {
		return err
	}
	adapter, err := inst.RequestAdapter(surface)
	if err != nil {
		return err
	}
	device, err := adapter.RequestDevice()
	if err != nil {
		return err
	}
	queue := device.Queue()
	format := surface.PreferredFormat(adapter)
	w, h := win.Size()
	surface.Configure(device, w, h, format)

	s, err := newSim(device, queue, format, n, w, h)
	if err != nil {
		return err
	}
	defer s.release()

	var mx, my float32 = float32(w) / 2, float32(h) / 2
	strength := float32(1)
	rendered := 0
	start := time.Now()
	last := start

	win.EachFrame(func() bool {
		for _, m := range win.MouseEvents() {
			switch m.Kind {
			case app.MouseMove:
				mx, my = m.X, m.Y
			case app.MouseDown:
				strength = -2 // hold to repel
			case app.MouseUp:
				strength = 1
			}
		}
		if nw, nh := win.Size(); nw != w || nh != h {
			w, h = nw, nh
			surface.Configure(device, w, h, format)
		}
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		last = now
		dt = min(dt, 1.0/30)

		tex, ok := surface.CurrentTexture()
		if !ok {
			surface.Configure(device, w, h, format)
			return true
		}
		view := tex.CreateView()
		s.frame(view, w, h, dt, mx, my, strength)
		surface.Present()

		view.Release()
		tex.Release()
		rendered++
		return maxFrames == 0 || rendered < maxFrames
	})

	elapsed := time.Since(start).Seconds()
	fmt.Printf("%d particles, %d frames in %.1fs = %.1f fps\n",
		n, rendered, elapsed, float64(rendered)/elapsed)
	return nil
}
