package fbx

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real-asset tests read a skinned character and an animation clip from
// the directory named by GFX_FBX_ASSETS (models/hostile.fbx and
// animations/Idle.fbx under it, a Mixamo export); they skip when it is
// unset or absent so the suite stays hermetic elsewhere.
var (
	hostileFBX = fbxAsset("models/hostile.fbx")
	idleFBX    = fbxAsset("animations/Idle.fbx")
)

func fbxAsset(rel string) string {
	if root := os.Getenv("GFX_FBX_ASSETS"); root != "" {
		return filepath.Join(root, rel)
	}
	return ""
}

func loadDoc(t *testing.T, path string) *Doc {
	t.Helper()
	if path == "" {
		t.Skip("GFX_FBX_ASSETS unset")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("asset absent: %v", err)
	}
	d, err := Parse(b)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return d
}

// TestHostileMeshSkin: the 6100 character file yields a skinned mesh with a
// bone hierarchy — the P3b payoff (a real actor, not a placeholder box).
func TestHostileMeshSkin(t *testing.T) {
	doc, err := ToGLTF(loadDoc(t, hostileFBX), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Nodes) == 0 {
		t.Fatal("no nodes")
	}
	if len(doc.Meshes) == 0 {
		t.Fatal("no mesh extracted")
	}
	// Some node must own the mesh, with vertices + a skin.
	var meshNode int = -1
	for i, n := range doc.Nodes {
		if n.Mesh >= 0 {
			meshNode = i
			break
		}
	}
	if meshNode < 0 {
		t.Fatal("no node owns the mesh")
	}
	prim := doc.Meshes[doc.Nodes[meshNode].Mesh].Primitives[0]
	if len(prim.Positions) < 100 {
		t.Fatalf("mesh has only %d verts", len(prim.Positions))
	}
	if len(prim.Indices)%3 != 0 {
		t.Fatalf("indices not triangulated: %d", len(prim.Indices))
	}
	if len(doc.Skins) == 0 {
		t.Fatal("no skin extracted")
	}
	if len(prim.Joints) != len(prim.Positions) || len(prim.Weights) != len(prim.Positions) {
		t.Fatalf("skin attrs mismatch: %d joints / %d weights / %d verts",
			len(prim.Joints), len(prim.Weights), len(prim.Positions))
	}
	// Every vertex's weights should sum to ~1.
	bad := 0
	for _, w := range prim.Weights {
		s := w[0] + w[1] + w[2] + w[3]
		if s < 0.99 || s > 1.01 {
			bad++
		}
	}
	if bad > len(prim.Weights)/20 {
		t.Fatalf("%d/%d vertices have non-normalized weights", bad, len(prim.Weights))
	}
	t.Logf("hostile: %d nodes, %d verts, %d tris, %d joints",
		len(doc.Nodes), len(prim.Positions), len(prim.Indices)/3, len(doc.Skins[0].Joints))
}

// TestHostileMaterials: the mesh splits per material slot and each material
// carries its embedded diffuse texture (Video Content PNG).
func TestHostileMaterials(t *testing.T) {
	doc, err := ToGLTF(loadDoc(t, hostileFBX), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Materials) < 2 {
		t.Fatalf("expected >=2 materials (body+head), got %d", len(doc.Materials))
	}
	textured := 0
	for _, m := range doc.Materials {
		if m.Image != nil {
			textured++
			if b := m.Image.Bounds(); b.Dx() < 16 || b.Dy() < 16 {
				t.Fatalf("material %q: implausible texture %v", m.Name, b)
			}
		}
	}
	if textured == 0 {
		t.Fatal("no material has an embedded diffuse texture")
	}
	prims := 0
	for _, mesh := range doc.Meshes {
		for _, p := range mesh.Primitives {
			prims++
			if p.Material < 0 || p.Material >= len(doc.Materials) {
				t.Fatalf("mesh %q: primitive without material (%d)", mesh.Name, p.Material)
			}
			if len(p.UVs) != len(p.Positions) {
				t.Fatalf("mesh %q: %d UVs for %d verts", mesh.Name, len(p.UVs), len(p.Positions))
			}
		}
	}
	if prims < 3 {
		t.Fatalf("expected >=3 primitives after material split, got %d", prims)
	}
	for i, m := range doc.Materials {
		tex := "untextured"
		if m.Image != nil {
			tex = m.Image.Bounds().Size().String()
		}
		t.Logf("material %d %q: rough %.2f, %s", i, m.Name, m.Roughness, tex)
	}
}

// TestExternalTextures: with the embedded Content blobs stripped, textures
// come from the resolve callback — tried with the recorded paths and their
// basenames.
func TestExternalTextures(t *testing.T) {
	doc := loadDoc(t, hostileFBX)
	var strip func(n *Node)
	strip = func(n *Node) {
		if n.Name == "Content" {
			for i := range n.Props {
				n.Props[i].Raw = nil
			}
		}
		for _, c := range n.Children {
			strip(c)
		}
	}
	strip(doc.Root)

	// A 8x8 PNG served for any basename-shaped request.
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	var asked []string
	g, err := ToGLTF(doc, func(uri string) ([]byte, error) {
		asked = append(asked, uri)
		if strings.ContainsAny(uri, `/\`) {
			return nil, os.ErrNotExist // exporter temp dirs never exist
		}
		return buf.Bytes(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range g.Materials {
		if m.Image == nil {
			t.Fatalf("material %q: no image via resolver (asked %q)", m.Name, asked)
		}
		if b := m.Image.Bounds(); b.Dx() != 8 || b.Dy() != 8 {
			t.Fatalf("material %q: got %v, want the resolver's 8x8", m.Name, b)
		}
	}
	base := false
	for _, u := range asked {
		if u == "Soldier_Body_diffuse.png" || u == "Soldier_head_diffuse.png" {
			base = true
		}
	}
	if !base {
		t.Fatalf("resolver never asked for a bare basename: %q", asked)
	}
}

// TestIdleClip: the 7700 animation file yields a clip with rotation channels.
func TestIdleClip(t *testing.T) {
	doc, err := ToGLTF(loadDoc(t, idleFBX), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Animations) == 0 {
		t.Fatal("no animation clip extracted")
	}
	clip := doc.Animations[0]
	if clip.Duration <= 0 {
		t.Fatalf("clip duration = %v", clip.Duration)
	}
	rot := 0
	for _, c := range clip.Channels {
		if len(c.Rot) > 0 {
			rot++
		}
	}
	if rot == 0 {
		t.Fatal("no rotation channels")
	}
	t.Logf("Idle: %d channels (%d rotation), %.2fs", len(clip.Channels), rot, clip.Duration)
}
