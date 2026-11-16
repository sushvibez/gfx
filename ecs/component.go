package ecs

// Component is one component type's storage: a dense, contiguous column
// of T (the slice the GPU binds directly — DESIGN.md §4) plus the
// entity↔row indices around it. Create with NewComponent; values are
// added per entity and iterated in column order.
type Component[T any] struct {
	world    *World
	dense    []T      // the column: contiguous, GPU-bindable
	entities []Entity // row → entity
	sparse   []int32  // entity id → row, -1 when absent

	// One merged dirty row range per GPU mirror (GPUColumn), each consumed
	// by that mirror's own Sync: a single range shared by every mirror was
	// cleared by whichever synced first, and the others never uploaded the
	// change. A component nobody mirrors tracks nothing.
	mirrors []*rowSpan
}

// rowSpan is one merged range of dirty rows; lo < 0 means clean.
type rowSpan struct{ lo, hi int }

func (s *rowSpan) add(i int) {
	if s.lo < 0 {
		s.lo, s.hi = i, i
		return
	}
	s.lo = min(s.lo, i)
	s.hi = max(s.hi, i)
}

// attach registers a mirror's dirty range; detach drops it. A mirror that
// is never detached goes on being widened (and is kept alive) for as long
// as the component lives.
func (c *Component[T]) attach(s *rowSpan) { c.mirrors = append(c.mirrors, s) }

func (c *Component[T]) detach(s *rowSpan) {
	for i, m := range c.mirrors {
		if m == s {
			last := len(c.mirrors) - 1
			c.mirrors[i] = c.mirrors[last]
			c.mirrors[last] = nil
			c.mirrors = c.mirrors[:last]
			return
		}
	}
}

// NewComponent registers a component type with the world.
func NewComponent[T any](w *World) *Component[T] {
	c := &Component[T]{world: w}
	w.columns = append(w.columns, c)
	return c
}

// markRow widens every mirror's dirty range to include row i.
func (c *Component[T]) markRow(i int) {
	for _, s := range c.mirrors {
		s.add(i)
	}
}

func (c *Component[T]) row(e Entity) int32 {
	if !c.world.Alive(e) || int(e.ID) >= len(c.sparse) {
		return -1
	}
	return c.sparse[e.ID]
}

// Add sets e's component value, creating or overwriting.
func (c *Component[T]) Add(e Entity, v T) {
	if !c.world.Alive(e) {
		return
	}
	for int(e.ID) >= len(c.sparse) {
		c.sparse = append(c.sparse, -1)
	}
	if r := c.sparse[e.ID]; r >= 0 {
		c.dense[r] = v
		c.markRow(int(r))
		return
	}
	c.sparse[e.ID] = int32(len(c.dense))
	c.dense = append(c.dense, v)
	c.entities = append(c.entities, e)
	c.markRow(len(c.dense) - 1)
}

// Get returns a pointer to e's value, or nil. The pointer is valid until
// the next Add/Remove on this component or Despawn of an entity that has
// it (rows move).
func (c *Component[T]) Get(e Entity) *T {
	r := c.row(e)
	if r < 0 {
		return nil
	}
	return &c.dense[r]
}

// Has reports whether e has this component.
func (c *Component[T]) Has(e Entity) bool { return c.row(e) >= 0 }

// Remove detaches the component from e (swap-remove keeps the column
// dense).
func (c *Component[T]) Remove(e Entity) {
	r := c.row(e)
	if r < 0 {
		return
	}
	c.removeRow(r, e.ID)
}

func (c *Component[T]) removeRow(r int32, id uint32) {
	last := int32(len(c.dense) - 1)
	if r != last {
		c.dense[r] = c.dense[last]
		moved := c.entities[last]
		c.entities[r] = moved
		c.sparse[moved.ID] = r
	}
	var zero T
	c.dense[last] = zero
	c.dense = c.dense[:last]
	c.entities = c.entities[:last]
	c.sparse[id] = -1
	if r != last {
		c.markRow(int(r)) // the swapped-in row changed
	}
	// Clamp the ranges to the shrunken column.
	for _, s := range c.mirrors {
		if s.lo >= len(c.dense) {
			s.lo = -1
		} else if s.hi >= len(c.dense) {
			s.hi = len(c.dense) - 1
		}
	}
}

func (c *Component[T]) removeIfPresent(e Entity) {
	// Called during despawn, when the entity is still technically alive.
	if int(e.ID) < len(c.sparse) {
		if r := c.sparse[e.ID]; r >= 0 {
			c.removeRow(r, e.ID)
		}
	}
}

// Len reports how many entities have this component.
func (c *Component[T]) Len() int { return len(c.dense) }

// Column exposes the dense column. Mutating values through it is the
// fast path for systems; call MarkDirty afterwards if the column is
// GPU-shared. The slice is the column as of this call: an Add, Remove or
// Despawn afterwards can move or shorten the column, so take it again
// rather than keeping it across them.
func (c *Component[T]) Column() []T { return c.dense }

// EntityAt reports which entity owns row i of the column.
func (c *Component[T]) EntityAt(i int) Entity { return c.entities[i] }

// MarkDirty flags the whole column for GPU re-upload.
func (c *Component[T]) MarkDirty() {
	if len(c.dense) > 0 {
		for _, s := range c.mirrors {
			s.lo, s.hi = 0, len(c.dense)-1
		}
	}
}

// MarkDirtyRow flags one row (mutated via Column or Get) for GPU
// re-upload.
func (c *Component[T]) MarkDirtyRow(i int) {
	if i >= 0 && i < len(c.dense) {
		c.markRow(i)
	}
}

// Each calls fn for every (entity, value) pair, in column order.
//
// fn may change the world while Each runs, with these rules (Each2 follows
// them for both of its components):
//
//   - Writing through the *T is the point of Each. It does not mark the
//     row dirty: call MarkDirtyRow or MarkDirty if the column is mirrored
//     to the GPU.
//   - Adding this component to an entity that lacks it appends a row. The
//     new row is not visited by this Each, and the *T passed to fn is no
//     longer valid after the Add (the column may have moved).
//   - Removing this component, or despawning an entity that has it, moves
//     the last row into the freed one. If the freed row is the one being
//     visited or an earlier one, the row moved into it is not visited by
//     this Each; a row freed further on costs nothing. The *T passed to fn
//     is invalid after a removal. To visit every row and remove some,
//     collect the entities and remove them after Each returns.
//   - Other components may be added and removed freely.
//
// None of this is safe from another goroutine: a world and its components
// belong to one goroutine at a time.
func (c *Component[T]) Each(fn func(Entity, *T)) {
	// The row count is taken once so rows added by fn wait for the next
	// pass, and checked again each step so rows removed by fn end the walk
	// instead of running off the column.
	for i, n := 0, len(c.dense); i < n && i < len(c.dense); i++ {
		fn(c.entities[i], &c.dense[i])
	}
}

// Each2 iterates entities that have both components, passing pointers to
// both values. It walks the smaller column, so the order is that column's
// and Each's rules about adding and removing inside fn apply to both.
func Each2[A, B any](ca *Component[A], cb *Component[B], fn func(Entity, *A, *B)) {
	if len(cb.dense) < len(ca.dense) {
		for i, n := 0, len(cb.dense); i < n && i < len(cb.dense); i++ {
			e := cb.entities[i]
			if a := ca.Get(e); a != nil {
				fn(e, a, &cb.dense[i])
			}
		}
		return
	}
	for i, n := 0, len(ca.dense); i < n && i < len(ca.dense); i++ {
		e := ca.entities[i]
		if b := cb.Get(e); b != nil {
			fn(e, &ca.dense[i], b)
		}
	}
}
