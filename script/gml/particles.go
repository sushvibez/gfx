package gml

// QtQuick.Particles: ParticleSystem with Emitter, ImageParticle and the
// direction/affector pieces the corpus uses. This is a real simulation —
// emitters spawn particles at their rate, each carries a velocity,
// acceleration, size and life span, and the system advances them on the
// frame tick and draws them as soft round sprites.
//
// Modeled: emitRate, lifeSpan (+Variation), size/endSize (+Variation),
// velocity/acceleration via AngleDirection / PointDirection / TargetDirection,
// emitter geometry (its own rect), enabled/running, groups, and
// ImageParticle's source/color/colorVariation/alpha/entryEffect fade. Not
// modeled: TrailEmitter
// following another group, Affectors beyond gravity-style acceleration, and
// sprite animation.

import (
	"fmt"
	"github.com/sushvibez/gfx/core"
	"math"
	"math/rand"
	"os"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// particle is one live particle in a system.
type particle struct {
	group   string
	x, y    float32
	vx, vy  float32
	ax, ay  float32
	size    float32
	endSize float32
	born    float64 // system time at birth (seconds)
	life    float64 // seconds
	color   ui.RGBA
	fadeIn  float64 // seconds of entry fade
	tex     string  // the ImageParticle source this particle draws with
}

// particleSystem is the retained simulation behind a ParticleSystem element.
type particleSystem struct {
	now        float64 // seconds since the system started
	parts      []particle
	pending    map[string]float64 // emitter key -> fractional particles owed
	rng        *rand.Rand
	lastRect   ui.Rect
	generation int
	// An ImageParticle's texture: the encoded bytes read at build time and
	// the GPU image decoded on first paint. QML particles are a SPRITE —
	// the corpus' particle.png is a soft radial glow — and drawing a hard
	// dot instead was most of the particles page's difference.
	texData map[string][]byte
	texGPU  map[string]*render.Image
}

// advance steps the simulation by dt seconds: it ages particles out, moves
// the survivors, and lets each emitter spawn its share.
func (s *particleSystem) advance(dt float64, emitters []emitterSpec, painters []particlePainter, rect ui.Rect) {
	if dt <= 0 {
		return
	}
	s.now += dt
	s.lastRect = rect
	// Age out, then integrate. (One pass, compacting in place.)
	live := s.parts[:0]
	for _, p := range s.parts {
		if s.now-p.born >= p.life {
			continue
		}
		p.vx += p.ax * float32(dt)
		p.vy += p.ay * float32(dt)
		p.x += p.vx * float32(dt)
		p.y += p.vy * float32(dt)
		live = append(live, p)
	}
	s.parts = live

	for i, e := range emitters {
		if !e.enabled || e.rate <= 0 {
			continue
		}
		key := fmt.Sprintf("%d", i)
		s.pending[key] += e.rate * dt
		n := int(s.pending[key])
		if n <= 0 {
			continue
		}
		s.pending[key] -= float64(n)
		for k := 0; k < n && len(s.parts) < 4000; k++ {
			s.parts = append(s.parts, s.spawn(e, painters, rect))
		}
	}
}

// spawn makes one particle for an emitter, applying its variations.
func (s *particleSystem) spawn(e emitterSpec, painters []particlePainter, rect ui.Rect) particle {
	vary := func(base, variation float32) float32 {
		if variation == 0 {
			return base
		}
		return base + variation*(2*s.rng.Float32()-1)
	}
	// Emitter geometry: particles start somewhere inside its rect.
	w, h := e.w, e.h
	if e.fillSys {
		w, h = max(w, rect.W), max(h, rect.H)
	}
	x := e.x + s.rng.Float32()*max(w, 0)
	y := e.y + s.rng.Float32()*max(h, 0)
	angle := float64(vary(e.angle, e.angleVariation)) * math.Pi / 180
	mag := float64(vary(e.magnitude, e.magnitudeVariation))
	life := float64(vary(e.lifeSpan, e.lifeSpanVariation)) / 1000
	if life <= 0 {
		life = 1
	}
	size := max(vary(e.size, e.sizeVariation), 1)
	endSize := size
	if e.endSize >= 0 {
		endSize = max(e.endSize, 0)
	}
	p := particle{
		group:   e.group,
		x:       x,
		y:       y,
		vx:      float32(math.Cos(angle) * mag),
		vy:      float32(math.Sin(angle) * mag),
		ax:      e.accelX,
		ay:      e.accelY,
		size:    size,
		endSize: endSize,
		born:    s.now,
		life:    life,
		color:   ui.RGB(0xffffff),
		fadeIn:  0.15,
	}
	// The ImageParticle for this group decides the colour.
	for _, pt := range painters {
		if pt.matches(p.group) {
			c := pt.color
			if pt.colorVariation > 0 {
				// QML's colorVariation LERPS each channel toward a fresh
				// uniform random one — at 1.0 the colour is fully random
				// whatever the base was. Probed against Qt with a base
				// whose red is zero (#00D1A9) at variation 0/0.3/0.6/1.0:
				// its mean red rose 0.00, 0.14, 0.30, 0.44 of full scale
				// (i.e. v/2, the mean of the random channel) while green
				// fell from 0.82 to 0.49 — a signed jitter around the base
				// (what we did) leaves half the red clamped at zero and
				// keeps green near its base, which is why our particles
				// were short of Qt's pinks.
				v := pt.colorVariation
				j := func(base float32) float32 {
					return min(max(base*(1-v)+v*s.rng.Float32(), 0), 1)
				}
				c = ui.RGBA{R: j(c.R), G: j(c.G), B: j(c.B), A: c.A}
			}
			c.A *= pt.alpha
			p.color = c
			p.tex = pt.source
			break
		}
	}
	return p
}

// draw paints every live particle as a soft dot, fading in at birth and out
// toward the end of its life.
func (s *particleSystem) draw(r *render.Renderer2D, rect ui.Rect) {
	// The paint callback is where the system's LAID-OUT box arrives, and
	// the tick spawns into it (an `anchors.fill` emitter has no rect of
	// its own until then).
	s.lastRect = rect
	for _, p := range s.parts {
		age := s.now - p.born
		if age < 0 || age >= p.life {
			continue
		}
		t := age / p.life
		// QML's ImageParticle fades a particle in over the first TENTH of
		// its life and out over the last QUARTER — the constants in Qt's
		// own particle shader (`fadeIn = min(t*10, 1)`,
		// `fadeOut = 1 - min((t - 0.75)*4, 1)`), where ours faded in over a
		// fixed 150ms and out over the last 30%. Probed with a single
		// particle whose life is far longer than the capture: Qt's peak
		// alpha rose 0.39/0.59/0.78 for lives of 100/60/40 seconds, a slope
		// of ~10.5 per unit of life fraction (its shader's 10, plus an
		// offset of about 0.13 this probe could not account for).
		alpha := float32(min(t*10, 1))
		alpha *= float32(1 - max(0, min((t-0.75)*4, 1)))
		size := p.size + (p.endSize-p.size)*float32(t)
		c := p.color
		c.A *= alpha
		if img := s.texture(r, p.tex); img != nil {
			// The sprite the markup names, tinted by the particle's colour
			// — the textures are white/greyscale glows, so a multiply is
			// exactly QML's ImageParticle.
			r.DrawImage(img, rect.X+p.x-size/2, rect.Y+p.y-size/2, size, size, c)
			continue
		}
		// No texture: a round dot. Our batcher has no point sprites, so
		// this is a max-radius rounded rect, which reads the same.
		r.FillRoundRect(rect.X+p.x-size/2, rect.Y+p.y-size/2, size, size, size/2, c)
	}
}

// texture decodes (once) and returns the GPU image for a source, or nil
// when the markup named none — or the file could not be read.
func (s *particleSystem) texture(r *render.Renderer2D, src string) *render.Image {
	if src == "" || len(s.texData) == 0 {
		return nil
	}
	if img, ok := s.texGPU[src]; ok {
		return img
	}
	data := s.texData[src]
	if len(data) == 0 {
		return nil
	}
	img, err := r.LoadImage(data)
	if err != nil {
		img = nil
	}
	if s.texGPU == nil {
		s.texGPU = map[string]*render.Image{}
	}
	s.texGPU[src] = img // nil cached too: do not retry every frame
	return img
}

// emitterSpec is one Emitter's resolved configuration for a frame.
type emitterSpec struct {
	group                         string
	enabled                       bool
	rate                          float64
	x, y, w, h                    float32
	lifeSpan, lifeSpanVariation   float32
	size, sizeVariation, endSize  float32
	angle, angleVariation         float32
	magnitude, magnitudeVariation float32
	accelX, accelY                float32
	// fillSys: `anchors.fill: parent` on the emitter — its rect is the
	// system's, resolved when a particle spawns rather than at build time.
	// The system's own size is a LAID-OUT one, so an anchored system has
	// none on the build that creates it, and a document with no ids in it
	// never rebuilds (nothing feeds the geometry signature): the emitter
	// stayed a POINT at the origin and the page showed one blob in the
	// corner.
	fillSys bool
}

// particlePainter is one ImageParticle: which groups it colours, and how.
type particlePainter struct {
	groups         []string
	color          ui.RGBA
	colorVariation float32
	alpha          float32
	source         string // ImageParticle.source: the sprite to draw
}

// matches reports whether this painter applies to a particle group. An
// ImageParticle with no groups paints the default (empty) group, as in QML.
func (p particlePainter) matches(group string) bool {
	if len(p.groups) == 0 {
		return group == ""
	}
	for _, g := range p.groups {
		if g == group {
			return true
		}
	}
	return false
}

// isParticlePart reports the QtQuick.Particles children a ParticleSystem
// draws: emitters, painters and affectors. They are usually nested inside
// the system, but QML only requires a `system:` pointing at it — the
// corpus' interactive card, and any page that anchors an emitter to
// something else, writes them as siblings.
func isParticlePart(typeName string) bool {
	switch typeName {
	case "Emitter", "TrailEmitter", "ImageParticle", "ItemParticle",
		"CustomParticle", "Age", "Attractor", "Friction", "Gravity",
		"GroupGoal", "SpriteGoal", "Turbulence", "Wander", "PointAttractor":
		return true
	}
	return false
}

// collectParticleParts records, at LOAD time, every particle part declared
// outside its system, keyed by the id its `system:` names. It has to happen
// before anything builds: such a part usually sits AFTER its system in the
// document, so registering it while the tree builds would leave the system
// a frame behind — and the frame that registered has already snapshotted
// its signatures, so no rebuild would follow to catch up.
func (doc *docState) collectParticleParts(n *node) {
	if n == nil {
		return
	}
	if isParticlePart(n.typeName) {
		id := ""
		for _, a := range n.attrs {
			if a.name == "system" {
				if ref, ok := a.expr.(*identExpr); ok {
					id = ref.name
				}
			}
		}
		if id != "" {
			if doc.particleParts == nil {
				doc.particleParts = map[string][]*node{}
			}
			doc.particleParts[id] = append(doc.particleParts[id], n)
		}
	}
	for _, c := range n.children {
		doc.collectParticleParts(c)
	}
	for _, a := range n.attrs {
		if a.elem != nil {
			doc.collectParticleParts(a.elem)
		}
	}
}

// partRef is a particle part plus the scope its bindings read.
type partRef struct {
	node *node
	env  *Env
}

// buildParticleSystem renders a ParticleSystem: it reads its Emitter and
// ImageParticle children, advances the simulation with the frame delta, and
// draws the particles inside its own rect.
func buildParticleSystem(n *node, env *Env, doc *docState) (ui.Element, error) {
	// A particle system is live state the property system cannot stamp
	// (its emitters and painters register themselves as the subtree
	// builds), so a cached copy of it is an empty box: keep it untracked.
	core.MarkUntrackedRead()
	running := true
	var w, h float32
	for _, a := range n.attrs {
		switch a.name {
		case "id", "anchors.fill", "anchors.centerIn", "paused", "empty",
			"particleCount", "clip", "z", "opacity", "visible":
			// accepted, not modeled
		case "running":
			if b, err := attrBool(a, env); err == nil {
				running = b
			}
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			h = f
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: ParticleSystem has no attribute %q", a.line, a.name)
		}
	}
	sys := doc.particleSystem(doc.key(n))
	// Emitter positions are in the system's own coordinates, so they need
	// its real size. An `anchors.fill` system has no width/height of its
	// own: take last frame's laid-out box (zero on the very first frame,
	// settled by the geometry signature like every other self-geometry
	// read).
	if pe, ok := doc.prevElems.get2(doc.keyID(n)); ok {
		if b := ui.Bounds(pe); b.W > 0 && b.H > 0 {
			w, h = b.W, b.H
		}
	}
	// A ParticleSystem is not a visual item in QML — the PAINTER draws, in
	// the system's coordinates — so markup routinely gives the system no
	// size at all and lets its emitter fill the enclosing item. Ours draws
	// the particles itself, so with no size of its own it takes the box it
	// sits in; otherwise an emitter written that way emitted into a
	// zero-size rect and the page showed nothing.
	if w == 0 || h == 0 {
		if po, ok := env.vars["parent"].(*elemObject); ok && po != nil && po.el != nil {
			if b := ui.Bounds(po.el); b.W > 0 && b.H > 0 {
				w, h = b.W, b.H
			}
		}
	}

	// Resolve the children each build, so bound emitter rates and sizes
	// follow their sliders.
	var emitters []emitterSpec
	var painters []particlePainter
	// The system's own children, plus the parts declared elsewhere and
	// bound to it by `system:` (each with the scope its bindings read).
	parts := make([]partRef, 0, len(n.children))
	for _, c := range n.children {
		parts = append(parts, partRef{node: c, env: env})
	}
	// Plus the parts bound to this system by `system:` from elsewhere in
	// the document; they read the SYSTEM's scope, which is theirs too
	// wherever QML allows the reference (same file, same delegate row).
	if id := nodeID(n); id != "" {
		for _, c := range doc.particleParts[id] {
			parts = append(parts, partRef{node: c, env: env})
		}
	}
	for _, part := range parts {
		c, env := part.node, part.env
		switch c.typeName {
		case "Emitter", "TrailEmitter":
			e, err := readEmitter(c, env, w, h)
			if err != nil {
				return nil, err
			}
			e.enabled = e.enabled && running
			emitters = append(emitters, e)
		case "ImageParticle", "ItemParticle", "CustomParticle":
			pt := readPainter(c, env)
			// Read the sprite's bytes here, where the document's base
			// directory is known; the draw decodes them on the GPU.
			if pt.source != "" && sys.texData[pt.source] == nil {
				if path := resolveImagePath(doc.baseDir, pt.source); path != "" {
					if b, err := os.ReadFile(path); err == nil {
						if sys.texData == nil {
							sys.texData = map[string][]byte{}
						}
						sys.texData[pt.source] = b
					}
				}
			}
			painters = append(painters, pt)
		default:
			// Affectors, groups and anything else: accepted, not modeled.
		}
	}
	if len(painters) == 0 {
		painters = []particlePainter{{color: ui.RGB(0xffffff), alpha: 1}}
	}
	sys.emitters, sys.painters = emitters, painters
	sys.running = running
	// A running system has been emitting since it appeared, so a still
	// frame of one should look populated rather than empty. Warm it up once
	// with the emitters we just resolved — which also makes a headless
	// screenshot (where almost no wall-clock time passes) representative.
	if running && !sys.warmed && w > 0 && h > 0 {
		sys.warmed = true
		for i := 0; i < 40; i++ {
			sys.advance(0.025, emitters, painters, sys.lastRect)
		}
	}
	if w <= 0 {
		w = 300
	}
	if h <= 0 {
		h = 200
	}
	el := ui.NewCanvas(w, h, func(r *render.Renderer2D, rect ui.Rect) {
		sys.draw(r, rect)
	})
	// Its own laid-out box is what an anchored system reads next frame,
	// and registering it is also what puts that box in the document's
	// geometry signature — otherwise a page whose particles are its only
	// geometry never rebuilds to pick it up.
	doc.registerElem(doc.keyID(n), el)
	doc.noteIDKey(n, doc.keyID(n))
	return el, nil
}

// readEmitter resolves one Emitter's configuration. Its geometry defaults to
// the system's own box, and anchors place it the way markup asks.
func readEmitter(n *node, env *Env, sysW, sysH float32) (emitterSpec, error) {
	e := emitterSpec{
		enabled: true, rate: 10, lifeSpan: 1000, size: 16, endSize: -1,
		angle: 270, magnitude: 100,
	}
	var anchorH, anchorV string
	for _, a := range n.attrs {
		f := func() float32 {
			v, err := attrFloat(a, env)
			if err != nil {
				return 0
			}
			return v
		}
		switch a.name {
		case "id":
		case "group":
			if v, err := a.expr.eval(env); err == nil {
				e.group = str(v)
			}
		case "enabled":
			if b, err := attrBool(a, env); err == nil {
				e.enabled = b
			}
		case "emitRate":
			e.rate = float64(f())
		case "lifeSpan":
			e.lifeSpan = f()
		case "lifeSpanVariation":
			e.lifeSpanVariation = f()
		case "size":
			e.size = f()
		case "sizeVariation":
			e.sizeVariation = f()
		case "endSize":
			e.endSize = f()
		case "x":
			e.x = f()
		case "y":
			e.y = f()
		case "width":
			e.w = f()
		case "height":
			e.h = f()
		case "anchors.horizontalCenter", "anchors.centerIn":
			anchorH = "center"
		case "anchors.verticalCenter":
			anchorV = "center"
		case "anchors.bottom":
			anchorV = "bottom"
		case "anchors.top":
			anchorV = "top"
		case "anchors.left":
			anchorH = "left"
		case "anchors.right":
			anchorH = "right"
		case "anchors.fill":
			e.w, e.h, e.fillSys = sysW, sysH, true
		case "velocity", "acceleration":
			dx, dy, angle, angleVar, mag, magVar := readDirection(a, env)
			if a.name == "velocity" {
				e.angle, e.angleVariation = angle, angleVar
				e.magnitude, e.magnitudeVariation = mag, magVar
			} else {
				e.accelX, e.accelY = dx, dy
			}
		case "shape", "startTime", "maximumEmitted", "emitCap", "follow",
			"emitRatePerParticle", "emitWidth", "emitHeight", "emitShape",
			"anchors.margins", "anchors.horizontalCenterOffset":
			// accepted, not modeled
		}
	}
	// Place the emitter box inside the system, honouring the anchors above.
	switch anchorH {
	case "center":
		e.x = (sysW - e.w) / 2
	case "right":
		e.x = sysW - e.w
	}
	switch anchorV {
	case "center":
		e.y = (sysH - e.h) / 2
	case "bottom":
		e.y = sysH - max(e.h, 1)
	}
	return e, nil
}

// readDirection reads a velocity/acceleration element: AngleDirection in
// polar terms, PointDirection and TargetDirection in cartesian ones.
func readDirection(a attr, env *Env) (dx, dy, angle, angleVar, mag, magVar float32) {
	d := a.elem
	if d == nil {
		return 0, 0, 270, 0, 100, 0
	}
	get := func(name string, def float32) float32 {
		for _, at := range d.attrs {
			if at.name != name || at.expr == nil {
				continue
			}
			if f, err := attrFloat(at, env); err == nil {
				return f
			}
		}
		return def
	}
	switch d.typeName {
	case "AngleDirection":
		return 0, 0, get("angle", 270), get("angleVariation", 0),
			get("magnitude", 100), get("magnitudeVariation", 0)
	case "PointDirection", "TargetDirection", "CumulativeDirection":
		x, y := get("x", 0), get("y", 0)
		xv, yv := get("xVariation", 0), get("yVariation", 0)
		ang := float32(math.Atan2(float64(y), float64(x)) * 180 / math.Pi)
		m := float32(math.Hypot(float64(x), float64(y)))
		return x + xv, y + yv, ang, 0, m, float32(math.Hypot(float64(xv), float64(yv)))
	}
	return 0, 0, 270, 0, 100, 0
}

// readPainter resolves one ImageParticle's colouring.
func readPainter(n *node, env *Env) particlePainter {
	p := particlePainter{color: ui.RGB(0xffffff), alpha: 1}
	for _, a := range n.attrs {
		switch a.name {
		case "groups":
			if v, err := a.expr.eval(env); err == nil {
				if arr, ok := v.([]any); ok {
					for _, g := range arr {
						p.groups = append(p.groups, str(g))
					}
				} else {
					p.groups = append(p.groups, str(v))
				}
			}
		case "color":
			if c, err := attrColor(a, env); err == nil {
				p.color = c
			}
		case "colorVariation":
			if f, err := attrFloat(a, env); err == nil {
				p.colorVariation = f
			}
		case "alpha", "opacity":
			if f, err := attrFloat(a, env); err == nil {
				p.alpha = f
			}
		case "source":
			if v, err := a.expr.eval(env); err == nil {
				p.source = str(v)
			}
		}
	}
	return p
}

// particleSystem returns the retained simulation for a system element.
func (doc *docState) particleSystem(key string) *liveParticleSystem {
	if doc.particles == nil {
		doc.particles = map[string]*liveParticleSystem{}
	}
	if s, ok := doc.particles[key]; ok {
		return s
	}
	s := &liveParticleSystem{particleSystem: particleSystem{
		pending: map[string]float64{},
		// A fixed seed keeps a headless screenshot reproducible.
		rng: rand.New(rand.NewSource(1)),
	}}
	doc.particles[key] = s
	return s
}

// liveParticleSystem is a system plus the per-frame configuration the
// document's tick needs to advance it.
type liveParticleSystem struct {
	particleSystem
	emitters []emitterSpec
	painters []particlePainter
	running  bool
	warmed   bool
}

// advanceParticles steps every registered system by the frame delta. Called
// from the same tick as timers and animators.
func (doc *docState) advanceParticles(dtMs float64) {
	for _, s := range doc.particles {
		if !s.running {
			continue
		}
		s.advance(dtMs/1000, s.emitters, s.painters, s.lastRect)
	}
}
