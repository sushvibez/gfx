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

	// One merged dirty row range, consumed by GPU sync. -1 lo = clean.
	dirtyLo, dirtyHi int
}

// NewComponent registers a component type with the world.
func NewComponent[T any](w *World) *Component[T] {
	c := &Component[T]{world: w, dirtyLo: -1}
	w.columns = append(w.columns, c)
	return c
}

// markRow widens the dirty range to include row i.
func (c *Component[T]) markRow(i int) {
	if c.dirtyLo < 0 {
		c.dirtyLo, c.dirtyHi = i, i
		return
	}
	c.dirtyLo = min(c.dirtyLo, i)
	c.dirtyHi = max(c.dirtyHi, i)
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
// the next Add/Remove on this component (rows move).
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
	// Clamp the range to the shrunken column.
	if c.dirtyLo >= len(c.dense) {
		c.dirtyLo = -1
	} else if c.dirtyHi >= len(c.dense) {
		c.dirtyHi = len(c.dense) - 1
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
// GPU-shared.
func (c *Component[T]) Column() []T { return c.dense }

// EntityAt reports which entity owns row i of the column.
func (c *Component[T]) EntityAt(i int) Entity { return c.entities[i] }

// MarkDirty flags the whole column for GPU re-upload.
func (c *Component[T]) MarkDirty() {
	if len(c.dense) > 0 {
		c.dirtyLo, c.dirtyHi = 0, len(c.dense)-1
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
func (c *Component[T]) Each(fn func(Entity, *T)) {
	for i := range c.dense {
		fn(c.entities[i], &c.dense[i])
	}
}

// Each2 iterates entities that have both components, passing pointers to
// both values. It walks the smaller column.
func Each2[A, B any](ca *Component[A], cb *Component[B], fn func(Entity, *A, *B)) {
	if len(cb.dense) < len(ca.dense) {
		for i := range cb.dense {
			e := cb.entities[i]
			if a := ca.Get(e); a != nil {
				fn(e, a, &cb.dense[i])
			}
		}
		return
	}
	for i := range ca.dense {
		e := ca.entities[i]
		if b := cb.Get(e); b != nil {
			fn(e, &ca.dense[i], b)
		}
	}
}
