package gml

// QtLocation's Map with its overlay items: MapPolyline, MapQuickItem,
// MapItemView, MapCircle and MapRectangle.
//
// There is NO TILE SOURCE here — nothing in this framework fetches map
// imagery — so a Map draws no basemap. What it does do is the part the
// overlays depend on: a real Web-Mercator projection around `center` at
// `zoomLevel`, so a route line, its waypoints and a moving aircraft marker
// land in the right places relative to each other and to the viewport, and
// panning/zooming moves them the way it should. Plugin/PluginParameter are
// accepted and ignored.
//
// center and zoomLevel are engine state, so `map.center =
// QtPositioning.coordinate(lat, lon)` and `map.zoomLevel += 1` work from
// handlers, and toCoordinate/fromCoordinate invert the projection.

import (
	"fmt"
	"time"

	"github.com/chewxy/math32"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// mapProjection is a Web-Mercator projection fixed to one viewport: the
// pixel size of the world at this zoom, and where the centre sits.
type mapProjection struct {
	world    float32 // world width in pixels at this zoom
	cx, cy   float32 // centre in world pixels
	w, h     float32 // viewport size
	zoom     float32
	lat, lon float32
}

// newMapProjection builds the projection for a centre, zoom and viewport.
func newMapProjection(lat, lon, zoom, w, h float32) mapProjection {
	p := mapProjection{world: 256 * pow2(zoom), zoom: zoom, w: w, h: h, lat: lat, lon: lon}
	p.cx, p.cy = p.worldOf(lat, lon)
	return p
}

// pow2 is 2^n for a fractional zoom level.
func pow2(n float32) float32 { return math32.Exp2(n) }

// worldOf projects a coordinate to world pixels.
func (p mapProjection) worldOf(lat, lon float32) (float32, float32) {
	x := (lon + 180) / 360 * p.world
	// Clamp the latitude to Mercator's valid range before the log.
	if lat > 85.05112878 {
		lat = 85.05112878
	}
	if lat < -85.05112878 {
		lat = -85.05112878
	}
	s := math32.Sin(lat * math32.Pi / 180)
	y := (0.5 - math32.Log((1+s)/(1-s))/(4*math32.Pi)) * p.world
	return x, y
}

// screenOf projects a coordinate to viewport pixels.
func (p mapProjection) screenOf(lat, lon float32) (float32, float32) {
	x, y := p.worldOf(lat, lon)
	return x - p.cx + p.w/2, y - p.cy + p.h/2
}

// coordinateAt inverts screenOf: viewport pixels back to lat/lon.
func (p mapProjection) coordinateAt(sx, sy float32) (float32, float32) {
	x := sx - p.w/2 + p.cx
	y := sy - p.h/2 + p.cy
	lon := x/p.world*360 - 180
	n := math32.Pi * (1 - 2*y/p.world)
	lat := 180 / math32.Pi * math32.Atan(math32.Sinh(n))
	return lat, lon
}

// mapState is a Map element's retained view: where it looks and how close.
// The three are engine properties, so a handler panning or zooming the map
// invalidates the frame like any other property write, and the bindings that
// read them (a label showing the zoom, a marker hidden below a zoom
// threshold) are tracked.
type mapState struct {
	lat, lon Ref
	zoom     Ref
}

func (doc *docState) mapFor(key string) *mapState {
	if doc.maps == nil {
		doc.maps = map[string]*mapState{}
	}
	if m, ok := doc.maps[key]; ok {
		return m
	}
	m := &mapState{
		lat:  doc.floatProp(key+".center.latitude", 0),
		lon:  doc.floatProp(key+".center.longitude", 0),
		zoom: doc.floatProp(key+".zoomLevel", 8),
	}
	doc.maps[key] = m
	return m
}

// mapNum reads one of the map's numeric properties.
func mapNum(r Ref) float32 {
	f, _ := toFloat32(r.get())
	return f
}

// buildMap renders a Map: an anchor-mode box holding its projected overlays.
func buildMap(n *node, env *Env, doc *docState) (ui.Element, error) {
	key := doc.key(n)
	st := doc.mapFor(key)
	var w, h float32
	grow := false
	minZoom, maxZoom := float32(0), float32(20)
	seeded := doc.mapSeeded == nil || !doc.mapSeeded[key]
	for _, a := range n.attrs {
		switch a.name {
		case "center":
			// The declared centre seeds the state once; after that the state
			// is authoritative, since handlers assign it (`map.center = …`).
			if seeded {
				if v, err := a.expr.eval(env); err == nil {
					if lat, lon, ok := asCoordinate(v); ok {
						_, _ = st.lat.set(lat), st.lon.set(lon)
					}
				}
			}
		case "zoomLevel":
			if seeded {
				if f, err := attrFloat(a, env); err == nil {
					_ = st.zoom.set(f)
				}
			}
		case "minimumZoomLevel":
			if f, err := attrFloat(a, env); err == nil {
				minZoom = f
			}
		case "maximumZoomLevel":
			if f, err := attrFloat(a, env); err == nil {
				maxZoom = f
			}
		case "width":
			if f, err := attrFloat(a, env); err == nil {
				w = f
			}
		case "height":
			if f, err := attrFloat(a, env); err == nil {
				h = f
			}
		case "anchors.fill":
			grow = true
		case "id", "plugin", "activeMapType", "copyrightsVisible", "color",
			"gesture.enabled", "gesture.acceptedGestures", "bearing", "tilt",
			"fieldOfView", "clip", "z", "opacity", "visible", "enabled",
			"anchors.margins", "anchors.centerIn", "objectName", "supportedMapTypes":
			// accepted, applied below or not modeled
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: Map has no attribute %q", a.line, a.name)
		}
	}
	if doc.mapSeeded == nil {
		doc.mapSeeded = map[string]bool{}
	}
	doc.mapSeeded[key] = true
	if z := mapNum(st.zoom); z < minZoom || z > maxZoom {
		_ = st.zoom.set(min(max(z, minZoom), maxZoom))
	}

	// The viewport size: this frame's box if it has been laid out, else the
	// declared size. The projection needs real pixels, so the first frame
	// may place items with a stale size and settle on the next one.
	vw, vh := w, h
	if pe, ok := doc.prevElems.get2(doc.intern(key)); ok {
		b := ui.Bounds(pe)
		if b.W > 0 && b.H > 0 {
			vw, vh = b.W, b.H
		}
	}
	if vw <= 0 {
		vw = 320
	}
	if vh <= 0 {
		vh = 240
	}
	proj := newMapProjection(mapNum(st.lat), mapNum(st.lon), mapNum(st.zoom), vw, vh)

	// The QML contract: writable centre and zoom, plus the projection API.
	doc.exposeIDProp(n, "center", mapCenterRef{st})
	doc.exposeIDProp(n, "zoomLevel", mapZoomRef{st: st, min: minZoom, max: maxZoom})
	doc.exposeIDProp(n, "minimumZoomLevel", float64(minZoom))
	doc.exposeIDProp(n, "maximumZoomLevel", float64(maxZoom))
	doc.exposeIDMethods(n, map[string]any{
		"toCoordinate": jsFunc(func(args []any) (any, error) {
			var x, y float32
			if len(args) > 0 {
				if pt, ok := args[0].(map[string]any); ok {
					x, _ = toFloat32(pt["x"])
					y, _ = toFloat32(pt["y"])
				}
			}
			lat, lon := proj.coordinateAt(x, y)
			return coordinateValue(lat, lon), nil
		}),
		"fromCoordinate": jsFunc(func(args []any) (any, error) {
			if len(args) > 0 {
				if lat, lon, ok := asCoordinate(args[0]); ok {
					x, y := proj.screenOf(lat, lon)
					return map[string]any{"x": float64(x), "y": float64(y)}, nil
				}
			}
			return map[string]any{"x": float64(0), "y": float64(0)}, nil
		}),
		"pan": jsFunc(func(args []any) (any, error) {
			dx, dy := float32(0), float32(0)
			if len(args) > 0 {
				dx, _ = toFloat32(args[0])
			}
			if len(args) > 1 {
				dy, _ = toFloat32(args[1])
			}
			lat, lon := proj.coordinateAt(vw/2+dx, vh/2+dy)
			_, _ = st.lat.set(lat), st.lon.set(lon)
			return nil, nil
		}),
		"fitViewportToVisibleMapItems": jsFunc(func([]any) (any, error) { return nil, nil }),
		"clearMapItems":                jsFunc(func([]any) (any, error) { return nil, nil }),
		"addMapItem":                   jsFunc(func([]any) (any, error) { return nil, nil }),
	})

	box := ui.Div().AnchorMode().Size(vw, vh)
	if grow {
		box.Grow()
	}
	// Overlays, in declaration order (later ones paint on top).
	for _, c := range n.children {
		els, err := doc.buildMapItem(c, env, proj)
		if err != nil {
			if doc.lenient {
				logf("gml: %v (element skipped)", err)
				continue
			}
			return nil, err
		}
		box.Children(els...)
	}
	return box, nil
}

// buildMapItem builds one overlay of a Map, positioned by the projection.
func (doc *docState) buildMapItem(n *node, env *Env, proj mapProjection) ([]ui.Element, error) {
	switch n.typeName {
	case "MapPolyline", "MapPolygon":
		return doc.buildMapPolyline(n, env, proj)
	case "MapCircle":
		return doc.buildMapCircle(n, env, proj)
	case "MapQuickItem":
		el, err := doc.buildMapQuickItem(n, env, proj)
		if el == nil || err != nil {
			return nil, err
		}
		return []ui.Element{el}, nil
	case "MapItemView":
		return doc.buildMapItemView(n, env, proj)
	case "Plugin", "PluginParameter":
		return nil, nil
	}
	if isNonVisual(n) {
		doc.refreshNonVisual(n, env)
		return nil, nil
	}
	// A plain item declared inside the Map (an overlay, a MouseArea): build
	// it as an ordinary anchored child.
	el, err := build(n, env, doc)
	if err != nil || el == nil {
		return nil, err
	}
	return []ui.Element{el}, nil
}

// buildMapPolyline draws a line (or filled polygon) through a path of
// coordinates.
func (doc *docState) buildMapPolyline(n *node, env *Env, proj mapProjection) ([]ui.Element, error) {
	var path []any
	width := float32(2)
	col := ui.RGB(0x00d1a9)
	fill := false
	for _, a := range n.attrs {
		switch a.name {
		case "path":
			if v, err := a.expr.eval(env); err == nil {
				if arr, ok := v.([]any); ok {
					path = arr
				}
			}
		case "line.width":
			if f, err := attrFloat(a, env); err == nil && f > 0 {
				width = f
			}
		case "line.color", "border.color":
			if c, err := attrColor(a, env); err == nil {
				col = c
			}
		case "color":
			if c, err := attrColor(a, env); err == nil {
				col = c
				fill = n.typeName == "MapPolygon"
			}
		}
	}
	// The path may be assigned from a handler (`routeLine.path = coords`),
	// which lands on the id object rather than the attribute.
	if len(path) == 0 {
		if id := nodeID(n); id != "" {
			if obj := doc.elemObjs.get(doc.here(id)); obj != nil {
				if v, ok := obj.member("path"); ok {
					if arr, ok := v.([]any); ok {
						path = arr
					}
				}
			}
		}
	}
	// `path` is writable so the assignment above has somewhere to go.
	pathRef := doc.varProp(doc.key(n)+".path", path)
	doc.exposeIDProp(n, "path", pathRef)
	if v, ok := pathRef.get().([]any); ok && len(v) > len(path) {
		path = v // assigned from a handler since the last build
	}
	if len(path) < 2 {
		return nil, nil
	}
	pts := make([][2]float32, 0, len(path))
	for _, c := range path {
		if lat, lon, ok := asCoordinate(c); ok {
			x, y := proj.screenOf(lat, lon)
			pts = append(pts, [2]float32{x, y})
		}
	}
	if len(pts) < 2 {
		return nil, nil
	}
	canvas := ui.NewCanvas(proj.w, proj.h, func(r *render.Renderer2D, rect ui.Rect) {
		p := render.NewPath()
		for i, pt := range pts {
			x, y := rect.X+pt[0], rect.Y+pt[1]
			if i == 0 {
				p.MoveTo(x, y)
			} else {
				p.LineTo(x, y)
			}
		}
		if fill {
			c := col
			c.A *= 0.4
			r.FillPath(p.Close(), c)
		}
		r.StrokePathRound(p, width, col)
	})
	return []ui.Element{ui.Anchored(canvas, "", ui.Anchors{Fill: "parent"})}, nil
}

// buildMapCircle draws a circle of a radius in metres around a coordinate.
func (doc *docState) buildMapCircle(n *node, env *Env, proj mapProjection) ([]ui.Element, error) {
	var lat, lon, radius float32
	col := ui.RGBA{R: 0, G: 0.82, B: 0.66, A: 0.3}
	border := ui.RGB(0x00d1a9)
	borderW := float32(1)
	for _, a := range n.attrs {
		switch a.name {
		case "center":
			if v, err := a.expr.eval(env); err == nil {
				if la, lo, ok := asCoordinate(v); ok {
					lat, lon = la, lo
				}
			}
		case "radius":
			if f, err := attrFloat(a, env); err == nil {
				radius = f
			}
		case "color":
			if c, err := attrColor(a, env); err == nil {
				col = c
			}
		case "border.color":
			if c, err := attrColor(a, env); err == nil {
				border = c
			}
		case "border.width":
			if f, err := attrFloat(a, env); err == nil {
				borderW = f
			}
		}
	}
	if radius <= 0 {
		return nil, nil
	}
	// Metres per pixel at this latitude, from the Mercator scale.
	mpp := 156543.03392 * math32.Cos(lat*math32.Pi/180) / pow2(proj.zoom)
	rpx := radius / mpp
	x, y := proj.screenOf(lat, lon)
	circle := ui.Div().Size(rpx*2, rpx*2).Rounded(rpx).Bg(col).Border(borderW, border)
	return []ui.Element{ui.Anchored(circle, "", ui.Anchors{
		X: x - rpx, Y: y - rpx, HasX: true, HasY: true})}, nil
}

// buildMapQuickItem places an item's sourceItem at a coordinate, offset by
// its anchorPoint.
func (doc *docState) buildMapQuickItem(n *node, env *Env, proj mapProjection) (ui.Element, error) {
	var lat, lon, ax, ay float32
	var source *node
	visible := true
	for _, a := range n.attrs {
		switch a.name {
		case "coordinate":
			if v, err := a.expr.eval(env); err == nil {
				if la, lo, ok := asCoordinate(v); ok {
					lat, lon = la, lo
				}
			}
		case "anchorPoint.x":
			if f, err := attrFloat(a, env); err == nil {
				ax = f
			}
		case "anchorPoint.y":
			if f, err := attrFloat(a, env); err == nil {
				ay = f
			}
		case "anchorPoint":
			if v, err := a.expr.eval(env); err == nil {
				if pt, ok := v.(map[string]any); ok {
					ax, _ = toFloat32(pt["x"])
					ay, _ = toFloat32(pt["y"])
				}
			}
		case "sourceItem":
			if a.elem != nil {
				source = a.elem
			}
		case "visible":
			if b, err := attrBool(a, env); err == nil {
				visible = b
			}
		}
	}
	if source == nil {
		// `sourceItem` may also be the single child element.
		for _, c := range n.children {
			if !isNonVisual(c) {
				source = c
				break
			}
		}
	}
	if source == nil || !visible {
		return nil, nil
	}
	el, err := build(source, env, doc)
	if err != nil || el == nil {
		return nil, err
	}
	x, y := proj.screenOf(lat, lon)
	return ui.Anchored(el, "", ui.Anchors{X: x - ax, Y: y - ay, HasX: true, HasY: true}), nil
}

// buildMapItemView is a Repeater for map overlays: one delegate per model
// row, each built in that row's scope.
func (doc *docState) buildMapItemView(n *node, env *Env, proj mapProjection) ([]ui.Element, error) {
	var modelAttr *attr
	for i := range n.attrs {
		if n.attrs[i].name == "model" {
			modelAttr = &n.attrs[i]
		}
	}
	if modelAttr == nil {
		return nil, nil
	}
	delegate, err := delegateChild(n)
	if err != nil {
		return nil, err
	}
	rows, count, err := resolveModel(*modelAttr, env)
	if err != nil {
		return nil, err
	}
	viewKey := doc.key(n)
	var out []ui.Element
	for i := 0; i < count; i++ {
		var row map[string]any
		if rows != nil {
			row = rows[i]
		}
		restore := enterRow(viewKey, delegate, env, doc, row, i)
		els, err := doc.buildMapItem(delegate, env, proj)
		restore()
		if err != nil {
			return nil, err
		}
		out = append(out, els...)
	}
	return out, nil
}

// mapCenterRef makes `map.center` readable as a coordinate and assignable
// from one.
type mapCenterRef struct{ st *mapState }

func (r mapCenterRef) get() any {
	return coordinateValue(mapNum(r.st.lat), mapNum(r.st.lon))
}
func (r mapCenterRef) set(v any) error {
	lat, lon, ok := asCoordinate(v)
	if !ok {
		return fmt.Errorf("gml: center wants a coordinate")
	}
	if err := r.st.lat.set(lat); err != nil {
		return err
	}
	return r.st.lon.set(lon)
}
func (r mapCenterRef) animate(to any, _ time.Duration) error { return r.set(to) }
func (r mapCenterRef) onChange(func())                       {}

// mapZoomRef is `map.zoomLevel`, clamped to the map's limits on write.
type mapZoomRef struct {
	st       *mapState
	min, max float32
}

func (r mapZoomRef) get() any { return float64(mapNum(r.st.zoom)) }
func (r mapZoomRef) set(v any) error {
	f, err := toFloat32(v)
	if err != nil {
		return err
	}
	return r.st.zoom.set(min(max(f, r.min), r.max))
}
func (r mapZoomRef) animate(to any, _ time.Duration) error { return r.set(to) }
func (r mapZoomRef) onChange(func())                       {}

// coordinateValue is a QtPositioning coordinate as this engine holds it: a
// plain object read by component (`c.latitude`).
func coordinateValue(lat, lon float32) map[string]any {
	return map[string]any{
		"latitude": float64(lat), "longitude": float64(lon),
		"altitude": float64(0), "isValid": true,
	}
}

// asCoordinate reads a coordinate value (from QtPositioning.coordinate, or a
// plain {latitude, longitude} object).
func asCoordinate(v any) (lat, lon float32, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap {
		return 0, 0, false
	}
	la, hasLat := m["latitude"]
	lo, hasLon := m["longitude"]
	if !hasLat || !hasLon {
		return 0, 0, false
	}
	lat, _ = toFloat32(la)
	lon, _ = toFloat32(lo)
	return lat, lon, true
}

// qtPositioningNS is the QtPositioning namespace: coordinate factories and
// the geo maths these pages use.
func qtPositioningNS() map[string]any {
	return map[string]any{
		"coordinate": jsFunc(func(a []any) (any, error) {
			var lat, lon float32
			if len(a) > 0 {
				lat, _ = toFloat32(a[0])
			}
			if len(a) > 1 {
				lon, _ = toFloat32(a[1])
			}
			return coordinateValue(lat, lon), nil
		}),
		"shapeToCircle":    jsFunc(func([]any) (any, error) { return nil, nil }),
		"circle":           jsFunc(func([]any) (any, error) { return nil, nil }),
		"rectangle":        jsFunc(func([]any) (any, error) { return nil, nil }),
		"path":             jsFunc(func([]any) (any, error) { return nil, nil }),
		"polygon":          jsFunc(func([]any) (any, error) { return nil, nil }),
		"shapeToRectangle": jsFunc(func([]any) (any, error) { return nil, nil }),
	}
}

// geoDistance is the great-circle distance between two coordinates in
// metres (the sphere is close enough for a map overlay).
func geoDistance(lat1, lon1, lat2, lon2 float32) float32 {
	const r = 6371000.0
	toRad := func(d float32) float32 { return d * math32.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math32.Sin(dLat/2)*math32.Sin(dLat/2) +
		math32.Cos(toRad(lat1))*math32.Cos(toRad(lat2))*
			math32.Sin(dLon/2)*math32.Sin(dLon/2)
	return r * 2 * math32.Atan2(math32.Sqrt(a), math32.Sqrt(1-a))
}
