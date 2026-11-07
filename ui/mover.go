package ui

import "github.com/chewxy/math32"

// Mover moves something by hand: attached to an element with
// DivEl.DragMove, a press-drag on the element moves the target by the
// pointer's movement — along Axis, kept inside the bounds, once the
// pointer has travelled Threshold pixels — and reports the drag as it goes
// (QML's MouseArea drag.target and DragHandler). Keep one across frames,
// like a ScrollState: the drag in progress lives in it.
//
// Pos and SetPos are the target's position, in whatever coordinates the
// caller keeps it in (an item's x/y in its parent); the drag adds the
// pointer's movement in window pixels. A nil SetPos moves nothing and
// only reports — how a page pans a map or a canvas by hand.
//
// A press that never passes the threshold moves nothing, so the element's
// OnClick still fires for it.
type Mover struct {
	Pos    func() (x, y float32)
	SetPos func(x, y float32)
	Axis   MoveAxis
	// Threshold is how far the pointer travels before the target moves.
	// NewMover sets DefaultDragThreshold; zero moves at once.
	Threshold float32
	// The target's bounds (NewMover: unbounded).
	MinX, MaxX, MinY, MaxY float32
	// OnActive runs when a drag starts moving the target and when it ends.
	OnActive func(active bool)
	// OnMove runs for every movement while active, with the change since
	// the last one (QML's onTranslationChanged delta).
	OnMove func(dx, dy float32)

	active         bool
	startX, startY float32
	tx, ty         float32 // this drag's movement
	px, py         float32 // all drags' movement, summed
}

// MoveAxis is the direction a Mover moves its target in.
type MoveAxis int

const (
	MoveXY MoveAxis = iota // both ways
	MoveX                  // horizontally only
	MoveY                  // vertically only
)

// DefaultDragThreshold is the distance a drag travels before it moves
// anything: Qt's startDragDistance, 10 logical pixels.
const DefaultDragThreshold = 10

// NewMover returns an unbounded Mover over a target's position with the
// default threshold.
func NewMover(pos func() (x, y float32), setPos func(x, y float32)) *Mover {
	inf := math32.Inf(1)
	return &Mover{Pos: pos, SetPos: setPos, Threshold: DefaultDragThreshold,
		MinX: -inf, MaxX: inf, MinY: -inf, MaxY: inf}
}

// Active reports whether a drag is moving the target.
func (m *Mover) Active() bool { return m.active }

// Translation is the current drag's movement (zero between drags).
func (m *Mover) Translation() (x, y float32) { return m.tx, m.ty }

// PersistentTranslation is every drag's movement added up.
func (m *Mover) PersistentTranslation() (x, y float32) { return m.px, m.py }

// Drag is the element's drag handler (DivEl.OnDrag's signature): dx, dy
// is the movement since the press.
func (m *Mover) Drag(dx, dy float32, phase DragPhase) {
	switch phase {
	case DragStart:
		if m.Pos != nil {
			m.startX, m.startY = m.Pos()
		}
		m.tx, m.ty = 0, 0
		return
	case DragEnd:
		if m.active {
			m.active = false
			if m.OnActive != nil {
				m.OnActive(false)
			}
		}
		return
	}
	switch m.Axis {
	case MoveX:
		dy = 0
	case MoveY:
		dx = 0
	}
	if !m.active {
		if dx*dx+dy*dy <= m.Threshold*m.Threshold {
			return
		}
		m.active = true
		if m.OnActive != nil {
			m.OnActive(true)
		}
	}
	if m.SetPos != nil {
		m.SetPos(clamp32(m.startX+dx, m.MinX, m.MaxX), clamp32(m.startY+dy, m.MinY, m.MaxY))
	}
	ddx, ddy := dx-m.tx, dy-m.ty
	if ddx == 0 && ddy == 0 {
		return
	}
	m.tx, m.ty = dx, dy
	m.px, m.py = m.px+ddx, m.py+ddy
	if m.OnMove != nil {
		m.OnMove(ddx, ddy)
	}
}

// clamp32 keeps v in [lo, hi]; lo wins when they cross (the maximum is
// applied first).
func clamp32(v, lo, hi float32) float32 {
	if v > hi {
		v = hi
	}
	if v < lo {
		v = lo
	}
	return v
}

// DragMove makes the element drag m's target (see Mover).
func (d *DivEl) DragMove(m *Mover) *DivEl { return d.OnDrag(m.Drag) }
