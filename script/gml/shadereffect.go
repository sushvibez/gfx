package gml

// QtQuick's ShaderEffectSource and ShaderEffect.
//
// A ShaderEffect whose shaders arrive as .qsb files RUNS them: the SPIR-V
// inside goes straight to the GPU (see shaderfx.go), which is what a Qt app
// ships anyway. This file holds the rest — the effect kinds Qt implements
// itself (DropShadow, Glow, blur), and the fallback for a shader we cannot
// load, which renders the SOURCE unshaded rather than dropping the item's
// content. That is the degradation Qt itself shows when a shader fails to
// compile.

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// buildShaderEffectSource renders the item a ShaderEffectSource captures.
// `sourceItem: someId` names an element elsewhere in the document, which we
// build a second time here — the capture's stand-in.
func buildShaderEffectSource(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h float32
	hidden := false
	var sourceID string
	for _, a := range n.attrs {
		switch a.name {
		case "id", "hideSource", "live", "recursive", "mipmap", "smooth",
			"wrapMode", "textureSize", "format", "samples", "x", "y",
			"anchors.fill", "anchors.centerIn", "opacity", "z", "layer.enabled",
			// transform lists (Scale/Rotation on the captured copy) and the
			// rest of the item chrome: the capture is drawn in place here.
			"transform", "scale", "rotation", "transformOrigin", "clip",
			"antialiasing", "enabled", "visible.enabled", "objectName":
			// accepted, not modeled
		case "sourceItem":
			sourceID = doc.sourceRef(a.expr, env)
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
		case "visible":
			if b, err := attrBool(a, env); err == nil && !b {
				hidden = true
			}
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: ShaderEffectSource has no attribute %q",
				a.line, a.name)
		}
	}
	// `visible: false` on a capture means "the effect renders me instead" —
	// and since our effect draws the source itself, drawing here too would
	// double it.
	if hidden {
		return ui.Div(), nil
	}
	el := doc.buildByID(sourceID, env)
	if el == nil {
		// The source can also arrive as a VALUE — a component's `source:`
		// property carrying the item the use site passed
		// (`LensEffect { source: photoImage }`) — so follow it to its node.
		el = doc.buildBySourceValue(n, env)
	}
	if el == nil {
		return sizedBox(w, h), nil
	}
	// The capture is the size it states, not the size of the copy it holds:
	// the source is usually an anchor-filled Item, which measures nothing on
	// its own, and a sibling effect positions itself off THIS item's width
	// (`x: bgCapture.x; width: bgCapture.width`).
	if w > 0 || h > 0 {
		return ui.Div().Size(w, h).AnchorMode().
			Children(ui.Anchored(el, "", ui.Anchors{Fill: "parent"})), nil
	}
	return el, nil
}

// buildShaderEffect renders a ShaderEffect's source unshaded. The `source`
// (or `sourceItem`) property usually names a ShaderEffectSource or an item;
// either way what the user should see through the effect is that content.
func buildShaderEffect(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h float32
	var sourceID string
	// The two effects we CAN do honestly: a drop shadow and a glow are a
	// blurred copy of the source behind it, and the renderer has a gaussian
	// shadow primitive. Read their parameters as we go.
	shadow := effectShadow{color: ui.RGBA{A: 0.4}, blur: 8}
	for _, a := range n.attrs {
		switch a.name {
		case "source", "sourceItem", "src", "maskSource":
			if sourceID == "" {
				sourceID = doc.sourceRef(a.expr, env)
			}
		case "radius", "samples":
			// Qt's radius is the blur extent; samples is quality only. Zero
			// is a real value — a glow of radius 0 is not drawn at all —
			// so it must not fall back to the default.
			if a.name == "radius" {
				if f, err := attrFloat(a, env); err == nil {
					shadow.blur = max(f, 0)
					shadow.stated = true
				}
			}
		case "color":
			if c, err := attrColor(a, env); err == nil {
				shadow.color = c
			}
		case "horizontalOffset":
			if f, err := attrFloat(a, env); err == nil {
				shadow.dx = f
			}
		case "verticalOffset":
			if f, err := attrFloat(a, env); err == nil {
				shadow.dy = f
			}
		case "spread":
			if f, err := attrFloat(a, env); err == nil {
				shadow.spread = f
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
		case "visible":
			if b, err := attrBool(a, env); err == nil && !b {
				return ui.Div(), nil
			}
		default:
			// Shader code, uniforms (any custom property), blending and
			// mesh settings: accepted and ignored. A ShaderEffect's whole
			// attribute surface is user-defined, so nothing here is an
			// error.
		}
	}
	// A ShaderEffect that names .qsb shaders we can load RUNS them (see
	// shaderfx.go): the SPIR-V inside goes to the GPU and the uniform block
	// is filled from the markup. Everything below is the fallback for a
	// shader we cannot use.
	if el := shaderEffectElement(n, env, doc, w, h); el != nil {
		return el, nil
	}
	el := doc.buildByID(sourceID, env)
	if el == nil {
		// The source can also arrive as a VALUE rather than a bare id in
		// this file: a component wrapping the effect declares
		// `property var source` and its use site assigns the item
		// (`LensEffect { source: photoImage }`).
		el = doc.buildBySourceValue(n, env)
	}
	if el == nil {
		return sizedBox(w, h), nil
	}
	// The effect is the size it states, not the size of the copy it holds:
	// the source is usually an anchor-filled item, which measures nothing
	// on its own, and a zero-sized effect draws nothing at all (there is no
	// texture to blur into).
	if w > 0 || h > 0 {
		el = ui.Div().Size(w, h).AnchorMode().
			Children(ui.Anchored(el, "", ui.Anchors{Fill: "parent"}))
	}
	// A blur effect (MultiEffect's blurEnabled, Qt5Compat's GaussianBlur /
	// FastBlur / RecursiveBlur) is one we can run for real: the source
	// renders into its own texture and is composited blurred.
	colorizeCol, colorize := effectColorize(n, env)
	if radius := effectBlurRadius(n, env); radius > 0 {
		l := ui.Layer(doc.layerFor(doc.key(n)+"|blur"), el).Blur(radius)
		if colorize {
			// A colorizing blur is one object: the captured group is
			// composited by luminance in the colorization colour, which is
			// what the corpus' neon glow (`blurEnabled` + `colorization:
			// 1`) is — without it the source's white star stays white in
			// the middle of a teal glow.
			l.Colorize(colorizeCol)
		}
		el = l
	} else if colorize {
		el = ui.Layer(doc.layerFor(doc.key(n)+"|colorize"), el).Colorize(colorizeCol)
	}
	// DropShadow / Glow: put the source in a box that carries the shadow.
	// The shadow shape is the source's BOX (we cannot blur arbitrary
	// content), which is what these pages use them for — a card, a button,
	// a panel — and it reads like Qt's.
	switch n.typeName {
	case "DropShadow", "InnerShadow", "Glow":
		// Nothing to draw: a glow of radius 0 does not glow whatever its
		// spread, and a shadow with no blur sitting exactly under its
		// source is invisible. Ours would show as a hard rectangle the size
		// of the effect's own box, which for an aspect-fitted image is
		// bigger than the image.
		if shadow.stated && shadow.blur == 0 &&
			(n.typeName == "Glow" || (shadow.dx == 0 && shadow.dy == 0)) {
			return el, nil
		}
		if n.typeName == "Glow" {
			shadow.dx, shadow.dy = 0, 0
			if shadow.color.A == 0 {
				shadow.color = ui.RGBA{R: 1, G: 1, B: 1, A: 0.5}
			}
		}
		box := ui.Div().AnchorMode().
			ShadowSpread(shadow.blur, shadow.dx, shadow.dy, shadow.spread, shadow.color).
			Rounded(doc.sourceRadius(sourceID, env)).
			Children(ui.Anchored(el, "", ui.Anchors{Fill: "parent"}))
		if w > 0 || h > 0 {
			box.Size(w, h)
		}
		return box, nil
	}
	return el, nil
}

// effectShadow is a DropShadow/Glow's parameters.
type effectShadow struct {
	blur, dx, dy, spread float32
	color                ui.RGBA
	stated               bool // the markup gave a radius (0 means none)
}

// sourceRadius reads the captured item's corner radius, so a shadow of a
// rounded card is rounded too.
func (doc *docState) sourceRadius(id string, env *Env) float32 {
	if id == "" || doc.nodesByID == nil {
		return 0
	}
	src := doc.nodesByID[id]
	if src == nil {
		return 0
	}
	for _, a := range src.attrs {
		if a.name == "radius" {
			if f, err := attrFloat(a, env); err == nil {
				return f
			}
		}
	}
	return 0
}

// buildByID builds the element declared with this id a second time, for the
// effect elements that "capture" another item. Nil when the id names no
// element (or the build fails), so callers can fall back to an empty box.
func (doc *docState) buildByID(id string, env *Env) ui.Element {
	if id == "" || doc.nodesByID == nil {
		return nil
	}
	src := doc.nodesByID[id]
	if src == nil {
		return nil
	}
	// Guard against an effect capturing itself (QML calls that `recursive`
	// and we do not model it).
	if doc.capturing == nil {
		doc.capturing = map[string]bool{}
	}
	if doc.capturing[id] {
		return nil
	}
	doc.capturing[id] = true
	doc.captures++ // a placement that captured is not patched (see slot)
	defer delete(doc.capturing, id)
	// The captured item is conventionally `visible: false` — Qt hides it so
	// only the effect's output shows. Since our "effect" IS the item, build
	// it visible.
	// Build it in the scope it was DECLARED in when we know it: the capture
	// may be triggered from inside a component whose `root` is a different
	// object than the one the captured item's bindings name.
	scope := env
	if obj := doc.elemObjs.get(doc.here(id)); obj != nil && obj.nodeEnv != nil {
		scope = obj.nodeEnv
	}
	el, err := build(stripVisible(stripAnchorAttrs(src)), scope, doc)
	if err != nil {
		logf("gml: shader source %s: %v", id, err)
		return nil
	}
	return el
}

// sourceRef resolves an effect's source expression to the id of the item it
// names: a bare id directly (no evaluation needed), or any expression whose
// VALUE is an element — `sourceItem: root.backgroundItem`, a property
// holding the Item to capture, which is how a reusable glass card is handed
// the background it frosts.
func (doc *docState) sourceRef(e expr, env *Env) string {
	if e == nil {
		return ""
	}
	if id, ok := e.(*identExpr); ok {
		return id.name
	}
	v, err := e.eval(env)
	if err != nil {
		return ""
	}
	if r, ok := v.(Ref); ok {
		v = r.get()
	}
	if obj, ok := v.(*elemObject); ok && obj.node != nil {
		return nodeID(obj.node)
	}
	return ""
}

// sizedBox is an empty box at the requested size (or nothing when unsized).
func sizedBox(w, h float32) ui.Element {
	if w > 0 || h > 0 {
		return ui.Div().Size(w, h)
	}
	return ui.Div()
}

// stripVisible drops a node's `visible` attribute, for the effect elements
// that render an item their author hid.
func stripVisible(n *node) *node {
	has := false
	for _, a := range n.attrs {
		if a.name == "visible" {
			has = true
			break
		}
	}
	if !has {
		return n
	}
	cp := *n
	cp.attrs = nil
	for _, a := range n.attrs {
		if a.name != "visible" {
			cp.attrs = append(cp.attrs, a)
		}
	}
	return &cp
}

// buildBySourceValue builds the item an effect's `source`/`sourceItem`
// attribute EVALUATES to, for the cases where it is not a bare id in this
// file: a component property holding the item, or a use-site value frozen
// into the instance.
// buildBySourceValue builds the item an effect's source names when it is
// not an id in this file but a PROPERTY the use site assigned — the shape a
// component wrapping an effect takes (`ShaderEffect { property var source }`,
// used as `LensEffect { source: photoImage }`). An effect whose own markup
// names a source is left alone: an attribute that resolved to nothing means
// the markup says there is none.
func (doc *docState) buildBySourceValue(n *node, env *Env) ui.Element {
	for _, a := range n.attrs {
		switch a.name {
		case "source", "sourceItem", "src":
			return nil
		}
	}
	for _, name := range []string{"source", "sourceItem"} {
		v, ok := env.lookup(name)
		if !ok {
			continue
		}
		if r, ok := v.(Ref); ok {
			v = r.get()
		}
		obj, ok := v.(*elemObject)
		if !ok || obj.node == nil {
			continue
		}
		if id := nodeID(obj.node); id != "" {
			if el := doc.buildByID(id, env); el != nil {
				return el
			}
		}
	}
	return nil
}
