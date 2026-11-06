//go:build !js

package render

import "testing"

// px reads one pixel as 0..255 RGBA.
func px(pix []byte, w, x, y int) (r, g, b, a int) {
	i := (y*w + x) * 4
	return int(pix[i]), int(pix[i+1]), int(pix[i+2]), int(pix[i+3])
}

// near reports whether v is within tol of want — blend results go through
// an 8-bit target, and multiply/screen do a rounding trip each.
func near(v, want, tol int) bool { return v >= want-tol && v <= want+tol }

// A group composited with Multiply darkens what is under it: the classic
// yellow-over-cyan check, where multiply gives green and Normal would just
// paint the yellow.
func TestBlendMultiply(t *testing.T) {
	const W, H = 40, 40
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, RGBA{G: 1, B: 1, A: 1}) // cyan backdrop
		r.BeginGroup()
		r.FillRect(0, 0, W, H, RGBA{R: 1, G: 1, A: 1}) // yellow
		r.EndGroup(BlendMultiply, 1)
	})
	red, green, blue, _ := px(pix, W, 20, 20)
	// (1,1,0) * (0,1,1) = (0,1,0)
	if !near(red, 0, 4) || !near(green, 255, 4) || !near(blue, 0, 4) {
		t.Errorf("multiply gave (%d,%d,%d), want green", red, green, blue)
	}
}

// Screen lightens: mid grey over mid grey comes out at 3/4.
func TestBlendScreen(t *testing.T) {
	const W, H = 40, 40
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1})
		r.BeginGroup()
		r.FillRect(0, 0, W, H, RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1})
		r.EndGroup(BlendScreen, 1)
	})
	red, _, _, _ := px(pix, W, 20, 20)
	if !near(red, 191, 4) { // 0.5 + 0.5 - 0.25 = 0.75
		t.Errorf("screen gave %d, want ~191", red)
	}
}

// Darken and Lighten pick a channel at a time, and — the part fixed-function
// min()/max() gets wrong without help — they must leave the backdrop alone
// where the group covers nothing.
func TestBlendDarkenLighten(t *testing.T) {
	const W, H = 60, 40
	// The group paints only the left half; the right half must stay as the
	// backdrop drew it.
	for _, tc := range []struct {
		mode       BlendMode
		wantR      int
		wantOuterR int
	}{
		{BlendDarken, 64, 128},   // min(0.25, 0.5)
		{BlendLighten, 191, 128}, // max(0.75, 0.5)
	} {
		src := float32(0.25)
		if tc.mode == BlendLighten {
			src = 0.75
		}
		pix := drawClipped(t, W, H, func(r *Renderer2D) {
			r.FillRect(0, 0, W, H, RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1})
			r.BeginGroup()
			r.FillRect(0, 0, 30, H, RGBA{R: src, G: src, B: src, A: 1})
			r.EndGroup(tc.mode, 1)
		})
		if got, _, _, _ := px(pix, W, 10, 20); !near(got, tc.wantR, 4) {
			t.Errorf("%v inside gave %d, want ~%d", tc.mode, got, tc.wantR)
		}
		if got, _, _, _ := px(pix, W, 50, 20); !near(got, tc.wantOuterR, 4) {
			t.Errorf("%v outside the group gave %d, want the backdrop ~%d",
				tc.mode, got, tc.wantOuterR)
		}
	}
}

// A group alpha applies to the group as a WHOLE: two overlapping opaque
// squares inside a half-transparent group show one uniform tint, where
// drawing them at 0.5 each would double up where they overlap.
func TestGroupAlphaIsNotPerDraw(t *testing.T) {
	const W, H = 60, 40
	grouped := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, White)
		r.BeginGroup()
		r.FillRect(5, 5, 30, 30, RGBA{A: 1})
		r.FillRect(20, 5, 30, 30, RGBA{A: 1})
		r.EndGroup(BlendNormal, 0.5)
	})
	// Inside one square and inside the overlap: the same grey.
	one, _, _, _ := px(grouped, W, 10, 20)
	both, _, _, _ := px(grouped, W, 27, 20)
	if !near(one, 128, 4) {
		t.Errorf("group at alpha 0.5 gave %d, want ~128", one)
	}
	if !near(both, one, 3) {
		t.Errorf("the overlap gave %d but a single square gave %d — the "+
			"alpha was applied per draw, not to the group", both, one)
	}

	// Without the group, the overlap really is twice-composited.
	loose := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, White)
		r.FillRect(5, 5, 30, 30, RGBA{A: 0.5})
		r.FillRect(20, 5, 30, 30, RGBA{A: 0.5})
	})
	if o, _, _, _ := px(loose, W, 27, 20); o >= one-10 {
		t.Errorf("ungrouped overlap gave %d, expected darker than %d", o, one)
	}
}

// A group nested in another group is composited into it, so both modes
// apply in turn: white multiplied by grey inside, that result multiplied
// onto the backdrop outside.
func TestNestedGroupsCompose(t *testing.T) {
	const W, H = 40, 40
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, White)
		r.BeginGroup()
		r.FillRect(0, 0, W, H, RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1})
		r.BeginGroup()
		r.FillRect(0, 0, W, H, RGBA{R: 0.5, G: 0.5, B: 0.5, A: 1})
		r.EndGroup(BlendMultiply, 1) // 0.5*0.5 = 0.25 inside the group
		r.EndGroup(BlendNormal, 1)
	})
	if got, _, _, _ := px(pix, W, 20, 20); !near(got, 64, 5) {
		t.Errorf("nested multiply gave %d, want ~64", got)
	}
}

// An empty group composites nothing and leaves no trace.
func TestEmptyGroupDrawsNothing(t *testing.T) {
	const W, H = 20, 20
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, White)
		r.BeginGroup()
		r.EndGroup(BlendMultiply, 1)
	})
	if got, _, _, a := px(pix, W, 10, 10); got != 255 || a != 255 {
		t.Errorf("an empty group changed the pixel to %d (alpha %d)", got, a)
	}
}

// A luminosity soft mask fades what follows by the BRIGHTNESS of its own
// drawing: a white-to-black gradient mask over a red fill leaves red on
// the white side and the backdrop on the black side.
func TestSoftMaskLuminosity(t *testing.T) {
	const W, H = 64, 16
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, White)
		r.BeginSoftMask()
		r.GradientRect(0, 0, W, H, 0, White, RGBA{A: 1}, 0)
		r.EndSoftMask(SoftMaskLuminosity, RGBA{A: 1})
		r.FillRect(0, 0, W, H, RGBA{R: 1, A: 1})
		r.PopClip()
	})
	// Left: the mask is white, so the red is fully painted. Right: the
	// mask is black, so the white backdrop survives.
	lr, lg, _, _ := px(pix, W, 2, 8)
	rr, rg, _, _ := px(pix, W, W-3, 8)
	if !near(lr, 255, 6) || lg > 24 {
		t.Errorf("under the white end of the mask: (%d,%d), want red", lr, lg)
	}
	if !near(rr, 255, 6) || !near(rg, 255, 24) {
		t.Errorf("under the black end of the mask: (%d,%d), want the white backdrop", rr, rg)
	}
	// And the middle is a blend of the two, in order.
	var last int
	for i, x := range []int{6, 20, 34, 48, 60} {
		_, g, _, _ := px(pix, W, x, 8)
		if i > 0 && g < last-4 {
			t.Errorf("the fade is not monotonic: green %d at x=%d after %d", g, x, last)
		}
		last = g
	}
}

// An alpha soft mask takes the coverage from what the mask drawing
// covered, whatever colour it was drawn in: black at half alpha and white
// at half alpha mask identically.
func TestSoftMaskAlpha(t *testing.T) {
	const W, H = 32, 16
	for _, c := range []RGBA{{A: 0.5}, {R: 1, G: 1, B: 1, A: 0.5}} {
		pix := drawClipped(t, W, H, func(r *Renderer2D) {
			r.FillRect(0, 0, W, H, White)
			r.BeginSoftMask()
			r.FillRect(0, 0, W, H, c)
			r.EndSoftMask(SoftMaskAlpha, RGBA{A: 1})
			r.FillRect(0, 0, W, H, RGBA{A: 1}) // black
			r.PopClip()
		})
		if got, _, _, _ := px(pix, W, 16, 8); !near(got, 128, 6) {
			t.Errorf("alpha mask at 0.5 over white gave %d, want ~128", got)
		}
	}
}

// A soft mask is a clip: a box clip pushed inside one narrows the scissor
// while the mask still covers what it did — the mask's extent and the
// scissor are not the same rectangle.
func TestSoftMaskNarrowedByBoxClip(t *testing.T) {
	const W, H = 40, 40
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, White)
		r.BeginSoftMask()
		r.FillRect(0, 0, W, H, White) // let everything through
		r.EndSoftMask(SoftMaskLuminosity, RGBA{A: 1})
		r.PushClip(0, 0, 20, H) // ... but only on the left half
		r.FillRect(0, 0, W, H, RGBA{R: 1, A: 1})
		r.PopClip()
		r.PopClip()
	})
	if got, g, _, _ := px(pix, W, 10, 20); !near(got, 255, 4) || g > 16 {
		t.Errorf("inside both the mask and the box: (%d,%d), want red", got, g)
	}
	if _, g, _, _ := px(pix, W, 30, 20); !near(g, 255, 4) {
		t.Errorf("outside the box clip green=%d, want the white backdrop", g)
	}
}

// An empty soft mask covers nothing, so it masks everything away — the
// same thing a degenerate clip path does.
func TestEmptySoftMaskClipsAll(t *testing.T) {
	const W, H = 20, 20
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, White)
		r.BeginSoftMask()
		r.EndSoftMask(SoftMaskLuminosity, RGBA{A: 1})
		r.FillRect(0, 0, W, H, RGBA{R: 1, A: 1})
		r.PopClip()
	})
	if rr, g, _, _ := px(pix, W, 10, 10); g < 250 || rr < 250 {
		t.Errorf("an empty mask let (%d,%d) through, want the white backdrop", rr, g)
	}
}

// A path clip inside a soft mask intersects it: the mask is rendered
// through the soft mask's coverage, so only where BOTH allow it does
// anything reach the frame.
func TestPathClipInsideSoftMask(t *testing.T) {
	const W, H = 40, 40
	// The mask lets through the left half only; the path is a box on the
	// top half, given as a path so it takes the mask route.
	p := NewPath()
	p.MoveTo(0, 0)
	p.LineTo(W, 0)
	p.LineTo(W, 20)
	p.LineTo(0, 20)
	p.Close()
	pix := drawClipped(t, W, H, func(r *Renderer2D) {
		r.FillRect(0, 0, W, H, White)
		r.BeginSoftMask()
		r.FillRect(0, 0, 20, H, White)
		r.EndSoftMask(SoftMaskLuminosity, RGBA{A: 1})
		r.PushClipPath(p, NonZero)
		r.FillRect(0, 0, W, H, RGBA{R: 1, A: 1})
		r.PopClip()
		r.PopClip()
	})
	for _, tc := range []struct {
		x, y    int
		wantRed bool
		where   string
	}{
		{10, 10, true, "inside both"},
		{30, 10, false, "outside the mask"},
		{10, 30, false, "outside the path"},
		{30, 30, false, "outside both"},
	} {
		_, g, _, _ := px(pix, W, tc.x, tc.y)
		if red := g < 128; red != tc.wantRed {
			t.Errorf("%s: green=%d (red=%v), want red=%v", tc.where, g, red, tc.wantRed)
		}
	}
}
