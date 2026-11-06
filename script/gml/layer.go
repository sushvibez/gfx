package gml

// QML's `layer.enabled` and the blur effects that ride on it.
//
// An item with `layer.enabled: true` is rendered into its own texture and
// composited once — which is what lets `layer.effect` post-process it. The
// effect we can run honestly is a blur (MultiEffect's blurEnabled,
// Qt5Compat's GaussianBlur / FastBlur / RecursiveBlur), and it is the one
// the pages care about: frosted glass is a blurred capture of whatever sits
// behind the card.
//
// The other effect a style leans on is COLORIZATION — `MultiEffect {
// colorization: 1; colorizationColor: c }` over an icon, which is how a
// Controls style tints one. That needs no texture: the source is recoloured
// as it is drawn (ui SVGEl/LazyImageEl Colorize), so a colorize-only layer
// stays a plain element.
//
// Everything else an effect declares — masks, levels — still renders its
// source unshaded (see shadereffect.go).

import (
	"github.com/sushvibez/gfx/ui"
)

// layerFor returns the retained offscreen target for an element key. GPU
// textures must survive the per-frame rebuild, so they live here.
func (doc *docState) layerFor(key string) *ui.LayerState {
	if doc.layers == nil {
		doc.layers = map[string]*ui.LayerState{}
	}
	st := doc.layers[key]
	if st == nil {
		st = ui.NewLayerState()
		doc.layers[key] = st
	}
	return st
}

// layerSpec is what an item's `layer.*` attributes ask for.
type layerSpec struct {
	enabled bool
	blur    float32 // gaussian radius in pixels (0 = plain layer)
	opacity float32 // 1 unless the effect fades the whole layer
	shadow  *shadowSpec

	// colorize recolours the source (no texture needed); applied to the
	// element itself, so it survives spec.enabled being false.
	colorize    bool
	colorizeCol ui.RGBA
}

// shadowSpec is MultiEffect's drop shadow: the layer's own silhouette,
// offset and blurred under it.
type shadowSpec struct {
	dx, dy, blur float32
	color        ui.RGBA
}

// itemLayer reads an item's layer.enabled / layer.effect. A layer with no
// effect we can run is not worth a texture, so it reports enabled only when
// the effect gives us something to do.
func itemLayer(n *node, env *Env) layerSpec {
	spec := layerSpec{opacity: 1}
	var effect *node
	for _, a := range n.attrs {
		switch a.name {
		case "layer.enabled":
			if b, err := attrBool(a, env); err == nil {
				spec.enabled = b
			}
		case "layer.effect":
			if a.elem != nil {
				effect = a.elem
			}
		}
	}
	if !spec.enabled || effect == nil {
		return layerSpec{opacity: 1}
	}

	spec.blur = effectBlurRadius(effect, env)
	spec.shadow = effectShadowSpec(effect, env)
	spec.colorizeCol, spec.colorize = effectColorize(effect, env)
	spec.enabled = spec.blur > 0 || spec.shadow != nil
	return spec
}

// effectBlurRadius is the blur an effect element asks for, in pixels; 0
// when it is not a blur (or is switched off).
func effectBlurRadius(n *node, env *Env) float32 {
	num := func(name string, def float32) float32 {
		for _, a := range n.attrs {
			if a.name == name && a.expr != nil {
				if f, err := attrFloat(a, env); err == nil {
					return f
				}
			}
		}
		return def
	}
	on := func(name string, def bool) bool {
		for _, a := range n.attrs {
			if a.name == name && a.expr != nil {
				if b, err := attrBool(a, env); err == nil {
					return b
				}
			}
		}
		return def
	}
	switch n.typeName {
	case "MultiEffect":
		// Qt 6: `blur` is 0..1 of `blurMax` (32 by default), scaled by
		// blurMultiplier.
		if !on("blurEnabled", false) {
			return 0
		}
		amount := num("blur", 0)
		mx := num("blurMax", 32)
		mult := num("blurMultiplier", 0)
		r := amount * mx * (1 + mult)
		return clampBlur(r)
	case "GaussianBlur", "FastBlur", "MaskedBlur", "RecursiveBlur":
		// Qt5Compat: `radius` is the blur extent in pixels. RecursiveBlur
		// applies it `loops` times, which widens it.
		r := num("radius", 0)
		if n.typeName == "RecursiveBlur" {
			if loops := num("loops", 1); loops > 1 {
				r *= loops * 0.5
			}
		}
		return clampBlur(r)
	}
	return 0
}

// effectColorize reads MultiEffect's colorization: the colour the source is
// recoloured to, and whether it applies at all. Qt mixes between the source
// and the colorized version by `colorization` (0..1); we do the mix at the
// halfway point rather than blending two draws — the styles that use it set
// it to 1.
func effectColorize(n *node, env *Env) (ui.RGBA, bool) {
	if n.typeName != "MultiEffect" {
		return ui.RGBA{}, false
	}
	amount := float32(0)
	col := ui.RGBA{R: 1, G: 1, B: 1, A: 1}
	for _, a := range n.attrs {
		if a.expr == nil {
			continue
		}
		switch a.name {
		case "colorization":
			if f, err := attrFloat(a, env); err == nil {
				amount = f
			}
		case "colorizationColor":
			if c, err := attrColor(a, env); err == nil {
				col = c
			}
		}
	}
	if amount < 0.5 {
		return ui.RGBA{}, false
	}
	return col, true
}

// effectShadowSpec reads MultiEffect's drop-shadow properties; nil when the
// effect casts no shadow. Qt's shadowBlur is a 0..1 fraction of blurMax,
// like `blur` itself.
func effectShadowSpec(n *node, env *Env) *shadowSpec {
	if n.typeName != "MultiEffect" {
		return nil
	}
	on := false
	sh := shadowSpec{blur: 8, color: ui.RGBA{A: 0.5}}
	mx := float32(32)
	for _, a := range n.attrs {
		if a.name == "blurMax" && a.expr != nil {
			if f, err := attrFloat(a, env); err == nil && f > 0 {
				mx = f
			}
		}
	}
	for _, a := range n.attrs {
		if a.expr == nil {
			continue
		}
		switch a.name {
		case "shadowEnabled":
			if b, err := attrBool(a, env); err == nil {
				on = b
			}
		case "shadowHorizontalOffset":
			if f, err := attrFloat(a, env); err == nil {
				sh.dx = f
			}
		case "shadowVerticalOffset":
			if f, err := attrFloat(a, env); err == nil {
				sh.dy = f
			}
		case "shadowBlur":
			if f, err := attrFloat(a, env); err == nil {
				sh.blur = clampBlur(f * mx)
			}
		case "shadowColor":
			if c, err := attrColor(a, env); err == nil {
				sh.color = c
			}
		case "shadowOpacity":
			if f, err := attrFloat(a, env); err == nil {
				sh.color.A *= f
			}
		}
	}
	if !on {
		return nil
	}
	return &sh
}

// clampBlur keeps a radius in the range the layer pipeline stays cheap and
// stable in: a huge radius would smear the item away entirely.
func clampBlur(r float32) float32 {
	switch {
	case r <= 0:
		return 0
	case r > 64:
		return 64
	default:
		return r
	}
}

// wrapLayer puts an element in its retained offscreen layer when the markup
// asked for one. Called at the end of the element's build, so the layout
// hints (grow, fills) are already on the element itself — the layer
// forwards them.
func wrapLayer(n *node, el ui.Element, env *Env, doc *docState) ui.Element {
	spec := itemLayer(n, env)
	if el == nil {
		return el
	}
	// A colorizing effect recolours the source in place — no offscreen
	// texture, and it composes with a blur/shadow layer around it.
	colorized := false
	if spec.colorize {
		switch e := el.(type) {
		case *ui.SVGEl:
			e.Colorize(spec.colorizeCol)
			colorized = true
		case *ui.LazyImageEl:
			e.Colorize(spec.colorizeCol)
			colorized = true
		}
	}
	if !spec.enabled {
		return el
	}
	// A CAPTURED copy needs its own layer state: the same markup is built a
	// second time inside an effect's capture, in that effect's coordinate
	// space, and sharing one state made the two overwrite each other's
	// texture — the second render's content was composited at the first's
	// rect, which showed up as a hard rectangular edge around every
	// blurred blob on the lens-tabs hero.
	key := doc.key(n) + "|layer"
	if len(doc.capturing) > 0 {
		key += "|cap"
	}
	l := ui.Layer(doc.layerFor(key), el).Blur(spec.blur)
	// Anything else colorizes as a GROUP: the captured texture is
	// composited by luminance, which is what a MultiEffect over a shape
	// (the corpus' neon glow: a rounded rect with a white star on it)
	// draws. Recolouring only images left the star white.
	if spec.colorize && !colorized {
		l.Colorize(spec.colorizeCol)
	}
	if spec.opacity != 1 {
		l.Opacity(spec.opacity)
	}
	if sh := spec.shadow; sh != nil {
		l.Shadow(sh.dx, sh.dy, sh.blur, sh.color)
	}
	return l
}
