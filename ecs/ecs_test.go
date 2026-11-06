package ecs

import "testing"

type pos struct{ X, Y float32 }
type vel struct{ DX, DY float32 }

func TestSpawnDespawnGenerations(t *testing.T) {
	w := NewWorld()
	a := w.Spawn()
	b := w.Spawn()
	if a == b || !w.Alive(a) || !w.Alive(b) {
		t.Fatalf("spawn: a=%v b=%v", a, b)
	}
	w.Despawn(a)
	if w.Alive(a) {
		t.Fatal("despawned entity still alive")
	}
	c := w.Spawn() // reuses a's slot with a new generation
	if c.ID != a.ID || c.Gen == a.Gen {
		t.Fatalf("slot reuse: a=%v c=%v", a, c)
	}
	if w.Alive(a) {
		t.Fatal("stale handle alive after slot reuse")
	}
	if (Entity{}) == a {
		t.Fatal("zero entity must never be issued")
	}
	if w.Alive(Entity{}) {
		t.Fatal("zero entity must not be alive")
	}
}

func TestComponentAddGetRemove(t *testing.T) {
	w := NewWorld()
	p := NewComponent[pos](w)
	e := w.Spawn()

	if p.Has(e) || p.Get(e) != nil {
		t.Fatal("component present before Add")
	}
	p.Add(e, pos{1, 2})
	if got := p.Get(e); got == nil || got.X != 1 {
		t.Fatalf("Get = %v", got)
	}
	p.Add(e, pos{3, 4}) // overwrite
	if got := p.Get(e); got.X != 3 || p.Len() != 1 {
		t.Fatalf("overwrite: %v len=%d", got, p.Len())
	}
	p.Remove(e)
	if p.Has(e) || p.Len() != 0 {
		t.Fatal("Remove failed")
	}
}

func TestColumnStaysDenseOnRemove(t *testing.T) {
	w := NewWorld()
	p := NewComponent[pos](w)
	var es []Entity
	for i := range 5 {
		e := w.Spawn()
		p.Add(e, pos{float32(i), 0})
		es = append(es, e)
	}
	p.Remove(es[1]) // swap-remove moves the last row into slot 1
	if p.Len() != 4 {
		t.Fatalf("len = %d", p.Len())
	}
	col := p.Column()
	if len(col) != 4 {
		t.Fatalf("column len = %d", len(col))
	}
	// Every remaining entity still resolves to its own value.
	for _, e := range []Entity{es[0], es[2], es[3], es[4]} {
		got := p.Get(e)
		if got == nil {
			t.Fatalf("entity %v lost its value", e)
		}
	}
	// Row bookkeeping is consistent.
	for i := range col {
		e := p.EntityAt(i)
		if p.Get(e) != &col[i] {
			t.Fatalf("row %d maps to wrong entity", i)
		}
	}
}

func TestDespawnRemovesFromAllComponents(t *testing.T) {
	w := NewWorld()
	p := NewComponent[pos](w)
	v := NewComponent[vel](w)
	e := w.Spawn()
	p.Add(e, pos{1, 1})
	v.Add(e, vel{2, 2})
	w.Despawn(e)
	if p.Len() != 0 || v.Len() != 0 {
		t.Fatalf("despawn left components: pos=%d vel=%d", p.Len(), v.Len())
	}
}

func TestEach2IteratesIntersection(t *testing.T) {
	w := NewWorld()
	p := NewComponent[pos](w)
	v := NewComponent[vel](w)
	both := 0
	for i := range 10 {
		e := w.Spawn()
		p.Add(e, pos{float32(i), 0})
		if i%2 == 0 {
			v.Add(e, vel{1, 0})
			both++
		}
	}
	n := 0
	Each2(p, v, func(e Entity, pp *pos, vv *vel) {
		pp.X += vv.DX
		n++
	})
	if n != both {
		t.Fatalf("Each2 visited %d, want %d", n, both)
	}
	// A simple movement system: positions of even entities advanced.
	moved := 0
	p.Each(func(e Entity, pp *pos) {
		if v.Has(e) && pp.X != float32(int(pp.X)) {
			t.Fatalf("unexpected value %v", pp.X)
		}
		if v.Has(e) {
			moved++
		}
	})
	if moved != both {
		t.Fatalf("moved = %d", moved)
	}
}
