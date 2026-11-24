package fbx

import "testing"

// FBX 7.0–7.4 key objects and connections by int64 id, like 7.5+ (only
// the record offsets differ): a 7300 file's mesh must hang off its model.
func TestGraphIDKeyedFrom7000(t *testing.T) {
	for _, ver := range []uint32{7100, 7300, 7400, 7700} {
		d := &Doc{Version: ver, Root: &Node{Children: []*Node{
			{Name: "Objects", Children: []*Node{
				{Name: "Geometry", Props: []Prop{{Kind: 'L', I: 11}, {Kind: 'S', S: "Body\x00\x01Geometry"}, {Kind: 'S', S: "Mesh"}}},
				{Name: "Model", Props: []Prop{{Kind: 'L', I: 22}, {Kind: 'S', S: "Body\x00\x01Model"}, {Kind: 'S', S: "Mesh"}}},
			}},
			{Name: "Connections", Children: []*Node{
				{Name: "C", Props: []Prop{{Kind: 'S', S: "OO"}, {Kind: 'L', I: 11}, {Kind: 'L', I: 22}}},
			}},
		}}}
		g := d.buildGraph()
		if g.childOf[11] != 22 || g.objs[22] == nil || g.objs[22].name != "Body" {
			t.Errorf("FBX %d: geometry→model link %v, model %+v", ver, g.childOf[11], g.objs[22])
		}
	}
}
