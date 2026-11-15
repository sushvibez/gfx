package gltf

import (
	"fmt"
	"strings"
	"testing"
)

// A scene index or a node hierarchy that cannot be walked is an error from
// Parse, promptly: never a panic, and never a walk without end.
func TestParseRejectsBadHierarchy(t *testing.T) {
	deep := make([]string, MaxNodeDepth+2)
	for i := range deep {
		deep[i] = fmt.Sprintf(`{"children":[%d]}`, i+1)
	}
	deep[len(deep)-1] = `{}`
	cases := []struct{ name, json, want string }{
		{"negative scene", `{"scene":-1,"scenes":[]}`, "scene -1 out of range"},
		{"scene past the end", `{"scene":1,"scenes":[{"nodes":[]}]}`, "scene 1 out of range"},
		{"scene with no scenes", `{"scene":0}`, "scene 0 out of range"},
		{"self child", `{"nodes":[{"children":[0]}],"scenes":[{"nodes":[0]}],"scene":0}`, "own ancestor"},
		{"two-node cycle", `{"nodes":[{"children":[1]},{"children":[0]}],"scenes":[{"nodes":[0]}]}`, "own ancestor"},
		{"cycle off the scene", `{"nodes":[{},{"children":[2]},{"children":[1]}],"scenes":[{"nodes":[0]}]}`, "own ancestor"},
		{"child out of range", `{"nodes":[{"children":[5]}],"scenes":[{"nodes":[0]}]}`, "child 5, out of range"},
		{"negative child", `{"nodes":[{"children":[-1]}],"scenes":[{"nodes":[0]}]}`, "child -1, out of range"},
		{"root out of range", `{"nodes":[{}],"scenes":[{"nodes":[3]}]}`, "root 3 out of range"},
		{"two parents", `{"nodes":[{"children":[2]},{"children":[2]},{}],"scenes":[{"nodes":[0,1]}]}`, "child of both"},
		{"same child twice", `{"nodes":[{"children":[1,1]},{}],"scenes":[{"nodes":[0]}]}`, "child of both"},
		{"too deep", `{"nodes":[` + strings.Join(deep, ",") + `],"scenes":[{"nodes":[0]}]}`, "deeper than"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.json), nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error with %q", c.name, err, c.want)
		}
	}
}

// What glTF allows stays allowed: no scene named, no scenes at all, and a
// chain as deep as the limit.
func TestParseAcceptsPlainHierarchies(t *testing.T) {
	chain := make([]string, MaxNodeDepth)
	for i := range chain {
		chain[i] = fmt.Sprintf(`{"children":[%d]}`, i+1)
	}
	chain[len(chain)-1] = `{}`
	for name, json := range map[string]string{
		"no scenes":      `{"nodes":[{}]}`,
		"default scene":  `{"nodes":[{"children":[1]},{}],"scenes":[{"nodes":[0]}]}`,
		"deep but legal": `{"nodes":[` + strings.Join(chain, ",") + `],"scenes":[{"nodes":[0]}]}`,
	} {
		if _, err := Parse([]byte(json), nil); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func FuzzCheckHierarchy(f *testing.F) {
	f.Add([]byte{0, 0})
	f.Add([]byte{1, 0, 0, 1})
	f.Fuzz(func(t *testing.T, b []byte) {
		// Pairs of (parent, child) over 16 nodes; the last byte a root.
		d := &Document{Nodes: make([]Node, 16)}
		for i := 0; i+1 < len(b); i += 2 {
			p := int(b[i]) % 16
			d.Nodes[p].Children = append(d.Nodes[p].Children, int(int8(b[i+1]))%20)
		}
		if len(b) > 0 {
			d.Roots = []int{int(b[len(b)-1]) % 18}
		}
		if d.CheckHierarchy() != nil {
			return
		}
		// Accepted: every walk from every node must end.
		var walk func(i, depth int)
		walk = func(i, depth int) {
			if depth > 16 {
				t.Fatalf("accepted a hierarchy with a cycle: %v", b)
			}
			for _, c := range d.Nodes[i].Children {
				walk(c, depth+1)
			}
		}
		for i := range d.Nodes {
			walk(i, 0)
		}
	})
}
