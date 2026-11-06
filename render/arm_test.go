package render

import (
	"image"
	"testing"
)

func rgba1(r, g, b uint8) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, 1, 1))
	im.Pix[0], im.Pix[1], im.Pix[2], im.Pix[3] = r, g, b, 255
	return im
}

// armImage packs glTF's two textures into the ARM layout the material
// pipeline reads: occlusion in R (strength applied), roughness in G,
// metallic in B, with 1 wherever a map is missing.
func TestARMImagePacksOcclusionAndMetallicRoughness(t *testing.T) {
	mr := rgba1(7, 100, 200)
	occ := rgba1(0, 9, 9)
	if got := armImage(mr, occ, 1).Pix[:3]; got[0] != 0 || got[1] != 100 || got[2] != 200 {
		t.Errorf("mr+occ: %v", got)
	}
	// Half strength: occluded = 1 + 0.5*(0-1) = 0.5.
	if got := armImage(mr, occ, 0.5).Pix[0]; got < 127 || got > 128 {
		t.Errorf("half-strength occlusion = %d, want ~128", got)
	}
	if got := armImage(mr, nil, 1).Pix[:3]; got[0] != 255 || got[1] != 100 || got[2] != 200 {
		t.Errorf("mr only: %v (occlusion must read 1)", got)
	}
	if got := armImage(nil, occ, 1).Pix[:3]; got[0] != 0 || got[1] != 255 || got[2] != 255 {
		t.Errorf("occ only: %v (roughness and metallic must read 1)", got)
	}
	if armImage(mr, mr, 1) != mr {
		t.Error("an ORM map shared by both slots should be used as it is")
	}
	if armImage(nil, nil, 1) != nil {
		t.Error("no maps, no ARM")
	}
}
