package gml

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // sourceSize reads the header of whatever the page loads
	_ "image/jpeg" //
	_ "image/png"  //
	"os"
	"path/filepath"
	"strings"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/icons"
)

// QML Image element. The source is a file path (relative to the document, or
// absolute); common URL-ish schemes are tolerated. Raster bytes are read once
// into a retained ImageCell (keyed by source) and decoded+uploaded lazily on
// first paint, so the per-frame view rebuild doesn't re-read or re-upload.
// A .svg source renders as vector paths and honors `rotation:` (degrees,
// around the center — how QML spins an image). fillMode fits, stretches,
// crops, tiles or pads; a BorderImage is a 9-patch and an AnimatedImage
// plays by its own clock (animatedimage.go).
func buildImage(n *node, env *Env, doc *docState) (ui.Element, error) {
	var src string
	var w, h, rotDeg float32
	var stretch, crop, grow, sized bool
	// A BorderImage is a 9-patch: the border insets (source pixels) keep
	// their size, and the edges and middle fill by the tile modes.
	var fill ui.ImageFill
	var border [4]float32 // left, top, right, bottom
	hTile, vTile := render.PatchStretch, render.PatchStretch
	if n.typeName == "BorderImage" {
		stretch = true
	}
	var anim animatedImageAttrs
	// QML exposes the decoded image's natural size as `sourceSize`, which
	// styles size their icons from (`width: sourceSize.width`). Resolve it
	// before the attribute pass, since a size attribute may come first.
	defer shadowSourceSize(n, env, doc)()
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "grow": // extension (flexbox): fill the parent box on both axes
			b, err := attrBool(a, env)
			if err != nil {
				return nil, err
			}
			grow = b
		case "border.left", "border.top", "border.right", "border.bottom":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			border[map[string]int{"border.left": 0, "border.top": 1, "border.right": 2, "border.bottom": 3}[a.name]] = f
		case "horizontalTileMode", "verticalTileMode":
			if v, err := a.expr.eval(env); err == nil {
				m := patchModeOf(v)
				if a.name == "horizontalTileMode" {
					hTile = m
				} else {
					vTile = m
				}
			}
		case "playing", "paused", "speed", "currentFrame":
			if err := anim.read(a, env); err != nil {
				return nil, err
			}
		case "frameCount", "onFrameChanged", "onCurrentFrameChanged", "onPlayingChanged", "onPausedChanged":
			// read-only, or handlers this engine does not run
		case "fillMode":
			// Image.Stretch fills the box ignoring aspect;
			// PreserveAspectCrop covers it and clips the overflow;
			// PreserveAspectFit (the default) fits inside it.
			// The enum resolves to its NUMBER (Image.PreserveAspectCrop is
			// 2), so the value is read numerically; a page that states the
			// name as a string is honoured too.
			if v, err := a.expr.eval(env); err == nil {
				stretch, crop = fillModeFlags(v)
				fill = fillModeTile(v)
			}
		case "source":
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			src = str(v)
		case "rotation":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			rotDeg = f
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w, sized = f, true
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			h, sized = f, true
		case "visible":
			if b, err := attrBool(a, env); err == nil && !b {
				return ui.Div(), nil // hidden: no space, no hits
			}
		case "asynchronous", "cache", "smooth", "mipmap", "antialiasing",
			"sourceSize.width", "sourceSize.height", // decode is eager+cached
			// layer effects (tinting, blur) aren't modeled; the image still
			// draws, untinted:
			"layer.enabled", "layer.effect", "layer.samples",
			"opacity", "z", "scale", "clip", "horizontalAlignment",
			"verticalAlignment", "onStatusChanged", "status", "progress":
			// accepted, not modeled
		default:
			return nil, fmt.Errorf("gml:%d: Image has no attribute %q", a.line, a.name)
		}
	}
	// An explicit size that came out zero means zero — a style sizing its
	// icon from a value we don't model (sourceSize) must not fall back to
	// the default square and paint a huge icon.
	if sized && w <= 0 && h <= 0 {
		return ui.Div(), nil
	}
	// An unsized image is its SOURCE's size, as in QML — the item's
	// implicit size. The corpus' 9-patch tiles are 90x90 sources whose
	// stated size a layout strips (the layout owns it), and falling back
	// to a fixed square made them 80x80 and squeezable, where Qt hands
	// them their 90 and lets the grid overflow. The square is the last
	// resort, for a source we cannot measure at all.
	if w == 0 && h == 0 {
		if nw, nh := doc.rasterSize(src); nw > 0 && nh > 0 {
			w, h = float32(nw), float32(nh)
		}
	}
	switch {
	case w == 0 && h == 0:
		w, h = 80, 80
	case w == 0:
		w = h
	case h == 0:
		h = w
	}
	// Vector source: parsed once (cached), drawn as paths with rotation.
	if isVectorSource(src) {
		svg := doc.svgFor(src)
		if svg == nil {
			return ui.Div().Size(w, h), nil // unreadable/unparsable: empty box
		}
		el := ui.SVG(svg, w, h).Rotate(rotDeg * math32.Pi / 180)
		// fillMode applies to a vector source too: the corpus' FillMode
		// demo stretches the Qt logo, and a uniform fit letterboxed it.
		if stretch {
			el.Stretch()
		}
		if crop {
			el.Crop()
		}
		if grow {
			return ui.Div().Grow().AnchorMode().
				Children(ui.Anchored(el, "", ui.Anchors{Fill: "parent"})), nil
		}
		return el, nil
	}
	img := ui.LazyImage(doc.imageCell(src), w, h)
	// QML reports the SOURCE's own pixel size as the item's implicit size,
	// whatever box it lands in (see ui.LazyImageEl.Natural).
	if nw, nh := doc.rasterSize(src); nw > 0 && nh > 0 {
		img.Natural(float32(nw), float32(nh))
		// ... and on the id object, so a handler that runs BEFORE the
		// first layout still reads it (the blueprint viewer's
		// `Component.onCompleted: fitToView()` divides by it).
		doc.seedNatural(n, float32(nw), float32(nh))
	}
	if crop {
		img.Crop()
	}
	if stretch {
		img.Stretch()
	}
	if n.typeName == "BorderImage" {
		img.NinePatch(border[0], border[1], border[2], border[3], hTile, vTile)
	} else if fill != ui.FillFit {
		img.Fill(fill)
	}
	if n.typeName == "AnimatedImage" {
		doc.wireAnimatedImage(n, img, src, anim)
	}
	if grow {
		img.Grow()
	}
	return img, nil
}

// svgFor returns the parsed SVG for a source, parsing it once (nil when the
// file is missing or unsupported; the failure is logged once).
// "icon:name" and "icon:name/color" sources resolve through the ui/icons
// registry instead of the filesystem (so they work on js/wasm too).
func (doc *docState) svgFor(src string) *render.SVG {
	if s, ok := doc.svgs[src]; ok {
		return s
	}
	if name, ok := strings.CutPrefix(src, "icon:"); ok {
		var svg *render.SVG
		name, color, _ := strings.Cut(name, "/")
		if s, err := icons.Parse(name, color); err == nil {
			svg = s
		} else {
			logf("gml: Image source %q: %v", src, err)
		}
		doc.svgs[src] = svg
		return svg
	}
	var svg *render.SVG
	if path := resolveImagePath(doc.baseDir, src); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if s, err := render.ParseSVG(data); err == nil {
				svg = s
			} else {
				logf("gml: Image source %q: %v", src, err)
			}
		} else {
			logf("gml: Image source %q: %v", src, err)
		}
	}
	doc.svgs[src] = svg // negative results cached too
	return svg
}

// imageCell returns the retained cell for a source, reading its bytes once.
func (doc *docState) imageCell(src string) *ui.ImageCell {
	if c := doc.images[src]; c != nil {
		return c
	}
	c := &ui.ImageCell{}
	if path := resolveImagePath(doc.baseDir, src); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			c.Data = data
		} else {
			logf("gml: Image source %q: %v", src, err)
		}
	}
	doc.images[src] = c
	return c
}

// resolveImagePath maps a QML source to a filesystem path: it strips
// file://, qrc:/ and similar prefixes and resolves relative paths against the
// document directory. Remote (http/https) sources are not fetched (v1).
func resolveImagePath(baseDir, src string) string {
	if src == "" {
		return ""
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return "" // remote fetch not supported yet
	}
	// A query ("thumb.png?v=3") names a version of the same file: the
	// image cache keys on the whole source, so changing it reloads.
	if i := strings.IndexByte(src, '?'); i >= 0 {
		src = src[:i]
	}
	src = strings.TrimPrefix(src, "file://")
	src = strings.TrimPrefix(src, "qrc:")
	src = strings.TrimPrefix(src, "qrc")
	if filepath.IsAbs(src) {
		// Absolute qrc-style paths ("/assets/icons/x.svg") are project-tree
		// paths, not filesystem ones: try as-is, then search the document's
		// ancestor directories and their imports/ roots (the module-loader
		// convention).
		if _, err := os.Stat(src); err == nil {
			return src
		}
		rel := strings.TrimPrefix(src, "/")
		for dir := baseDir; dir != "" && dir != "/"; dir = filepath.Dir(dir) {
			if p := filepath.Join(dir, rel); statFile(p) {
				return p
			}
			if p := filepath.Join(dir, "imports", rel); statFile(p) {
				return p
			}
		}
		return src
	}
	if baseDir == "" {
		return src
	}
	return filepath.Join(baseDir, src)
}

// statFile reports whether p exists and is a regular file.
func statFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// shadowSourceSize binds `sourceSize` for the duration of the image's build
// (QML item scope), restoring the previous binding afterwards.
//
// A raster source reports its real decoded size, which is what QML does and
// what a page bounding an image by it needs (`width: Math.min(parent.width,
// sourceSize.width)` — zero there collapsed the image and everything
// anchored to it).
//
// A VECTOR source reports the size the document DECLARES — its width/height
// attributes, which is what Qt reports (48x48 for a Material icon) — and
// not its viewBox, which for those icons is 960 units and would paint an
// icon across the whole page. Reporting zero instead collapsed every icon a
// style sizes this way: `Image { width: Style.resize(sourceSize.width) }`
// is how the corpus' ToolButton draws its icon, and it drew nothing.
func shadowSourceSize(n *node, env *Env, doc *docState) func() {
	w, h := 0.0, 0.0
	if src := imageSourceOf(n, env); src != "" {
		if isVectorSource(src) {
			if svg := doc.svgFor(src); svg != nil {
				iw, ih := svg.IntrinsicSize()
				w, h = float64(iw), float64(ih)
			}
		} else {
			w, h = doc.rasterSize(src)
		}
	}
	prev, had := env.vars["sourceSize"]
	env.vars["sourceSize"] = map[string]any{"width": w, "height": h}
	// Computed from the decoded image, not injected: not a mutable read.
	if env.derivedNames == nil {
		env.derivedNames = map[string]bool{}
	}
	env.derivedNames["sourceSize"] = true
	return func() {
		delete(env.derivedNames, "sourceSize")
		if had {
			env.vars["sourceSize"] = prev
		} else {
			delete(env.vars, "sourceSize")
		}
	}
}

// isVectorSource reports whether a source renders as paths rather than
// pixels (an .svg file or a registry icon).
func isVectorSource(src string) bool {
	return strings.HasSuffix(strings.ToLower(src), ".svg") || strings.HasPrefix(src, "icon:")
}

// imageSourceOf reads an Image's `source` before the attribute pass, so
// sourceSize is known while the other attributes are evaluated.
func imageSourceOf(n *node, env *Env) string {
	for _, a := range n.attrs {
		if a.name != "source" || a.expr == nil {
			continue
		}
		if v, err := a.expr.eval(env); err == nil {
			return str(v)
		}
	}
	return ""
}

// rasterSize is a raster source's pixel size, read from its header once per
// document (decoding the whole image just to size it would be waste).
func (doc *docState) rasterSize(src string) (float64, float64) {
	if doc.rasterSizes == nil {
		doc.rasterSizes = map[string][2]float64{}
	}
	if wh, ok := doc.rasterSizes[src]; ok {
		return wh[0], wh[1]
	}
	var wh [2]float64
	if c := doc.imageCell(src); c != nil && len(c.Data) > 0 {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(c.Data)); err == nil {
			wh = [2]float64{float64(cfg.Width), float64(cfg.Height)}
		}
	}
	doc.rasterSizes[src] = wh
	return wh[0], wh[1]
}

// fillModeFlags reads an Image.fillMode value: QML's enum numbers (Stretch
// 0, PreserveAspectFit 1, PreserveAspectCrop 2) or the name as a string.
// The tiling modes and Pad are fillModeTile's.
func fillModeFlags(v any) (stretch, crop bool) {
	if f, err := toFloat32(v); err == nil {
		if _, isStr := v.(string); !isStr {
			return f == 0, f == 2
		}
	}
	s := str(v)
	return strings.Contains(s, "Stretch"), strings.Contains(s, "PreserveAspectCrop")
}
