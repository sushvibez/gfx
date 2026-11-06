package ui

import "testing"

// A grid's tracks take their preferred size from the cells in them and
// share out what is left in PROPORTION to those sizes — measured against
// Qt's own grid engine, which the markup this serves is written for.
func TestGridTracksShareSurplusProportionally(t *testing.T) {
	g := Grid(2, 0).GapXY(20, 20)
	g.Cell(Div().Size(100, 80).Grow(), 1, 1)
	g.Cell(Div().Size(700, 80).Grow(), 1, 1)
	g.Cell(Div().H(60).Grow(), 1, 1)
	g.Cell(Div().Size(400, 60).Grow(), 1, 1)
	g.layout(&Context{}, Rect{20, 20, 860, 260})
	// Column prefs are 100 and 700; 840 of usable width leaves 40 to share
	// out, which lands as +5 / +35.
	if got := g.placed[0].W; !closeTo(got, 105) {
		t.Errorf("narrow column = %v, want 105", got)
	}
	if got := g.placed[1].W; !closeTo(got, 735) {
		t.Errorf("wide column = %v, want 735", got)
	}
	// Equal prefs (both 0 here after the second row's fills) share evenly.
	eq := Grid(2, 0).GapXY(20, 20)
	eq.Cell(Div().H(40).Grow(), 1, 1)
	eq.Cell(Div().H(40).Grow(), 1, 1)
	eq.layout(&Context{}, Rect{20, 20, 860, 260})
	if a, b := eq.placed[0].W, eq.placed[1].W; !closeTo(a, 420) || !closeTo(b, 420) {
		t.Errorf("columns with no preference = %v/%v, want 420/420", a, b)
	}
}

// A spanning cell only widens the tracks it covers if they cannot hold it
// ALREADY — the cells that sit in one track each are measured first, or a
// row-spanning cell inflates a track its neighbours were about to fill.
func TestGridSpanDoesNotInflateSharedTracks(t *testing.T) {
	g := Grid(3, 0).GapXY(10, 10)
	g.Cell(Div().H(60).Grow(), 2, 1) // spans two columns
	g.Cell(Div().H(60).Grow(), 1, 1)
	g.Cell(Div().H(120).Grow(), 1, 2) // spans two rows: 55+10+55 == 120
	g.Cell(Div().H(55).Grow(), 1, 1)
	g.Cell(Div().H(55).Grow(), 1, 1)
	g.Cell(Div().H(55).Grow(), 1, 1)
	g.Cell(Div().H(55).Grow(), 1, 1)
	g.layout(&Context{}, Rect{20, 20, 860, 260})
	if g.rows != 3 {
		t.Fatalf("rows = %d, want 3 (the grid grows them for the items)", g.rows)
	}
	want := []float32{60, 55, 55}
	for i, w := range want {
		if !closeTo(g.rowPref[i], w) {
			t.Errorf("rowPref = %v, want %v", g.rowPref, want)
			break
		}
		_ = i
	}
}

// A cell that did not ask to fill keeps its size in its track: left, and
// vertically centred, as Qt aligns it.
func TestGridCellWithoutFillKeepsItsSize(t *testing.T) {
	g := Grid(2, 0).GapXY(20, 20)
	g.Cell(Div().H(40).Grow(), 1, 1) // sets the columns
	g.Cell(Div().H(40).Grow(), 1, 1)
	g.Cell(Div().Size(100, 40), 1, 1) // no fill
	g.Cell(Div().Size(100, 20), 1, 1)
	g.layout(&Context{}, Rect{20, 20, 860, 260})
	c := g.placed[2]
	if !closeTo(c.W, 100) || !closeTo(c.X, 20) {
		t.Errorf("unfilled cell = %v, want 100 wide at x=20", c)
	}
	// Its row is 120 tall (240 shared between two rows of 40), so a 40-tall
	// item sits 40 below the row's top.
	if !closeTo(c.H, 40) || !closeTo(c.Y, 20+120+20+40) {
		t.Errorf("unfilled cell y/h = %v/%v, want %v/40", c.Y, c.H, 20+120+20+40)
	}
}

// Positioner mode (QML's plain Grid) never shares out space: tracks stay
// at their preferred size.
func TestGridPositionedKeepsPreferredTracks(t *testing.T) {
	g := Grid(2, 0).GapXY(10, 10).Positioned()
	g.Cell(Div().Size(100, 40), 1, 1)
	g.Cell(Div().Size(200, 40), 1, 1)
	g.layout(&Context{}, Rect{0, 0, 800, 200})
	if got := g.placed[1].X; !closeTo(got, 110) {
		t.Errorf("second cell at x=%v, want 110 (tracks at their own size)", got)
	}
}

func closeTo(a, b float32) bool { return a-b < 0.6 && b-a < 0.6 }

// A grid NESTED in a Qt-style layout is only given as much as it can use:
// an axis nothing inside can grow along caps at the grid's own content.
// MEASURED against Qt 6.10 — two 280px cards in a `Layout.fillWidth`
// 2-column grid sit at 20 and 324, so the grid is 584 wide, not the row's
// full width. Without the cap our columns took the whole row and spread
// their surplus, pushing the second card 116px right (the 2.5D-cards page).
func TestGridCapsAtItsContentWhenNothingFills(t *testing.T) {
	g := Grid(2, 0).GapXY(24, 24)
	g.Cell(Div().Size(280, 80), 1, 1)
	g.Cell(Div().Size(280, 80), 1, 1)
	ctx := &Context{}
	g.measure(ctx)
	w, _ := g.layoutMax()
	if w != 584 {
		t.Errorf("grid caps at %v wide, want 584 (280 + 24 + 280)", w)
	}
	// ...and with a filling cell there is no ceiling on that axis.
	g2 := Grid(2, 0).GapXY(24, 24)
	g2.Cell(Div().Size(280, 80).Grow(), 1, 1)
	g2.Cell(Div().Size(280, 80), 1, 1)
	g2.measure(ctx)
	if w, _ := g2.layoutMax(); w != 0 {
		t.Errorf("a grid with a filling cell caps at %v, want no limit", w)
	}
	// The cap reaches the child through a column's stretch, so the cards
	// keep their width instead of spreading.
	col := Div().Col().SpreadCells().Children(g)
	col.layout(ctx, Rect{0, 0, 900, 200})
	if b := Bounds(g); b.W != 584 {
		t.Errorf("grid laid out %v wide in a 900px column, want 584", b.W)
	}
}

// A grid track never shrinks below its content's minimum: Qt keeps a 90px
// item 90px wide in a column too narrow for it and lets the layout
// OVERFLOW (measured against Qt 6.10 — two 90px cells in a 156px grid sit
// at 0 and 108, running past the edge). Ours used to scale every track
// down proportionally, squeezing the items to 79.
func TestGridTracksDoNotShrinkBelowTheirContent(t *testing.T) {
	g := Grid(2, 1).GapXY(18, 14).Children(
		Div().Size(90, 40),
		Div().Size(90, 40),
	)
	ctx := &Context{}
	g.layout(ctx, Rect{0, 0, 156, 200})
	kids := Children(g)
	if len(kids) != 2 {
		t.Fatalf("built %d children", len(kids))
	}
	for i, want := range []float32{0, 108} {
		if got := Bounds(kids[i]).X; got != want {
			t.Errorf("cell %d at x %v, want %v", i, got, want)
		}
		if got := Bounds(kids[i]).W; got != 90 {
			t.Errorf("cell %d is %v wide, want its own 90", i, got)
		}
	}
	// With room to spare the tracks still share the surplus.
	wide := Grid(2, 1).GapXY(18, 14).Children(
		Div().Size(90, 40).Grow(),
		Div().Size(90, 40).Grow(),
	)
	wide.layout(&Context{}, Rect{0, 0, 418, 200})
	if got := Bounds(Children(wide)[1]).X; got != 218 {
		t.Errorf("with surplus, cell 1 at x %v, want 218", got)
	}
}

// P pads the grid: the natural size grows by twice the padding and the
// cells are laid out inside it.
func TestGridPadding(t *testing.T) {
	g := Grid(2, 0).Gap(10).P(8)
	g.Cell(Div().Size(50, 20), 1, 1)
	g.Cell(Div().Size(70, 20), 1, 1)
	w, h := g.measure(&Context{})
	if w != 50+10+70+16 || h != 20+16 {
		t.Errorf("padded natural size = %vx%v, want %vx%v", w, h, 50+10+70+16, 20+16)
	}
	g.layout(&Context{}, Rect{100, 100, w, h})
	if p := g.placed[0]; p.X != 108 || p.Y != 108 {
		t.Errorf("first cell at %v,%v, want 108,108", p.X, p.Y)
	}
	if p := g.placed[1]; p.X != 108+50+10 || p.X+p.W != 100+w-8 {
		t.Errorf("second cell spans %v..%v, want %v..%v", p.X, p.X+p.W, 168, 100+w-8)
	}
}
