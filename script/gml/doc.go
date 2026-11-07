package gml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// Document is a loaded gml file bound to an Env: View() is the per-frame
// view function the ui layer drives, exactly like a hand-written one.
type Document struct {
	env  *Env
	doc  *docState
	root *node
}

// LoadString parses gml source and binds it to env.
func LoadString(src string, env *Env) (*Document, error) {
	return loadSource(src, env, "")
}

// LoadStringAt is LoadString for source that belongs to a directory:
// imports, singletons, file components and relative Image or Loader
// sources resolve against baseDir, as they do for a file loaded from
// there. Use it for markup embedded in a program (go:embed) next to the
// files it refers to.
func LoadStringAt(src, baseDir string, env *Env) (*Document, error) {
	return loadSource(src, env, baseDir)
}

// SetLenient switches QML's lenient mode on or off: a child whose build
// fails is logged and skipped instead of failing the page, and a few
// layout rules follow Qt's plain positioning. Load turns it on for .qml
// files; markup from LoadString or LoadStringAt that is real QML can ask
// for it here, before the first View.
func (d *Document) SetLenient(on bool) { d.doc.lenient = on }

// loadSource is the shared load path: imports resolve (when a base
// directory is known) BEFORE property declaration, so singletons like a
// Style object are in scope when property defaults evaluate.
func loadSource(src string, env *Env, baseDir string) (*Document, error) {
	// Markup written against Qt is laid out against Qt's line metrics: a
	// Text item's height is the font's OS/2 window ascent+descent, not the
	// face's own line height, and everything below a heading sits several
	// pixels off without it.
	render.SetQtTextMetrics(true)
	root, err := Parse(src)
	if err != nil {
		return nil, err
	}
	d := &Document{env: env, doc: newDocState(), root: root}
	d.doc.baseDir = baseDir
	// The document is the file whose imports are in scope until a component
	// file's own tree builds (QML imports are per file).
	d.doc.curFile = root
	if baseDir != "" {
		d.doc.resolveImports(root, env)
	}
	// Soft pass only: a default referencing a control's own live state
	// (msgIdSpin.value, ma.containsMouse) can't resolve until that control
	// has been built at least once — View() retries for real, once, right
	// after the tree's first build.
	defer d.doc.retryPendingPropsSoft()
	if err := declareProps(root, env, d.doc); err != nil {
		return nil, err
	}
	declareFuncs(root, env)
	// Connections first: a declared signal folds in the handlers that
	// target its element.
	d.doc.registerConnections(root)
	declareSignals(root, env, d.doc)
	d.doc.collectParticleParts(root)
	d.doc.registerComponents(root)
	return d, nil
}

// Load parses a gml file and binds it to env. The file's directory becomes
// the base for resolving relative Image sources and module imports.
func Load(path string, env *Env) (*Document, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := loadSource(string(src), env, filepath.Dir(path))
	if err == nil {
		d.doc.lenient = strings.HasSuffix(path, ".qml")
	}
	return d, err
}

// View returns the per-frame view function: it rebuilds the element tree
// from the document each frame, so every expression is a live binding.
// Build errors render as an error banner instead of crashing the frame.
// SetProperty writes one of the document root's declared properties from
// the host — how an application drives a page it loaded (`fullSize`, a
// theme flag, a data path). Reports whether the document declares it; the
// write marks the property's binding broken, as an assignment in markup
// would.
func (d *Document) SetProperty(name string, v any) bool {
	ref, ok := d.doc.props[name]
	if !ok {
		return false
	}
	if err := ref.set(v); err != nil {
		logf("gml: SetProperty %s: %v", name, err)
		return false
	}
	return true
}

// Property reads a document property back (nil when undeclared).
func (d *Document) Property(name string) any {
	if ref, ok := d.doc.props[name]; ok {
		return ref.get()
	}
	return nil
}

// buildStats reports per-build subtree counts on stderr (GFX_BUILD_STATS=1):
// how much of the document a frame really rebuilt.
var buildStats = os.Getenv("GFX_BUILD_STATS") != ""

// noSubtreeCache rebuilds every subtree every frame (GFX_NO_SUBTREE_CACHE=1)
// — the ground truth a suspected stale cache is compared against.
var noSubtreeCache = os.Getenv("GFX_NO_SUBTREE_CACHE") != ""

// cacheSkip forces every subtree whose key contains it to rebuild
// (GFX_CACHE_SKIP=<substring>) — bisecting a stale render down to the
// subtree that is being wrongly served.
var cacheSkip = os.Getenv("GFX_CACHE_SKIP")

// buildTrace names every subtree built or served per frame (GFX_BUILD_TRACE=1).
var buildTrace = os.Getenv("GFX_BUILD_TRACE") != ""

func (d *Document) View() func() ui.Element {
	return func() ui.Element {
		// Settings properties bind (load their saved values) on the first
		// frame, so a host's SetSettingsStore after loading still counts;
		// values changed last frame are written now, once per frame.
		d.doc.bindPendingSettings(false)
		d.doc.flushSettings()
		// Advance Timers by the wall-clock delta first, so any triggered
		// handlers' property changes (which bump the revision) are seen below.
		d.doc.tickTimers(time.Now())
		// Run queued Go-side signal emissions (native.go) on the UI thread;
		// their handlers mutate models/properties, so invalidate the cache.
		if d.doc.drainNativeSignals() {
			d.doc.invalidateCache()
		}
		// Stamp the geometry and scroll tokens from the LAID-OUT elements
		// before anything re-evaluates: a property bound to `contentX /
		// width` is re-evaluated below only when its read-set is stale,
		// and the reads that make it stale are these tokens. (beginFrame
		// stamps again after the registry rotates; unchanged is a no-op.)
		d.doc.frameNo++
		d.doc.stampTokens(&d.doc.elems)
		// Re-evaluate live property bindings (QML: a declared property's
		// default expression stays bound until imperatively assigned).
		// Before the signature check so a changed binding forces a rebuild.
		rebindProps(rootForBuild(d.root), d.env, d.doc)

		// Dirty-tracking: rebuild only when an observable input changed since the
		// last build — a property/text edit (core.Revision), a scroll offset, or
		// the laid-out geometry (resize / a settling geometry binding). Otherwise
		// return the previous tree untouched, so an idle UI does no interpreter
		// work. The cached tree's elements are reused, so the ui layer still
		// re-lays-them-out every frame (hover/focus/resize resolve at paint time).
		obs := d.doc.observableSignature()
		// The geometry signature folds every registered element's rect,
		// which is worth computing only when the tree COULD be served: an
		// animating page changes an observable every frame and rebuilds
		// regardless. A frame that rebuilt without measuring geometry
		// leaves geoSig unknown, so the first quiet frame after it rebuilds
		// once more and records it — one extra build per change, not one
		// signature per frame.
		var geo uint64
		geoKnown := false
		if d.doc.haveSigs && d.doc.cachedEl != nil && obs == d.doc.obsRev {
			geo, geoKnown = d.doc.geometrySignature(), true
			if d.doc.geoKnown && geo == d.doc.geoSig {
				return d.doc.cachedEl
			}
		}
		laidOut := d.doc.completed || d.doc.anyLaidOut()

		// Rotate the element registry so geometry refs read last frame's rects.
		if buildStats {
			fmt.Fprintf(os.Stderr, "gml: build: %d fresh, %d cache hits, %d patched | misses: %d absent, %d env, %d untracked, %d stamped\n",
				d.doc.freshBuilds, d.doc.cacheHits, d.doc.patched, d.doc.missAbsent, d.doc.missEnv, d.doc.missUntracked, d.doc.missStamped)
			d.doc.freshBuilds, d.doc.cacheHits, d.doc.patched = 0, 0, 0
			d.doc.missAbsent, d.doc.missEnv, d.doc.missUntracked, d.doc.missStamped = 0, 0, 0, 0
			if len(snapshotSites) > 0 {
				fmt.Fprintf(os.Stderr, "gml: snapshots by site: %v\n", snapshotSites)
				clear(snapshotSites)
			}
			if sites := core.UntrackedSites(); len(sites) > 0 {
				fmt.Fprintf(os.Stderr, "gml: untracked reads by site: %v\n", sites)
			}
			if untrackedNames != nil && len(untrackedNames) > 0 {
				fmt.Fprintf(os.Stderr, "gml: untracked reads by name: %v\n", untrackedNames)
				clear(untrackedNames)
			}
		}
		d.doc.beginFrame()
		el, err := build(rootForBuild(d.root), d.env, d.doc)
		if err != nil {
			d.doc.invalidateCache()
			return errorBanner(fmt.Errorf("%s: %w", d.doc.baseDir, err))
		}
		// Run Component.onCompleted handlers once (QML component-completion).
		// Errors are logged, not fatal.
		//
		// They run after the SECOND build, not the first: a handler reads
		// geometry as freely as anything else in QML, where the item tree
		// is sized during instantiation — the corpus' blueprint viewer
		// calls `fitToView()` from one, which divides by a Flickable's
		// width. Our geometry exists only once a tree has been laid out,
		// so the handlers wait for the build that has one behind it (they
		// are queued by the first build; the next one rotates last frame's
		// rects into place).
		if patchVerify && (d.doc.patched > 0 || patchVerifyAll) {
			d.doc.verifyPatched(el, rootForBuild(d.root), d.env)
		}
		d.doc.builds++
		if d.doc.builds == 1 {
			d.doc.collected = true // the queued set is the set that runs
		}
		// `laidOut` was read before this build, so it means a tree HAS BEEN
		// LAID OUT — which is the condition, not a build count: a frame can
		// build twice (the layout pass and the paint pass each ask for a
		// view), so counting builds would run the handlers while every
		// geometry is still zero. The build cap is the way out for a
		// document whose ids never get geometry at all.
		completionChanged := false
		if !d.doc.completed && (laidOut || d.doc.builds >= 4) {
			d.doc.completed = true
			before := d.doc.observableSignature()
			for _, t := range d.doc.onCompleted {
				t.env = d.doc.selfScope(t)
				if err := runStmts(t.stmts, t.env); err != nil {
					logf("gml: Component.onCompleted: %v", err)
				}
			}
			d.doc.onCompleted = nil
			// What completion CHANGED is not in this tree: the handlers ran
			// after the build that queued them. Since they run exactly once,
			// forcing one more rebuild cannot loop — and without it a model
			// a handler filled (QML's imperative `append` idiom) stayed
			// invisible until something else happened to change.
			completionChanged = d.doc.observableSignature() != before
			// What the handlers changed is not in the tree they ran after,
			// so rebuild here rather than a frame later: a model a handler
			// filled (QML's imperative `append` idiom) is then visible in
			// the very frame that completed the document, as in Qt.
			if completionChanged {
				if el2, err := build(rootForBuild(d.root), d.env, d.doc); err == nil && el2 != nil {
					el = el2
				}
			}
		}
		// Property defaults that reference a Control's own state
		// (msgIdSpin.value, fieldCheck.checked, ...) can only resolve AFTER
		// that control has been built at least once — declareProps' retry
		// at load time runs before any build ever happens, so give pending
		// defaults exactly one more chance now that doc.elemObjs is populated.
		if len(d.doc.pendingProps) > 0 {
			d.doc.retryPendingProps()
		}
		d.doc.bindPendingSettings(true) // aliases whose target needed the first build
		// Snapshot the signatures this tree was built against. obs is re-read
		// (the build — and onCompleted — can change properties); geoSig keeps the
		// pre-build geometry so the first frame after a change rebuilds once more
		// to settle any binding that reads this frame's freshly laid-out bounds.
		d.doc.cachedEl = el
		d.doc.noteGround(el)
		d.doc.obsRev = d.doc.observableSignature()
		d.doc.geoSig, d.doc.geoKnown = geo, geoKnown
		d.doc.haveSigs = !completionChanged
		return el
	}
}

// DeclaresType reports whether the document's markup declares an element of
// this type anywhere — how a host decides to install the hooks a feature
// needs (a View3D scene needs a 3D encode pass and a depth buffer).
func (d *Document) DeclaresType(typeName string) bool {
	return declaresType(d.root, typeName)
}

// DeclaresType is Document.DeclaresType for the currently loaded document.
func (l *Live) DeclaresType(typeName string) bool {
	if l.doc == nil {
		return false
	}
	return l.doc.DeclaresType(typeName)
}

func declaresType(n *node, typeName string) bool {
	if n == nil {
		return false
	}
	if n.typeName == typeName {
		return true
	}
	for _, a := range n.attrs {
		if a.elem != nil && declaresType(a.elem, typeName) {
			return true
		}
	}
	for _, c := range n.children {
		if declaresType(c, typeName) {
			return true
		}
	}
	return false
}

func errorBanner(err error) ui.Element {
	logf("gml: build error: %v", err)
	return ui.Div().Col().P(16).Gap(8).Bg(ui.RGB(0x3a1212)).Children(
		ui.Text("gml error:", ui.RGB(0xffb4b4)),
		ui.Text(err.Error(), ui.RGB(0xffdcdc)),
	)
}

// Live wraps a Document with hot reload: while the app runs, edits to the
// file appear on the next frame. Parse errors keep the last good document
// and show an error banner until the file parses again. Markup-declared
// properties, text fields and scroll positions keep their values across
// reloads (state is keyed by name, not by tree position).
type Live struct {
	path    string
	env     *Env
	doc     *Document
	lastErr error
	mtime   time.Time
	checked time.Time
	// hostProps are the properties the host set through SetProperty,
	// re-applied after each reload.
	hostProps map[string]any
}

// Watch loads path and reloads it whenever its mtime changes (polled at
// most every 200ms, from the frame loop).
func Watch(path string, env *Env) (*Live, error) {
	d, err := Load(path, env)
	if err != nil {
		return nil, err
	}
	l := &Live{path: path, env: env, doc: d}
	if st, err := os.Stat(path); err == nil {
		l.mtime = st.ModTime()
	}
	return l, nil
}

func (l *Live) maybeReload() {
	now := time.Now()
	if now.Sub(l.checked) < 200*time.Millisecond {
		return
	}
	l.checked = now
	st, err := os.Stat(l.path)
	if err != nil || st.ModTime().Equal(l.mtime) {
		return
	}
	l.mtime = st.ModTime()
	src, err := os.ReadFile(l.path)
	if err != nil {
		l.lastErr = err
		return
	}
	root, err := Parse(string(src))
	if err != nil {
		l.lastErr = err
		logf("gml: reload: %v", err)
		return
	}
	// Re-declare against the same doc state: named state survives.
	next := &Document{env: l.env, doc: l.doc.doc, root: root}
	next.doc.curFile = root
	next.doc.resolveImports(root, l.env)
	defer next.doc.retryPendingPropsSoft() // see loadSource's comment
	if err := declareProps(root, l.env, next.doc); err != nil {
		l.lastErr = err
		logf("gml: reload: %v", err)
		return
	}
	declareFuncs(root, l.env)
	next.doc.connections = map[string]map[string]funcDecl{}
	next.doc.registerConnections(root)
	declareSignals(root, l.env, next.doc)
	next.doc.components = map[string]*node{}
	next.doc.registerComponents(root)
	next.doc.invalidateCache() // markup changed — force a rebuild
	l.doc = next
	l.lastErr = nil
	// Re-apply whatever the host had set (fullSize, a theme flag): the
	// reloaded document has fresh properties at their declared defaults.
	for name, v := range l.hostProps {
		next.SetProperty(name, v)
	}
	fmt.Fprintf(os.Stderr, "gml: reloaded %s\n", l.path)
}

// View returns the live per-frame view function.
// SetProperty writes a root property on the currently loaded document (see
// Document.SetProperty). Applied again after a hot reload, so a host's
// settings survive editing the file.
func (l *Live) SetProperty(name string, v any) bool {
	if l.hostProps == nil {
		l.hostProps = map[string]any{}
	}
	l.hostProps[name] = v
	return l.doc.SetProperty(name, v)
}

// Err is the current parse/build error, nil while the file is good. A
// host that draws its own chrome around the page can show it with a
// way out (View alone replaces the page with a banner).
func (l *Live) Err() error {
	l.maybeReload()
	return l.lastErr
}

func (l *Live) View() func() ui.Element {
	return func() ui.Element {
		l.maybeReload()
		if l.lastErr != nil {
			return errorBanner(fmt.Errorf("%s: %w", l.path, l.lastErr))
		}
		return l.doc.View()()
	}
}

// rootForBuild strips anchor attributes from the document root: a root's
// `anchors.fill: parent` refers to the host container (the window), which
// the root already fills — leaving them on would make the root's own
// builder reject them.
func rootForBuild(root *node) *node {
	for _, a := range root.attrs {
		if a.name == "x" || a.name == "y" || strings.HasPrefix(a.name, "anchors.") {
			return stripAnchorAttrs(root)
		}
	}
	return root
}
