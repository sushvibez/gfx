package render

import "testing"

func TestPatchSegs(t *testing.T) {
	near := func(a, b float32) bool { return a-b < 1e-4 && b-a < 1e-4 }
	if s := patchSegs(10, 100, 30, PatchStretch); len(s) != 1 || s[0] != (patchSeg{10, 100, 1}) {
		t.Errorf("stretch = %v", s)
	}
	s := patchSegs(0, 100, 30, PatchRepeat)
	if len(s) != 4 || !near(s[3].pos, 90) || !near(s[3].size, 10) || !near(s[3].frac, 1.0/3) {
		t.Errorf("repeat = %v, want three whole tiles and a third", s)
	}
	s = patchSegs(0, 100, 30, PatchRound)
	if len(s) != 3 || !near(s[2].pos, 200.0/3) || !near(s[2].size, 100.0/3) || s[2].frac != 1 {
		t.Errorf("round = %v, want three tiles of 33.3", s)
	}
	if s := patchSegs(0, 10, 30, PatchRound); len(s) != 1 || s[0].size != 10 {
		t.Errorf("round of a short span = %v, want one squeezed tile", s)
	}
	if a, b := clampBorders(80, 40, 60); !near(a, 40) || !near(b, 20) {
		t.Errorf("clampBorders = %v, %v", a, b)
	}
}
