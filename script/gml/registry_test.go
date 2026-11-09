package gml

import "testing"

// Items written on one line get keys of their own: the registry, the
// subtree cache and per-item state are all keyed by them, and the corpus'
// one-line legends (`Row { Rectangle {} Label {} }`) shared one.
func TestSameLineItemsHaveDistinctKeys(t *testing.T) {
	root, err := Parse("Item {\n    Row { Rectangle { width: 8 } Text { text: \"A\" } }\n    Rectangle { }\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	row := root.children[0]
	keys := map[string]string{}
	for _, n := range []*node{row, row.children[0], row.children[1], root.children[1]} {
		k := elemKey(n)
		if other, ok := keys[k]; ok {
			t.Fatalf("%s and %s share the key %q", other, n.typeName, k)
		}
		keys[k] = n.typeName
	}
	if k := elemKey(row); k != "__el2" {
		t.Errorf("the first item on a line keeps the plain line key: %q", k)
	}
}
