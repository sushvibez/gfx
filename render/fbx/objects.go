package fbx

import "strings"

// This layer normalizes the two FBX generations we see (6100 name-keyed,
// 7700 id-keyed) into a common object graph so extraction is version-blind:
//   - obj.id is the connection key (int64 in 7700; a hash of the full name
//     string in 6100, which keys connections by name).
//   - obj.name is the clean object name ("mixamorig:Hips"), prefix-stripped
//     via BaseName for cross-file retargeting.
//   - conns maps child id -> parent id (the OO object-object links).

type object struct {
	id    int64
	name  string // clean (before the \x00\x01 class marker)
	class string // "LimbNode", "Mesh", "" ...
	node  *Node  // the underlying record
}

type graph struct {
	objs    map[int64]*object
	byNode  map[*Node]*object
	parent  map[int64][]int64 // parent id -> child ids (OO)
	childOf map[int64]int64   // child id -> parent id (first parent)
	// Property-object connections (OP): child id -> [(parent id, prop)].
	propConn map[int64][]propLink
	// Reverse OP index: parent id -> [(child id, prop)] (curve node -> curves).
	opChildren map[int64][]propLink
}

type propLink struct {
	parent int64 // the OTHER endpoint (parent for propConn, child for opChildren)
	prop   string
}

// nameID hashes a 6100 connection name string to a stable int64 id (FNV-1a).
func nameID(s string) int64 {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return int64(h)
}

// splitName splits FBX's "name\x00\x01class" into (name, class).
func splitName(s string) (name, class string) {
	if i := strings.Index(s, "\x00\x01"); i >= 0 {
		return s[:i], s[i+2:]
	}
	return s, ""
}

// BaseName strips a Mixamo namespace prefix ("mixamorig:" / "mixamorig6:")
// so a clip authored on one rig retargets onto another by bone name.
func BaseName(n string) string {
	if i := strings.LastIndex(n, ":"); i >= 0 {
		return n[i+1:]
	}
	return n
}

// buildGraph indexes Objects + Connections for either FBX generation.
func (d *Doc) buildGraph() *graph {
	g := &graph{
		objs:       map[int64]*object{},
		byNode:     map[*Node]*object{},
		parent:     map[int64][]int64{},
		childOf:    map[int64]int64{},
		propConn:   map[int64][]propLink{},
		opChildren: map[int64][]propLink{},
	}
	objs := d.find("Objects")
	if objs == nil {
		return g
	}
	wide := d.Version >= 7500
	for _, n := range objs.Children {
		o := &object{node: n}
		if wide {
			// [L id, S "name\x00\x01class", S class]
			if len(n.Props) >= 3 {
				o.id = n.Props[0].I
				o.name, _ = splitName(n.Props[1].S)
				o.class = n.Props[2].S
			}
		} else {
			// 6100: [S "name\x00\x01class", S class]; id = hash(prop0 string).
			if len(n.Props) >= 2 {
				o.id = nameID(n.Props[0].S)
				o.name, _ = splitName(n.Props[0].S)
				o.class = n.Props[1].S
			}
		}
		g.objs[o.id] = o
		g.byNode[n] = o
	}
	conns := d.find("Connections")
	if conns != nil {
		for _, c := range conns.Children {
			if len(c.Props) < 3 || c.Props[0].Kind != 'S' {
				continue
			}
			ctype := c.Props[0].S
			cid, pid := connID(c.Props[1], wide), connID(c.Props[2], wide)
			switch ctype {
			case "OO":
				g.parent[pid] = append(g.parent[pid], cid)
				if _, ok := g.childOf[cid]; !ok {
					g.childOf[cid] = pid
				}
			case "OP":
				prop := ""
				if len(c.Props) >= 4 && c.Props[3].Kind == 'S' {
					prop = c.Props[3].S
				}
				g.propConn[cid] = append(g.propConn[cid], propLink{parent: pid, prop: prop})
				g.opChildren[pid] = append(g.opChildren[pid], propLink{parent: cid, prop: prop})
			}
		}
	}
	return g
}

// connID reads a connection endpoint id in either keying scheme.
func connID(p Prop, wide bool) int64 {
	if wide {
		return p.I
	}
	return nameID(p.S)
}

func (d *Doc) find(name string) *Node {
	for _, c := range d.Root.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// child returns a node's first child with the given name.
func child(n *Node, name string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// arr returns a node's numeric data. FBX 7x00 stores it as a single array
// property; FBX 6100 stores it as a run of individual scalar props (D/F/I/L)
// — so fall back to collecting every scalar numeric property in order.
func arr(n *Node) []float64 {
	if n == nil {
		return nil
	}
	for _, p := range n.Props {
		if p.Arr != nil {
			return p.Arr
		}
	}
	var out []float64
	for _, p := range n.Props {
		switch p.Kind {
		case 'D', 'F':
			out = append(out, p.F)
		case 'I', 'L', 'Y', 'C':
			out = append(out, float64(p.I))
		}
	}
	return out
}

// prop70 finds a typed property value under Properties70/Properties60 by
// name (e.g. "Lcl Translation"), returning up to three float components.
func prop70(model *Node) map[string][3]float64 {
	out := map[string][3]float64{}
	pn := child(model, "Properties70")
	if pn == nil {
		pn = child(model, "Properties60")
	}
	if pn == nil {
		return out
	}
	for _, p := range pn.Children { // "P" in 7x00, "Property" in 6100
		if p.Name != "P" && p.Name != "Property" && p.Name != "PropertyTemplate" {
			continue
		}
		if len(p.Props) == 0 || p.Props[0].Kind != 'S' {
			continue
		}
		key := p.Props[0].S
		var v [3]float64
		n := 0
		for _, pp := range p.Props[1:] {
			if pp.Kind == 'D' || pp.Kind == 'F' {
				if n < 3 {
					v[n] = pp.F
					n++
				}
			} else if pp.Kind == 'I' || pp.Kind == 'L' {
				if n < 3 {
					v[n] = float64(pp.I)
					n++
				}
			}
		}
		if n > 0 {
			out[key] = v
		}
	}
	return out
}
