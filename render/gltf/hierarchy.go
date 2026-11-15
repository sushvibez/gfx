package gltf

import "fmt"

// MaxNodeDepth is the deepest node hierarchy a document may hold. Walks of
// the hierarchy recurse, so its depth must not be the file's to choose.
const MaxNodeDepth = 1024

// CheckHierarchy reports whether the node hierarchy is one a consumer can
// walk: every child and root index names a node, no node has two parents
// (glTF requires a strict tree, and a shared subtree can be made to unfold
// exponentially), nothing is its own ancestor, and no chain is deeper than
// MaxNodeDepth. Parse calls it; so should anything that walks a Document it
// did not get from Parse (render.BuildModel does).
func (d *Document) CheckHierarchy() error {
	n := len(d.Nodes)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = -1
	}
	for i := range d.Nodes {
		for _, c := range d.Nodes[i].Children {
			if c < 0 || c >= n {
				return fmt.Errorf("gltf: node %d has child %d, out of range (%d nodes)", i, c, n)
			}
			if parent[c] >= 0 {
				return fmt.Errorf("gltf: node %d is a child of both node %d and node %d", c, parent[c], i)
			}
			parent[c] = i
		}
	}
	for _, r := range d.Roots {
		if r < 0 || r >= n {
			return fmt.Errorf("gltf: scene root %d out of range (%d nodes)", r, n)
		}
	}
	// With one parent each, a node is in a cycle exactly when climbing from
	// it never reaches a node without a parent. depth memoizes the climb
	// (0 unknown), so the whole check is linear.
	depth := make([]int, n)
	var chain []int
	for i := range d.Nodes {
		chain = chain[:0]
		at := i
		for at >= 0 && depth[at] == 0 {
			if len(chain) > n {
				return fmt.Errorf("gltf: node %d is its own ancestor", i)
			}
			chain = append(chain, at)
			at = parent[at]
		}
		base := 0
		if at >= 0 {
			base = depth[at]
		}
		for k := len(chain) - 1; k >= 0; k-- {
			base++
			depth[chain[k]] = base
		}
		if base > MaxNodeDepth {
			return fmt.Errorf("gltf: node hierarchy deeper than %d", MaxNodeDepth)
		}
	}
	return nil
}
