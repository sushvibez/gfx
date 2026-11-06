package ecs

// World owns entities. Component storage lives in Component[T] values
// created against a world; despawning an entity removes it from every
// component that holds it.
//
// Storage note vs DESIGN.md §4: v0 uses sparse sets — one contiguous
// column per component type plus an entity→row index. That already gives
// the property the GPU contract needs (each component is one dense slice
// bindable as a storage buffer); archetype grouping is a cache-locality
// optimization for multi-component iteration that can replace the
// internals later without changing this API.
type World struct {
	gens    []uint32 // generation per entity slot
	alive   []bool
	free    []uint32
	columns []columnAny
}

// Entity is a handle: 32-bit slot index + generation, never a pointer
// (DESIGN.md §4.1). The zero Entity is never valid.
type Entity struct {
	ID  uint32
	Gen uint32
}

// columnAny lets the world detach despawned entities from every component
// without knowing their types.
type columnAny interface {
	removeIfPresent(e Entity)
}

func NewWorld() *World {
	// Slot 0 is reserved so the zero Entity stays invalid.
	return &World{gens: []uint32{0}, alive: []bool{false}}
}

// Spawn creates a new entity.
func (w *World) Spawn() Entity {
	if n := len(w.free); n > 0 {
		id := w.free[n-1]
		w.free = w.free[:n-1]
		w.alive[id] = true
		return Entity{ID: id, Gen: w.gens[id]}
	}
	id := uint32(len(w.gens))
	w.gens = append(w.gens, 1)
	w.alive = append(w.alive, true)
	return Entity{ID: id, Gen: 1}
}

// Despawn destroys an entity, removing all of its components. Stale
// handles to the entity become invalid (the generation bumps).
func (w *World) Despawn(e Entity) {
	if !w.Alive(e) {
		return
	}
	for _, c := range w.columns {
		c.removeIfPresent(e)
	}
	w.gens[e.ID]++
	w.alive[e.ID] = false
	w.free = append(w.free, e.ID)
}

// Alive reports whether the handle refers to a live entity.
func (w *World) Alive(e Entity) bool {
	return e.ID > 0 && int(e.ID) < len(w.gens) && w.alive[e.ID] && w.gens[e.ID] == e.Gen
}
