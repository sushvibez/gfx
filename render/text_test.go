//go:build !js

package render

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"

	"github.com/sushvibez/gfx/gpu"
)

func TestWrapText(t *testing.T) {
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	f, err := r.NewFont(goregular.TTF, 18)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	// Width that forces wrapping of this sentence.
	lines := r.WrapText(f, "the quick brown fox jumps over the lazy dog", 120)
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d: %v", len(lines), lines)
	}
	// Every line must fit within the limit.
	for _, ln := range lines {
		if w, _ := r.MeasureText(f, ln); w > 120 {
			t.Errorf("line %q width %v exceeds 120", ln, w)
		}
	}
	// No words were dropped (rejoining yields the original word sequence).
	got := ""
	for i, ln := range lines {
		if i > 0 {
			got += " "
		}
		got += ln
	}
	if got != "the quick brown fox jumps over the lazy dog" {
		t.Fatalf("wrap lost/reordered words: %q", got)
	}
	// Explicit newlines force breaks.
	if l2 := r.WrapText(f, "a\nb", 500); len(l2) != 2 {
		t.Fatalf("newline split = %v", l2)
	}
}

// TestRichText renders two differently-colored spans that wrap, and checks
// each color appears at its expected place.
func TestRichText(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 240, 120
	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	f, err := r.NewFont(goregular.TTF, 20)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	r.Begin(W, H)
	// "RED " in red, "green" in green, on one line near the top.
	x := float32(4)
	redW, _ := r.MeasureText(f, "RED ")
	r.DrawText(f, x, 4, "RED ", RGBA{R: 1, A: 1})
	r.DrawText(f, x+redW, 4, "green", RGBA{G: 1, A: 1})

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())
	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}
	// Scan the top text band for red-dominant and green-dominant pixels.
	red, green := 0, 0
	for y := 4; y < int(4+f.Height()); y++ {
		for x := 0; x < W; x++ {
			i := (y*W + x) * 4
			rr, gg := pix[i], pix[i+1]
			if rr > 150 && gg < 80 {
				red++
			}
			if gg > 150 && rr < 80 {
				green++
			}
		}
	}
	if red < 30 || green < 30 {
		t.Fatalf("rich text colors: red=%d green=%d, want both present", red, green)
	}
}

func TestTextRendering(t *testing.T) {
	device, queue := headlessDevice(t)
	const W, H = 360, 120

	target := device.CreateRenderTexture(W, H, gpu.FormatRGBA8Unorm)
	defer target.Release()
	view := target.CreateView()
	defer view.Release()

	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()

	f, err := r.NewFont(goregular.TTF, 24)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	const msg = "Hello, graphics!"
	tw, th := r.MeasureText(f, msg)
	if tw <= 0 || th <= 0 {
		t.Fatalf("MeasureText = %v,%v", tw, th)
	}
	if tw > W || th > H {
		t.Fatalf("text %vx%v larger than target", tw, th)
	}

	r.Begin(W, H)
	r.DrawText(f, 20, 40, msg, RGBA{1, 1, 1, 1})

	enc := device.CreateCommandEncoder()
	pass := enc.BeginRenderPass(view, gpu.Color{A: 1})
	if err := r.Flush(pass); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	pass.End()
	queue.Submit(enc.Finish())

	pix, err := device.ReadTexture(queue, target, W, H)
	if err != nil {
		t.Fatalf("ReadTexture: %v", err)
	}

	// Coverage: bright pixels must exist inside the text box and nowhere
	// outside it.
	bright := 0
	for y := 40; y < 40+int(th); y++ {
		for x := 20; x < 20+int(tw); x++ {
			i := (y*W + x) * 4
			if pix[i] > 200 && pix[i+1] > 200 && pix[i+2] > 200 {
				bright++
			}
		}
	}
	if bright < 100 {
		t.Errorf("only %d bright pixels inside text box, expected glyph coverage", bright)
	}
	outside := 0
	for y := range 20 {
		for x := range W {
			i := (y*W + x) * 4
			if pix[i] != 0 || pix[i+1] != 0 || pix[i+2] != 0 {
				outside++
			}
		}
	}
	if outside != 0 {
		t.Errorf("%d non-background pixels above the text box", outside)
	}

	img := &image.RGBA{Pix: pix, Stride: W * 4, Rect: image.Rect(0, 0, W, H)}
	out := filepath.Join(os.TempDir(), "gfx-text.png")
	if fh, err := os.Create(out); err == nil {
		_ = png.Encode(fh, img)
		fh.Close()
		t.Logf("screenshot: %s", out)
	}
}

// Qt reports a text item's height as the font's OS/2 WINDOW ascent+descent
// rounded up, not the face's own line height. Markup written against Qt is
// laid out against those numbers, so the gml engine asks for them; a UI
// built on the Go builders keeps the face's own.
func TestQtLineMetrics(t *testing.T) {
	asc, desc := winMetrics(goregular.TTF)
	if asc <= 0 || desc <= 0 {
		t.Skip("test face has no OS/2 window metrics")
	}
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	f, err := r.NewFont(goregular.TTF, 32)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	face, faceAsc := f.Height(), f.Ascent()
	if f.FaceHeight() != face || f.FaceAscent() != faceAsc {
		t.Fatal("Face* must report the face's own metrics whatever the mode is")
	}
	SetQtTextMetrics(true)
	defer SetQtTextMetrics(false)
	// The window metrics cover the tallest glyph in the face, so the box is
	// never shorter than the face's line height.
	if f.Height() < face {
		t.Errorf("Qt height %v is under the face height %v", f.Height(), face)
	}
	want := float32(int((float32(asc+desc) * 32 / float32(f.upem)) + 0.999))
	if d := f.Height() - want; d > 0.6 || d < -0.6 {
		t.Errorf("Qt height = %v, want %v (ceil of the window metrics)", f.Height(), want)
	}
	if f.FaceHeight() != face {
		t.Error("FaceHeight changed with the mode")
	}
	SetQtTextMetrics(false)
	if f.Height() != face {
		t.Error("turning the mode off must restore the face's metrics")
	}
}

// A family arrives one FACE at a time: a QML file writes one FontLoader
// for its regular file and one for its bold, and BOTH report the same
// family name. Keyed by family alone, whichever loaded last answered for
// both — which rendered whole documents in bold.
func TestGlobalFontStylesStayApart(t *testing.T) {
	reg := []byte("REGULAR")
	bold := []byte("BOLD")
	RegisterFontBytesStyled("styletest", StyleRegular, reg)
	RegisterFontBytesStyled("styletest", StyleBold, bold)
	if got := globalFontBytes("styletest", StyleRegular); string(got) != "REGULAR" {
		t.Errorf("regular lookup = %q", got)
	}
	if got := globalFontBytes("StyleTest", StyleBold); string(got) != "BOLD" {
		t.Errorf("bold lookup = %q (family match is case-insensitive)", got)
	}
	// A style the family has no face for falls back to its regular, the way
	// a font database does — the caller then emboldens synthetically.
	if got := globalFontBytes("styletest", StyleBold|StyleItalic); string(got) != "REGULAR" {
		t.Errorf("bold-italic lookup = %q, want the regular face", got)
	}
	// A family with only a bold face answers with it rather than nothing.
	RegisterFontBytesStyled("boldonly", StyleBold, bold)
	if got := globalFontBytes("boldonly", StyleRegular); string(got) != "BOLD" {
		t.Errorf("one-face family regular lookup = %q, want its only face", got)
	}
	if got := globalFontBytes("nosuchfamily", StyleRegular); got != nil {
		t.Errorf("unknown family = %q, want nothing", got)
	}
}

// The style a face declares comes from the FILE: its OS/2 weight class and
// the head table's italic bit. A "Book" or "Light" face declares neither,
// which is right — it is that family's regular face.
func TestFaceStyleFromFile(t *testing.T) {
	if got := FaceStyle(goregular.TTF); got != StyleRegular {
		t.Errorf("Go Regular read as style %v, want regular", got)
	}
	const boldPath = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
	b, err := os.ReadFile(boldPath)
	if err != nil {
		t.Skip("DejaVuSans-Bold not installed")
	}
	if FaceWeightClass(b) < 600 {
		t.Skip("the installed -Bold file is not a bold face (variable-font symlink)")
	}
	if got := FaceStyle(b); got&StyleBold == 0 {
		t.Errorf("DejaVu Sans Bold read as style %v, want bold", got)
	}
}

// A box sized to exactly its text's width — `width: label.implicitWidth`,
// the QML idiom every chat bubble uses — must not wrap. The running sum
// that decides where to break adds each word and space on its own, and can
// total a hair more than the same text measured in one piece; the wrap
// verifies at the margin, so the line that fits stays one line.
func TestWrapKeepsALineThatExactlyFits(t *testing.T) {
	device, queue := headlessDevice(t)
	r, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatalf("New2D: %v", err)
	}
	defer r.Release()
	f, err := r.NewFont(goregular.TTF, 12)
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Release()

	for _, s := range []string{
		"Great! Just finished the PathView page",
		"45 and counting! Working on showcases now",
		"the quick brown fox jumps over the lazy dog",
	} {
		w, _ := r.MeasureText(f, s)
		if lines := r.WrapText(f, s, w); len(lines) != 1 {
			t.Errorf("%q at its own width %v wrapped into %d lines: %v", s, w, len(lines), lines)
		}
	}
}

// Which line metrics Qt lays a face out with is the FACE's choice: OS/2
// fsSelection bit 7 (USE_TYPO_METRICS) says "use my typographic
// ascender/descender", and FreeType — so Qt — honours it; otherwise the
// WINDOWS ascent/descent apply. The two differ a lot (Ubuntu Sans asks for
// 940/260 where its win metrics say 1020/223), which put every unstyled
// label in the corpus 2px low. The test flips the bit on a copy of the
// bundled face and reads the metrics back.
func TestWinMetricsHonourUseTypoMetrics(t *testing.T) {
	os2 := os2Offset(t, goregular.TTF)
	win := func(b []byte) (int, int) { a, d := winMetrics(b); return a, d }
	plain := make([]byte, len(goregular.TTF))
	copy(plain, goregular.TTF)
	// The bit must start CLEAR for this face, or the test proves nothing.
	if plain[os2+63]&0x80 != 0 {
		t.Skip("the bundled face already sets USE_TYPO_METRICS")
	}
	winA, winD := win(plain)
	typoA := int(int16(uint16(plain[os2+68])<<8 | uint16(plain[os2+69])))
	typoD := int(int16(uint16(plain[os2+70])<<8 | uint16(plain[os2+71])))
	if winA == typoA && winD == -typoD {
		t.Skip("this face's win and typo metrics agree")
	}
	typoed := make([]byte, len(plain))
	copy(typoed, plain)
	typoed[os2+63] |= 0x80
	gotA, gotD := win(typoed)
	if gotA != typoA || gotD != -typoD {
		t.Errorf("with USE_TYPO_METRICS set: %d/%d, want the typo metrics %d/%d",
			gotA, gotD, typoA, -typoD)
	}
	if gotA == winA && gotD == winD {
		t.Errorf("the flag changed nothing (win %d/%d)", winA, winD)
	}
}

// os2Offset is the file offset of the OS/2 table.
func os2Offset(t *testing.T, ttf []byte) int {
	t.Helper()
	n := int(uint16(ttf[4])<<8 | uint16(ttf[5]))
	off := 12
	for i := 0; i < n; i++ {
		if string(ttf[off:off+4]) == "OS/2" {
			return int(uint32(ttf[off+8])<<24 | uint32(ttf[off+9])<<16 |
				uint32(ttf[off+10])<<8 | uint32(ttf[off+11]))
		}
		off += 16
	}
	t.Fatal("no OS/2 table")
	return 0
}

// Subpixel glyphs must land on the pixel grid vertically, not a third of
// a pixel off it.
//
// The LCD path rasterises at three times the size in both directions and
// reduces three source columns to one output pixel. It aligns the
// horizontal sample window to a multiple of three; it used not to align
// the vertical one, and took the placement from the 1x face instead. So
// every glyph was shifted down by its own 3x top modulo three — nothing,
// a third of a pixel, or two thirds, decided per glyph and per size. In
// Inter at 13 px the capitals and ascenders came out a third low and the
// x-height letters two thirds; at 14 px the other way about. A line of
// text sat at three different heights.
func TestSubpixelGlyphsAlignVertically(t *testing.T) {
	device, queue := headlessDevice(t)
	r2, err := New2D(device, queue, gpu.FormatRGBA8Unorm)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Release()

	faces := [][2]any{{"Go Regular", goregular.TTF}}
	// Inter is what the games draw with and what showed this up. It is
	// fetched rather than vendored, so it is a bonus when it is there.
	for _, up := range []string{"..", "../..", "../../.."} {
		if b, err := os.ReadFile(filepath.Join(up, "build", "fonts", "Inter-Regular.ttf")); err == nil {
			faces = append(faces, [2]any{"Inter Regular", b})
			break
		}
	}
	bites := false
	for _, fc := range faces {
		name, ttf := fc[0].(string), fc[1].([]byte)
		for _, size := range []float64{11, 12, 13, 14, 16, 20, 34} {
			f, err := r2.NewFont(ttf, size)
			if err != nil {
				t.Fatal(err)
			}
			if !f.ensureFace3() {
				t.Skip("no 3x face")
			}
			for _, r := range []rune{'A', 'E', 'H', 'b', 'd', 'h', 'l', 'o', 'x', 'n', '6', 'g', 'M'} {
				g, ok := f.ensureSubpixel(r, 0)
				if !ok || g.h == 0 {
					continue
				}
				d3, _, _, _, ok3 := f.face3.Glyph(fixed.P(0, 0), r)
				if !ok3 {
					continue
				}
				// The bearing must be the aligned raster's own top, so
				// the three source rows folded into each output pixel
				// are the three that belong to it.
				want := float32(d3.Min.Y-((d3.Min.Y%3)+3)%3) / subpixelScale
				if g.bearingY != want {
					t.Errorf("%s %.0f px %q: bearingY %v, wanted %v (3x top %d)",
						name, size, r, g.bearingY, want, d3.Min.Y)
				}
				// And whether this case would have caught the old rule,
				// which took the bearing from the 1x face.
				if g1, ok := f.ensure(r); ok && g1.bearingY != want {
					bites = true
				}
			}
		}
	}
	if !bites {
		t.Skip("no font here rasterises a glyph whose 3x top is off the 1x grid; " +
			"fetch Inter (scripts/fetch-fonts.sh) to exercise this")
	}
}
