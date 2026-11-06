package gml

import (
	"fmt"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// Navigation containers: StackLayout, SwipeView and StackView all show exactly
// one of their pages at a time rather than stacking every child (which is what
// a plain box does). StackLayout/SwipeView select by currentIndex; StackView
// keeps a runtime push/pop stack and shows its top. A TabBar tracks the
// currentIndex its TabButtons drive, so the canonical
//
//	TabBar { id: bar; TabButton{} TabButton{} }
//	StackLayout { currentIndex: bar.currentIndex; Page{} Page{} }
//
// pattern works end to end.

// componentRef is the value an id'd `Component { }` binds to: the single child
// node it wraps, instantiated on demand by Loader.sourceComponent or
// StackView.push rather than rendered in place.
type componentRef struct {
	node *node
}

// navStack is a StackView's runtime page stack. Entries are the nodes to build
// (initialItem, then whatever push/replace supply); the top is shown.
type navStack struct {
	items []*node
}

// slideState retains a SwipeView's page-change transition: the page shown
// last frame and a tweened 1→0 progress (a core.Property, so the tween keeps
// the dirty-tracker rebuilding until it settles).
type slideState struct {
	last int
	from int
	dir  int
	p    *core.Property[float32]
}

// buildStack renders a StackLayout/SwipeView/StackView: it shows the single
// current page (others are not built) inside a growing container that carries
// the container's own size/anchors/background.
func buildStack(n *node, env *Env, doc *docState) (ui.Element, error) {
	// Visual page children, in document order. A Repeater child expands into
	// one page per model row — QML's tidy way to declare five wizard steps —
	// so its delegate has to be built in that row's scope.
	var pages []stackPage
	for _, c := range n.children {
		if isNonVisual(c) {
			continue
		}
		if c.typeName == "Repeater" {
			rep, err := repeaterPages(c, env, doc)
			if err != nil {
				return nil, err
			}
			pages = append(pages, rep...)
			continue
		}
		pages = append(pages, stackPage{n: c})
	}

	// currentIndex: an explicit binding (often `bar.currentIndex`) or an
	// engine-owned persistent property unique to this container. Either way it
	// is exposed as `id.currentIndex` for other elements to bind against.
	var idxRef Ref
	var initialItem *node
	for _, a := range n.attrs {
		switch a.name {
		case "currentIndex":
			idxRef, _ = intBinding(doc, n, a, env)
		case "initialItem":
			// StackView { initialItem: someComponent } or an inline element.
			if a.elem != nil {
				initialItem = unwrapComponent(a.elem)
			} else if cr, err := a.expr.eval(env); err == nil {
				if c, ok := cr.(*componentRef); ok {
					initialItem = c.node
				}
			}
		}
	}
	if idxRef == nil {
		idxRef = doc.intProp(doc.key(n)+"|currentIndex", 0)
	}
	doc.exposeIDProp(n, "currentIndex", idxRef)
	// `count` is the QML contract an indicator binds to (PageIndicator {
	// count: view.count }), and depth/currentItem come with it.
	doc.exposeIDProp(n, "count", float64(len(pages)))
	doc.exposeIDProp(n, "depth", float64(len(pages)))

	// The container box itself (size/anchors/color), without the nav attrs that
	// buildBox doesn't know about.
	container, err := buildBox(stripStackAttrs(n), env, doc)
	if err != nil {
		return nil, err
	}
	box, _ := container.(*ui.DivEl)

	var current *stackPage
	var outgoing *stackPage // previous page, while a SwipeView slide is running
	var slideQ float32
	slideDir := 0
	if n.typeName == "StackView" {
		current = stackViewTop(n, env, doc, pages, initialItem, idxRef)
	} else {
		// StackLayout / SwipeView: pick the page at currentIndex.
		i := refInt(idxRef)
		if len(pages) > 0 {
			if i < 0 {
				i = 0
			}
			if i >= len(pages) {
				i = len(pages) - 1
			}
			current = &pages[i]
		}
		if n.typeName == "SwipeView" && box != nil && len(pages) > 1 {
			// A horizontal swipe flips the page (left = next), clamped.
			cur, count, ref := i, len(pages), idxRef
			box.OnSwipe(func(dir int) {
				next := cur - dir // swipe left (dir -1) advances
				if next < 0 {
					next = 0
				}
				if next >= count {
					next = count - 1
				}
				_ = ref.set(next)
			})
			// Page-change slide: tween 1→0; while running, the previous page
			// slides out as the new one slides in (ui.Slide).
			key := doc.key(n)
			st := doc.slides[key]
			if st == nil {
				st = &slideState{last: i, p: &core.Property[float32]{}}
				doc.slides[key] = st
			}
			if i != st.last {
				st.from, st.dir = st.last, 1
				if i < st.from {
					st.dir = -1
				}
				st.last = i
				st.p.Set(1)
				core.Tween(st.p, 0, 220*time.Millisecond, nil)
			}
			if q := st.p.Get(); q > 0.01 && st.from != i && st.from < len(pages) {
				outgoing, slideQ, slideDir = &pages[st.from], q, st.dir
			}
		}
	}

	if current != nil {
		el, err := current.build(env, doc)
		if err != nil {
			return nil, err
		}
		grow := func(e ui.Element) {
			switch c := e.(type) {
			case *ui.DivEl:
				c.Grow()
			case *ui.LazyImageEl:
				c.Grow()
			}
		}
		// Mid-transition: the previous page slides out under the new one.
		if outgoing != nil {
			old, err := outgoing.build(env, doc)
			if err != nil {
				return nil, err
			}
			grow(old)
			grow(el)
			el = ui.Slide(old, el, slideQ, slideDir)
		}
		if box != nil {
			box.Grow()
			grow(el) // let a page that wants to fill actually fill
			// Qt resizes the current page to the container, whatever the
			// page's own natural size is (a page is usually a bare Rectangle
			// whose children are anchored inside it, which measures as
			// nothing). Anchor-fill it rather than laying it out in flow.
			box.AnchorMode()
			box.Children(ui.Anchored(el, "", ui.Anchors{Fill: "parent"}))
		} else {
			return el, nil
		}
	} else if box != nil {
		box.Grow()
	}
	return container, nil
}

// stackPage is one page of a navigation container: a declared child, or a
// Repeater delegate together with the model row it stands for.
type stackPage struct {
	n    *node
	row  map[string]any // nil for a declared page
	idx  int            // row index inside the repeater
	key  string         // the repeater's view key (row-scope identity)
	repd bool           // came from a Repeater
}

// build renders the page, in its model row's scope when it came from a
// Repeater.
func (p *stackPage) build(env *Env, doc *docState) (ui.Element, error) {
	if !p.repd {
		return build(p.n, env, doc)
	}
	restore := enterRow(p.key, p.n, env, doc, p.row, p.idx)
	el, err := build(p.n, env, doc)
	restore()
	return el, err
}

// repeaterPages expands a Repeater declared inside a navigation container
// into its pages.
func repeaterPages(n *node, env *Env, doc *docState) ([]stackPage, error) {
	var modelAttr *attr
	for i := range n.attrs {
		if n.attrs[i].name == "model" || n.attrs[i].name == "count" {
			modelAttr = &n.attrs[i]
		}
	}
	if modelAttr == nil {
		return nil, fmt.Errorf("gml:%d: Repeater needs a model", n.line)
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	rows, count, err := resolveModel(*modelAttr, env)
	if err != nil {
		return nil, err
	}
	key := doc.key(n)
	out := make([]stackPage, 0, count)
	for i := 0; i < count; i++ {
		var row map[string]any
		if rows != nil {
			row = rows[i]
		}
		out = append(out, stackPage{n: delegate, row: row, idx: i, key: key, repd: true})
	}
	return out, nil
}

// stackViewTop returns the node a StackView currently shows: the top of its
// runtime push/pop stack. The stack is seeded once from initialItem (or the
// first declared child) and then driven by push/pop/replace/clear, which are
// wired as methods on the StackView's id. currentIndex tracks the depth.
func stackViewTop(n *node, env *Env, doc *docState, pages []stackPage, initial *node, idxRef Ref) *stackPage {
	key := doc.key(n)
	// The nav stack is mutated imperatively (push/pop) outside the property
	// system — a StackView subtree can never be proven fresh.
	core.MarkUntrackedRead()
	ns := doc.navStacks[key]
	if ns == nil {
		ns = &navStack{}
		if initial != nil {
			ns.items = append(ns.items, initial)
		} else if len(pages) > 0 {
			ns.items = append(ns.items, pages[0].n)
		}
		doc.navStacks[key] = ns
	}
	// Wire the imperative API onto the id object (idempotent across frames).
	if idObj := doc.elemObjs.get(doc.intern(nodeID(n))); idObj != nil && idObj.methods == nil {
		idObj.methods = stackViewMethods(ns, idxRef)
	}
	// Keep currentIndex in sync with the stack depth for read-back.
	_ = idxRef.set(len(ns.items) - 1)
	if len(ns.items) == 0 {
		return nil
	}
	return &stackPage{n: ns.items[len(ns.items)-1]}
}

// stackViewMethods builds StackView's JS-callable navigation API over its stack.
// push/replace take a Component (or inline item); pop/clear take no item. Each
// mutates the stack so the next frame renders the new top.
func stackViewMethods(ns *navStack, idxRef Ref) map[string]any {
	itemNode := func(v any) *node {
		switch t := v.(type) {
		case *componentRef:
			return t.node
		case *node:
			return t
		}
		return nil
	}
	sync := func() { _ = idxRef.set(len(ns.items) - 1) }
	return map[string]any{
		"push": jsFunc(func(args []any) (any, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("StackView.push needs an item")
			}
			if nd := itemNode(args[0]); nd != nil {
				ns.items = append(ns.items, nd)
				sync()
			}
			return nil, nil
		}),
		"pop": jsFunc(func(args []any) (any, error) {
			if len(ns.items) > 1 { // never pop the base item, like Qt
				ns.items = ns.items[:len(ns.items)-1]
				sync()
			}
			return nil, nil
		}),
		"replace": jsFunc(func(args []any) (any, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("StackView.replace needs an item")
			}
			if nd := itemNode(args[0]); nd != nil {
				if len(ns.items) == 0 {
					ns.items = append(ns.items, nd)
				} else {
					ns.items[len(ns.items)-1] = nd
				}
				sync()
			}
			return nil, nil
		}),
		"clear": jsFunc(func(args []any) (any, error) {
			if len(ns.items) > 1 {
				ns.items = ns.items[:1] // keep the base item
				sync()
			}
			return nil, nil
		}),
	}
}

// stripStackAttrs copies n as a plain box, dropping the navigation attrs that
// buildBox doesn't understand (currentIndex/initialItem and friends).
func stripStackAttrs(n *node) *node {
	cp := *n
	cp.typeName = "Item"
	cp.children = nil // pages are built separately by buildStack
	cp.attrs = nil
	for _, a := range n.attrs {
		switch a.name {
		case "currentIndex", "initialItem", "count", "interactive",
			"orientation", "currentItem", "depth", "empty", "busy":
			// nav state — handled by buildStack
		default:
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp
}

// buildTabBar renders a Qt Quick TabBar: a row of TabButtons that select a
// currentIndex. The index is an engine-owned persistent property exposed as
// `id.currentIndex`; clicking a tab sets it, and the active tab gets an accent
// underline.
func buildTabBar(n *node, env *Env, doc *docState) (ui.Element, error) {
	var idxRef Ref
	for _, a := range n.attrs {
		switch a.name {
		case "id", "width", "height", "spacing", "position", "contentWidth",
			"contentHeight", "z", "anchors.fill", "anchors.top", "anchors.left",
			"anchors.right", "anchors.bottom", "anchors.horizontalCenter",
			"anchors.topMargin", "Layout.fillWidth", "Layout.preferredWidth",
			"visible", "clip":
			// layout/chrome — accepted, applied below or ignored
		case "currentIndex":
			idxRef, _ = intBinding(doc, n, a, env)
		default:
			return nil, fmt.Errorf("gml:%d: TabBar has no attribute %q", a.line, a.name)
		}
	}
	if idxRef == nil {
		// The bar's own state, and the one thing every binding that reads
		// `bar.currentIndex` must see: the slot the bar's id already
		// publishes, so a StackLayout bound to it follows the tab the user
		// clicked. Only a bar whose id published nothing (or has no id at
		// all) needs a slot of its own.
		if idxRef = doc.idPropRef(n, "currentIndex"); idxRef == nil {
			idxRef = doc.intProp(doc.key(n)+".currentIndex", 0)
		}
	}
	doc.exposeIDProp(n, "currentIndex", idxRef)
	cur := refInt(idxRef)

	// Qt's Basic style, which is what a page gets when the app's style
	// module ships no TabButton.qml: a light bar, the current tab near-white
	// with black text, the others mid-grey with white text, each separated
	// by a hairline. No accent underline — that is a Material/Fusion idea.
	// Basic's own metrics, measured from Qt: the bar is 40 tall, its tabs
	// are 1px apart (the bar showing through, so two inactive tabs have no
	// visible divider at all), their labels are 12px in the default family
	// and they carry 6px of padding.
	// The BAR itself is light: where no tab covers it (a bar of three
	// narrow tabs in a wide card) Qt shows #efefef, not the inactive-tab
	// grey. An inactive tab paints its own grey over it.
	bar := ui.Div().Row().Gap(basicTabGap).Bg(ui.RGB(basicTabCurrent))
	// Tabs, in document order — a Repeater declares one per model row (the
	// dynamic-tabs idiom), so its delegate's label is read in that row's
	// scope like any other repeated element.
	type tabItem struct {
		label  string
		fixed  bool    // the tab sets its own width: it does not share the bar
		sizePx float64 // the type size the tab states, 0 for the default
	}
	var tabs []tabItem
	for _, c := range n.children {
		if isNonVisual(c) {
			continue
		}
		if c.typeName == "Repeater" {
			pages, err := repeaterPages(c, env, doc)
			if err != nil {
				return nil, err
			}
			for i := range pages {
				p := &pages[i]
				restore := enterRow(p.key, p.n, env, doc, p.row, p.idx)
				label, err := tabButtonLabel(p.n, env)
				restore()
				if err != nil {
					return nil, err
				}
				tabs = append(tabs, tabItem{label: label, fixed: hasAttr(p.n, "width"),
					sizePx: tabButtonFont(p.n, env)})
			}
			continue
		}
		label, err := tabButtonLabel(c, env)
		if err != nil {
			return nil, err
		}
		tabs = append(tabs, tabItem{label: label, fixed: hasAttr(c, "width"),
			sizePx: tabButtonFont(c, env)})
	}
	for index, t := range tabs {
		label := t.label
		active := index == cur
		textCol, bg := ui.RGB(0xffffff), ui.RGB(basicTabInactive)
		if active {
			textCol, bg = ui.RGB(0x000000), ui.RGB(basicTabCurrent)
		}
		txt := ui.Text(label, textCol).Truncate()
		size := t.sizePx
		if size <= 0 {
			size = basicTabFontPx // Basic does not take the application font
		}
		txt.SizePx(size)
		tab := ui.Div().Row().ItemsCenter().JustifyCenter().
			H(basicTabHeight).P(basicTabPad).
			Bg(bg).Children(txt)
		if !active {
			// Basic lightens an inactive tab under the pointer; the current
			// one already IS the light colour and does not react.
			tab.HoverBg(ui.RGB(basicTabHover))
		}
		if !t.fixed {
			// Qt's TabBar shares its width between the tabs; only a tab that
			// sets its own width keeps a natural one.
			tab.Grow().Shrink().Clip()
		}
		idx := index
		tab.OnClick(func() { _ = idxRef.set(idx) })
		bar.Children(tab)
	}
	return bar, nil
}

// Qt Basic's tab palette, sampled from the reference renders.
const (
	basicTabCurrent  = 0xefefef // the current tab (and the hairline dividers)
	basicTabInactive = 0x9f9f9f // the others, and the bar behind them
	basicTabHover    = 0xb4b4b4 // Basic lightens an inactive tab under the pointer
)

// Qt Basic's tab metrics, measured from Qt (see buildTabBar).
const (
	basicTabHeight = 40
	basicTabPad    = 6
	basicTabGap    = 1
	basicTabFontPx = 12
)

// tabButtonLabel reads a TabButton's text. Non-TabButton children fall back to
// their text: attribute if present.
func tabButtonLabel(n *node, env *Env) (string, error) {
	for _, a := range n.attrs {
		if a.name == "text" {
			v, err := a.expr.eval(env)
			if err != nil {
				return "", err
			}
			return str(v), nil
		}
	}
	return "", nil
}

// tabButtonFont reads the type size a TabButton states for its label (0 when
// it states none). A bar of four tabs sharing one row fits very different
// amounts of text at 14px than at the document default, so ignoring this
// truncated labels Qt shows in full.
func tabButtonFont(n *node, env *Env) float64 {
	for _, a := range n.attrs {
		switch a.name {
		case "font.pixelSize", "font.pointSize":
			if f, err := attrFloat(a, env); err == nil && f > 0 {
				return float64(f)
			}
		}
	}
	return 0
}

// refInt reads a Ref as an int (0 on any failure).
func refInt(r Ref) int {
	if r == nil {
		return 0
	}
	if f, err := toFloat64(r.get()); err == nil {
		return int(f)
	}
	return 0
}
