package gml

// QtGraphs' 3D charts: Bars3D, Scatter3D and Surface3D over their
// ItemModel*DataProxy sources.
//
// These are drawn as an ISOMETRIC PROJECTION on the 2D renderer rather than
// through the engine's 3D pipeline: the charts are diagrams, and an
// isometric plot of the same data reads the way the real ones do (a floor
// grid, extruded bars, depth-ordered points) without wiring a scene, camera
// and meshes per chart element. cameraPreset picks the projection's
// handedness; shadows, themes, selection and rotation are not modeled.

import (
	"sort"
	"strings"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// chart3DKind is which of the three graph types we are drawing.
type chart3DKind int

const (
	chartBars3D chart3DKind = iota
	chartScatter3D
	chartSurface3D
)

// cell3D is one datum: grid coordinates (row, col) for bars and surfaces,
// or free x/y/z for scatter.
type cell3D struct {
	row, col int
	x, y, z  float32
	label    string
}

// buildChart3D renders Bars3D / Scatter3D / Surface3D.
func buildChart3D(n *node, env *Env, doc *docState) (ui.Element, error) {
	kind := chartBars3D
	switch n.typeName {
	case "Scatter3D":
		kind = chartScatter3D
	case "Surface3D":
		kind = chartSurface3D
	}
	var w, h float32
	mirror := false // cameraPreset ...Left variants look from the other side
	base := ui.RGB(0x00d1a9)
	valueMin, valueMax := float32(0), float32(0)
	for _, a := range n.attrs {
		switch {
		case a.name == "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case a.name == "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			h = f
		case a.name == "cameraPreset":
			if v, err := a.expr.eval(env); err == nil {
				mirror = containsFold(str(v), "Left")
			}
		case a.name == "valueAxis.min", a.name == "axisY.min":
			if f, err := attrFloat(a, env); err == nil {
				valueMin = f
			}
		case a.name == "valueAxis.max", a.name == "axisY.max":
			if f, err := attrFloat(a, env); err == nil {
				valueMax = f
			}
		default:
			// Axes titles/formats, themes, shadow and selection settings,
			// and the rest of the 3D surface: accepted, not modeled.
		}
	}
	// Each series child carries its own colour and its data proxy; the
	// proxies are read at PAINT time, because the corpus fills their
	// ListModels from Component.onCompleted and from handlers.
	var series []series3D
	for _, c := range n.children {
		switch c.typeName {
		case "Bar3DSeries", "Scatter3DSeries", "Surface3DSeries":
			sp := series3D{color: base}
			for _, a := range c.attrs {
				switch a.name {
				case "baseColor", "color":
					if col, err := attrColor(a, env); err == nil {
						sp.color = col
					}
				case "dataProxy":
					if a.elem != nil {
						if p, ok := readProxy(a.elem, env); ok {
							sp.proxies = append(sp.proxies, p)
						}
					}
				}
			}
			for _, sub := range c.children {
				if p, ok := readProxy(sub, env); ok {
					sp.proxies = append(sp.proxies, p)
				}
			}
			// The series' own declarations still run (onCompleted, Timer).
			doc.collectCompleted(c, env)
			series = append(series, sp)
		}
	}
	if w <= 0 {
		w = 320
	}
	if h <= 0 {
		h = 240
	}
	axisMin, axisMax := valueMin, valueMax
	return ui.NewCanvas(w, h, func(r *render.Renderer2D, rect ui.Rect) {
		plotted := make([]series3D, 0, len(series))
		var all []cell3D
		for _, sp := range series {
			sp.cells = nil
			for _, p := range sp.proxies {
				sp.cells = append(sp.cells, p.cells()...)
			}
			all = append(all, sp.cells...)
			plotted = append(plotted, sp)
		}
		lo, hi := axisMin, axisMax
		if hi <= lo {
			lo, hi = dataRange(all, kind)
		}
		drawChart3D(r, rect, kind, plotted, lo, hi, mirror)
	}), nil
}

// series3D is one plotted series: its colour, its data proxies and the cells
// they resolved to on the current frame.
type series3D struct {
	color   ui.RGBA
	proxies []proxy3D
	cells   []cell3D
}

// proxy3D is a resolved ItemModel*DataProxy: the roles it maps, plus the
// model attribute and the scope needed to re-read it every frame.
type proxy3D struct {
	model                       attr
	env                         *Env
	rowRole, colRole, valueRole string
	xRole, yRole, zRole         string
}

// readProxy reads an ItemModel*DataProxy element's role mapping.
func readProxy(n *node, env *Env) (proxy3D, bool) {
	if !containsFold(n.typeName, "DataProxy") {
		return proxy3D{}, false
	}
	p := proxy3D{env: env.snapshot()}
	for _, a := range n.attrs {
		switch a.name {
		case "itemModel", "model":
			p.model = a
		case "rowRole":
			p.rowRole, _ = attrString(a, env)
		case "columnRole":
			p.colRole, _ = attrString(a, env)
		case "valueRole":
			p.valueRole, _ = attrString(a, env)
		case "xPosRole":
			p.xRole, _ = attrString(a, env)
		case "yPosRole":
			p.yRole, _ = attrString(a, env)
		case "zPosRole":
			p.zRole, _ = attrString(a, env)
		}
	}
	return p, true
}

// cells resolves the proxy's model to plot data.
func (p proxy3D) cells() []cell3D {
	rows, _, err := resolveModel(p.model, p.env)
	if err != nil {
		return nil
	}
	// Rows and columns are categorical: map each distinct label to an index
	// in first-seen order, which is what the proxies do.
	rowIdx, colIdx := map[string]int{}, map[string]int{}
	index := func(m map[string]int, key string) int {
		if i, ok := m[key]; ok {
			return i
		}
		i := len(m)
		m[key] = i
		return i
	}
	out := make([]cell3D, 0, len(rows))
	for _, row := range rows {
		c := cell3D{}
		if p.rowRole != "" {
			c.row = index(rowIdx, str(row[p.rowRole]))
		}
		if p.colRole != "" {
			c.col = index(colIdx, str(row[p.colRole]))
			c.label = str(row[p.colRole])
		}
		if p.valueRole != "" {
			c.y, _ = toFloat32(row[p.valueRole])
		}
		if p.xRole != "" {
			c.x, _ = toFloat32(row[p.xRole])
		}
		if p.yRole != "" {
			c.y, _ = toFloat32(row[p.yRole])
		}
		if p.zRole != "" {
			c.z, _ = toFloat32(row[p.zRole])
		}
		out = append(out, c)
	}
	return out
}

// dataRange is the value range to plot when the axis does not say.
func dataRange(cells []cell3D, kind chart3DKind) (float32, float32) {
	if len(cells) == 0 {
		return 0, 1
	}
	lo, hi := cells[0].y, cells[0].y
	for _, c := range cells {
		lo, hi = min(lo, c.y), max(hi, c.y)
	}
	if kind != chartScatter3D && lo > 0 {
		lo = 0 // bars and surfaces read against a zero floor
	}
	if hi <= lo {
		hi = lo + 1
	}
	return lo, hi
}

// drawChart3D plots the series in an isometric projection inside rect.
func drawChart3D(r *render.Renderer2D, rect ui.Rect, kind chart3DKind,
	series []series3D, valueMin, valueMax float32, mirror bool) {

	rows, cols, total := 1, 1, 0
	for _, sp := range series {
		total += len(sp.cells)
		for _, c := range sp.cells {
			rows = max(rows, c.row+1)
			cols = max(cols, c.col+1)
		}
	}
	if rect.W <= 0 || rect.H <= 0 || total == 0 {
		return
	}
	if kind == chartScatter3D {
		// Scatter has no grid: give it a square floor to plot onto.
		rows, cols = 8, 8
	}
	// Isometric basis: one grid step right-down for columns, left-down for
	// rows, and straight up for value. The plot is fitted into the rect.
	stepX := rect.W / float32(rows+cols+1) * 0.9
	stepY := stepX * 0.5
	height := rect.H * 0.4
	originX := rect.X + rect.W/2
	originY := rect.Y + rect.H*0.8
	sign := float32(1)
	if mirror {
		sign = -1
	}
	project := func(rowF, colF, value float32) (float32, float32) {
		t := (value - valueMin) / max(valueMax-valueMin, 0.0001)
		x := originX + sign*(colF-rowF)*stepX
		y := originY - (colF+rowF)*stepY - t*height
		return x, y
	}

	// Floor grid.
	grid := ui.RGBA{R: 0.5, G: 0.52, B: 0.56, A: 0.25}
	// A fine data grid (a 30x30 surface) still gets a readable floor.
	stride := func(n int) int { return max(n/8, 1) }
	for i := 0; i <= rows; i += stride(rows) {
		x0, y0 := project(float32(i), 0, valueMin)
		x1, y1 := project(float32(i), float32(cols), valueMin)
		r.StrokePath(render.NewPath().MoveTo(x0, y0).LineTo(x1, y1), 1, grid)
	}
	for j := 0; j <= cols; j += stride(cols) {
		x0, y0 := project(0, float32(j), valueMin)
		x1, y1 := project(float32(rows), float32(j), valueMin)
		r.StrokePath(render.NewPath().MoveTo(x0, y0).LineTo(x1, y1), 1, grid)
	}

	for _, sp := range series {
		switch kind {
		case chartBars3D:
			drawBars(r, project, sp, valueMin, stepX, stepY)
		case chartScatter3D:
			drawScatter(r, project, sp, rows, cols, stepX)
		case chartSurface3D:
			drawSurface(r, project, sp)
		}
	}
}

// drawBars draws a bar series back to front, so nearer bars overlap the ones
// behind them.
func drawBars(r *render.Renderer2D, project func(rowF, colF, v float32) (float32, float32),
	sp series3D, floor, stepX, stepY float32) {

	order := make([]cell3D, len(sp.cells))
	copy(order, sp.cells)
	sort.SliceStable(order, func(i, j int) bool {
		return order[i].row+order[i].col < order[j].row+order[j].col
	})
	for _, c := range order {
		drawBar3D(r, project, float32(c.row), float32(c.col), c.y,
			floor, stepX, stepY, sp.color)
	}
}

// drawScatter normalises the series' free x/z into the grid and plots the
// points from back to front.
func drawScatter(r *render.Renderer2D, project func(rowF, colF, v float32) (float32, float32),
	sp series3D, rows, cols int, stepX float32) {

	if len(sp.cells) == 0 {
		return
	}
	lo, hi := sp.cells[0].x, sp.cells[0].x
	zlo, zhi := sp.cells[0].z, sp.cells[0].z
	for _, c := range sp.cells {
		lo, hi = min(lo, c.x), max(hi, c.x)
		zlo, zhi = min(zlo, c.z), max(zhi, c.z)
	}
	spanX, spanZ := max(hi-lo, 0.0001), max(zhi-zlo, 0.0001)
	pts := make([]cell3D, len(sp.cells))
	copy(pts, sp.cells)
	depth := func(c cell3D) float32 {
		return (c.x-lo)/spanX + (c.z-zlo)/spanZ
	}
	sort.SliceStable(pts, func(i, j int) bool { return depth(pts[i]) < depth(pts[j]) })
	for _, c := range pts {
		rowF := (c.z - zlo) / spanZ * float32(rows)
		colF := (c.x - lo) / spanX * float32(cols)
		x, y := project(rowF, colF, c.y)
		d := max(stepX*0.3, 4)
		r.FillRoundRect(x-d/2, y-d/2, d, d, d/2, sp.color)
	}
}

// drawSurface draws the mesh as a wireframe: one polyline per row and per
// column of the grid.
func drawSurface(r *render.Renderer2D, project func(rowF, colF, v float32) (float32, float32),
	sp series3D) {

	at := map[[2]int]cell3D{}
	rows, cols := 0, 0
	for _, c := range sp.cells {
		at[[2]int{c.row, c.col}] = c
		rows, cols = max(rows, c.row+1), max(cols, c.col+1)
	}
	line := func(cellAt func(i int) (cell3D, bool), n int) {
		p := render.NewPath()
		started := false
		for i := 0; i < n; i++ {
			c, ok := cellAt(i)
			if !ok {
				continue
			}
			x, y := project(float32(c.row), float32(c.col), c.y)
			if !started {
				p.MoveTo(x, y)
				started = true
			} else {
				p.LineTo(x, y)
			}
		}
		if started {
			r.StrokePathRound(p, 1, sp.color)
		}
	}
	for i := 0; i < rows; i++ {
		row := i
		line(func(j int) (cell3D, bool) { c, ok := at[[2]int{row, j}]; return c, ok }, cols)
	}
	for j := 0; j < cols; j++ {
		col := j
		line(func(i int) (cell3D, bool) { c, ok := at[[2]int{i, col}]; return c, ok }, rows)
	}
}

// drawBar3D draws one extruded bar: top face, then the two visible sides,
// shaded so the solid reads as a box.
func drawBar3D(r *render.Renderer2D, project func(rowF, colF, v float32) (float32, float32),
	row, col, value, floor, stepX, stepY float32, base ui.RGBA) {

	inset := float32(0.15)
	r0, r1 := row+inset, row+1-inset
	c0, c1 := col+inset, col+1-inset
	// Corners of the top face and of the base.
	type pt struct{ x, y float32 }
	top := []pt{}
	bottom := []pt{}
	for _, rc := range [][2]float32{{r0, c0}, {r0, c1}, {r1, c1}, {r1, c0}} {
		x, y := project(rc[0], rc[1], value)
		top = append(top, pt{x, y})
		x, y = project(rc[0], rc[1], floor)
		bottom = append(bottom, pt{x, y})
	}
	quad := func(a, b, c, d pt, col ui.RGBA) {
		p := render.NewPath().MoveTo(a.x, a.y).LineTo(b.x, b.y).
			LineTo(c.x, c.y).LineTo(d.x, d.y).Close()
		r.FillPath(p, col)
	}
	shade := func(c ui.RGBA, f float32) ui.RGBA {
		return ui.RGBA{R: c.R * f, G: c.G * f, B: c.B * f, A: c.A}
	}
	// Two side faces (front-left and front-right), then the top.
	quad(top[1], top[2], bottom[2], bottom[1], shade(base, 0.7))
	quad(top[2], top[3], bottom[3], bottom[2], shade(base, 0.55))
	quad(top[0], top[1], top[2], top[3], base)
}

// containsFold reports a case-insensitive substring match, for the enum
// names the presets arrive as ("Graphs3D.CameraPreset.IsometricLeftHigh").
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
