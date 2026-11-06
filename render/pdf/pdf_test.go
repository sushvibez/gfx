package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"testing"

	"github.com/sushvibez/gfx/render"
)

// buildPDF assembles a one-page PDF with the given content stream, xref
// table and all — the smallest real file the reader has to handle.
func buildPDF(content string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] " +
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// A hand-built file opens: its page tree, geometry, resources and content
// all come back.
func TestLoadMinimalFile(t *testing.T) {
	doc, err := Load(buildPDF("1 0 0 RG 0 0 1 rg 10 10 100 50 re f"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if doc.PageCount() != 1 {
		t.Fatalf("%d pages, want 1", doc.PageCount())
	}
	p := doc.Page(0)
	if w, h := p.Size(); w != 200 || h != 100 {
		t.Errorf("page is %vx%v, want 200x100", w, h)
	}
	if got := string(p.Content()); got != "1 0 0 RG 0 0 1 rg 10 10 100 50 re f" {
		t.Errorf("content = %q", got)
	}
	if _, ok := p.Resources["Font"]; !ok {
		t.Error("the page did not inherit its Resources")
	}
}

// A page tree inherits Resources, MediaBox and Rotate from its parents.
func TestPageTreeInheritance(t *testing.T) {
	src := "%PDF-1.7\n" +
		"1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n" +
		"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 2 /MediaBox [0 0 300 400] " +
		"/Rotate 90 /Resources << /Font << /F1 9 0 R >> >> >> endobj\n" +
		"3 0 obj << /Type /Pages /Parent 2 0 R /Kids [4 0 R 5 0 R] /Count 2 >> endobj\n" +
		"4 0 obj << /Type /Page /Parent 3 0 R >> endobj\n" +
		"5 0 obj << /Type /Page /Parent 3 0 R /MediaBox [0 0 100 100] /Rotate 0 >> endobj\n" +
		"trailer << /Root 1 0 R >>\n"
	doc, err := Load([]byte(src))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if doc.PageCount() != 2 {
		t.Fatalf("%d pages, want 2", doc.PageCount())
	}
	// The first page inherits everything; a rotated page reports its size
	// the way it is displayed.
	p0 := doc.Page(0)
	if w, h := p0.Size(); w != 400 || h != 300 {
		t.Errorf("rotated page is %vx%v, want 400x300", w, h)
	}
	if p0.Rotate != 90 {
		t.Errorf("Rotate = %d, want 90", p0.Rotate)
	}
	if _, ok := p0.Resources["Font"]; !ok {
		t.Error("inherited Resources missing")
	}
	// The second overrides both.
	p1 := doc.Page(1)
	if w, h := p1.Size(); w != 100 || h != 100 {
		t.Errorf("page 1 is %vx%v, want 100x100", w, h)
	}
	if p1.Rotate != 0 {
		t.Errorf("page 1 Rotate = %d, want 0", p1.Rotate)
	}
}

// A file whose xref is wrong still opens: the reader scans for objects.
func TestBrokenXrefRecovers(t *testing.T) {
	b := buildPDF("0 g 0 0 10 10 re f")
	// Point startxref at nonsense.
	i := bytes.LastIndex(b, []byte("startxref"))
	b = append(b[:i], []byte("startxref\n999999\n%%EOF\n")...)
	doc, err := Load(b)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if doc.PageCount() != 1 {
		t.Fatalf("%d pages, want 1", doc.PageCount())
	}
	if len(doc.Page(0).Content()) == 0 {
		t.Error("no content after the xref scan")
	}
}

// The object parser handles the syntax real files use.
func TestObjectParsing(t *testing.T) {
	src := `<< /A 1 /B 2.5 /C /Name#20With#20Spaces /D (a\(b\)c\n) /E <414243>
	         /F [1 2 R 3 true null] /G << /H .5 >> /I -4. >>`
	l := &lexer{b: []byte(src)}
	obj, err := l.object()
	if err != nil {
		t.Fatalf("object: %v", err)
	}
	d, ok := obj.(Dict)
	if !ok {
		t.Fatalf("got %T, want a Dict", obj)
	}
	if v, _ := d["A"].(int64); v != 1 {
		t.Errorf("A = %v", d["A"])
	}
	if v, _ := d["B"].(float64); v != 2.5 {
		t.Errorf("B = %v", d["B"])
	}
	if v, _ := d["C"].(Name); v != "Name With Spaces" {
		t.Errorf("C = %q", d["C"])
	}
	if v, _ := d["D"].(String); string(v) != "a(b)c\n" {
		t.Errorf("D = %q", d["D"])
	}
	if v, _ := d["E"].(String); string(v) != "ABC" {
		t.Errorf("E = %q", d["E"])
	}
	arr, _ := d["F"].(Array)
	// "1 2 R" is ONE element (a reference), then 3, true and null.
	if len(arr) != 4 {
		t.Fatalf("F has %d elements, want 4", len(arr))
	}
	if r, ok := arr[0].(Ref); !ok || r.Num != 1 || r.Gen != 2 {
		t.Errorf("F[0] = %v, want a reference 1 2 R", arr[0])
	}
	inner, _ := d["G"].(Dict)
	if v, _ := inner["H"].(float64); v != 0.5 {
		t.Errorf("G.H = %v, want 0.5 (a bare .5)", inner["H"])
	}
	if v, _ := d["I"].(float64); v != -4 {
		t.Errorf("I = %v, want -4 (a trailing dot)", d["I"])
	}
}

// The filters decode what the spec says they do.
func TestFilters(t *testing.T) {
	doc := &Document{}
	hex := &Stream{Dict: Dict{"Filter": Name("ASCIIHexDecode")}, Raw: []byte("48656C6C6F>"), doc: doc}
	if got, err := hex.Decode(); err != nil || string(got) != "Hello" {
		t.Errorf("ASCIIHex = %q, %v", got, err)
	}
	a85 := &Stream{Dict: Dict{"Filter": Name("ASCII85Decode")}, Raw: []byte("87cURD]i,\"Ebo80~>"), doc: doc}
	if got, err := a85.Decode(); err != nil || string(got) != "Hello World!" {
		t.Errorf("ASCII85 = %q, %v", got, err)
	}
	// RunLength: a literal run of four bytes (length 3 means 3+1), then
	// 'x' repeated 257-253 = 4 times, then the end marker.
	rl := &Stream{Dict: Dict{"Filter": Name("RunLengthDecode")},
		Raw: []byte{3, 'H', 'e', 'l', 'l', 253, 'x', 128}, doc: doc}
	if got, err := rl.Decode(); err != nil || string(got) != "Hellxxxx" {
		t.Errorf("RunLength = %q, %v", got, err)
	}
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	zw.Write([]byte("flate content"))
	zw.Close()
	fl := &Stream{Dict: Dict{"Filter": Name("FlateDecode")}, Raw: zbuf.Bytes(), doc: doc}
	if got, err := fl.Decode(); err != nil || string(got) != "flate content" {
		t.Errorf("Flate = %q, %v", got, err)
	}
	// A filter chain applies in order.
	var chain bytes.Buffer
	zw = zlib.NewWriter(&chain)
	zw.Write([]byte("4142>"))
	zw.Close()
	both := &Stream{Dict: Dict{"Filter": Array{Name("FlateDecode"), Name("ASCIIHexDecode")}},
		Raw: chain.Bytes(), doc: doc}
	if got, err := both.Decode(); err != nil || string(got) != "AB" {
		t.Errorf("chain = %q, %v", got, err)
	}
	// An image codec ends the chain and is reported rather than decoded.
	jpg := &Stream{Dict: Dict{"Filter": Name("DCTDecode")}, Raw: []byte{0xFF, 0xD8, 1, 2}, doc: doc}
	if data, codec, err := jpg.DecodeImage(); err != nil || codec != "DCTDecode" || len(data) != 4 {
		t.Errorf("DCT = %d bytes, codec %q, %v", len(data), codec, err)
	}
}

// A PNG predictor is undone row by row (this is what xref streams and
// most Flate images are filtered through).
func TestPNGPredictor(t *testing.T) {
	// Two rows of three bytes, filter type 2 (Up): the second row is the
	// difference from the first.
	data := []byte{
		0, 10, 20, 30, // row 1, filter None
		2, 1, 2, 3, // row 2, filter Up
	}
	doc := &Document{}
	out := applyPredictor(data, Dict{
		"Predictor": int64(12), "Colors": int64(1), "Columns": int64(3),
		"BitsPerComponent": int64(8),
	}, doc)
	want := []byte{10, 20, 30, 11, 22, 33}
	if !bytes.Equal(out, want) {
		t.Errorf("predictor = %v, want %v", out, want)
	}
}

// Functions: an exponential interpolation, a stitching function that picks
// a sub-function, and a sampled one.
func TestFunctions(t *testing.T) {
	rd := &Renderer{doc: &Document{}}
	exp := rd.function(Dict{
		"FunctionType": int64(2), "Domain": Array{int64(0), int64(1)},
		"C0": Array{float64(0)}, "C1": Array{float64(1)}, "N": int64(1),
	})
	if got := exp.eval([]float32{0.25}); len(got) != 1 || absf(got[0]-0.25) > 1e-5 {
		t.Errorf("type 2 = %v, want [0.25]", got)
	}
	stitch := rd.function(Dict{
		"FunctionType": int64(3), "Domain": Array{int64(0), int64(1)},
		"Bounds": Array{float64(0.5)},
		"Encode": Array{int64(0), int64(1), int64(0), int64(1)},
		"Functions": Array{
			Dict{"FunctionType": int64(2), "C0": Array{float64(0)}, "C1": Array{float64(0)}, "N": int64(1)},
			Dict{"FunctionType": int64(2), "C0": Array{float64(1)}, "C1": Array{float64(1)}, "N": int64(1)},
		},
	})
	if got := stitch.eval([]float32{0.2}); len(got) != 1 || got[0] != 0 {
		t.Errorf("stitching below the bound = %v, want [0]", got)
	}
	if got := stitch.eval([]float32{0.8}); len(got) != 1 || got[0] != 1 {
		t.Errorf("stitching above the bound = %v, want [1]", got)
	}
	sampled := &function{
		kind: 0, domain: []float32{0, 1}, rng: []float32{0, 1},
		samples: []byte{0, 255}, size: []int{2}, bps: 8, outputs: 1,
	}
	if got := sampled.eval([]float32{0.5}); len(got) != 1 || absf(got[0]-0.5) > 0.01 {
		t.Errorf("sampled midpoint = %v, want [0.5]", got)
	}
}

// Encodings: the base tables, /Differences, and a ToUnicode CMap.
func TestEncodings(t *testing.T) {
	doc := &Document{}
	enc := encodingMap(doc, Dict{
		"Encoding": Dict{
			"BaseEncoding": Name("WinAnsiEncoding"),
			"Differences":  Array{int64(65), Name("bullet"), Name("Euro")},
		},
	}, "Helvetica")
	if enc[0x41] != 0x2022 {
		t.Errorf("code 65 = %U, want a bullet (Differences)", enc[0x41])
	}
	if enc[0x42] != 0x20AC {
		t.Errorf("code 66 = %U, want a euro (the second difference)", enc[0x42])
	}
	if enc[0x43] != 'C' {
		t.Errorf("code 67 = %U, want 'C' (the base encoding)", enc[0x43])
	}
	if enc[0xE9] != 0xE9 {
		t.Errorf("code 0xE9 = %U, want e-acute from WinAnsi", enc[0xE9])
	}
}

// A composite font's widths come from /W in both of its forms, and its
// codes are two bytes wide.
func TestType0WidthsAndCodes(t *testing.T) {
	rd := &Renderer{doc: &Document{cache: map[int]Object{}}, fonts: map[string]*fontRes{}}
	f := rd.fontFromDict(Dict{
		"Subtype":  Name("Type0"),
		"Encoding": Name("Identity-H"),
		"DescendantFonts": Array{Dict{
			"Subtype": Name("CIDFontType2"), "DW": int64(1000),
			"W": Array{
				int64(3), Array{float64(250), float64(333)}, // 3 and 4
				int64(10), int64(12), float64(500), // 10..12
			},
		}},
	}, "test")
	if !f.twoByte {
		t.Error("a Type0 font must read two-byte codes")
	}
	if got := f.codes(String{0x00, 0x03, 0x00, 0x0B}); len(got) != 2 || got[0] != 3 || got[1] != 11 {
		t.Errorf("codes = %v, want [3 11]", got)
	}
	for code, want := range map[int]float32{3: 250, 4: 333, 11: 500, 99: 1000} {
		if got := f.widthOf(code); got != want {
			t.Errorf("width of %d = %v, want %v", code, got, want)
		}
	}
}

// A standard-14 font with no /Widths still measures: the reader knows the
// metrics it is expected to.
func TestStandardWidths(t *testing.T) {
	rd := &Renderer{doc: &Document{cache: map[int]Object{}}, fonts: map[string]*fontRes{}}
	f := rd.fontFromDict(Dict{"Subtype": Name("Type1"), "BaseFont": Name("Helvetica")}, "h")
	if got := f.widthOf('W'); got != 944 {
		t.Errorf("Helvetica W = %v, want 944", got)
	}
	if got := f.widthOf('i'); got != 222 {
		t.Errorf("Helvetica i = %v, want 222", got)
	}
	c := rd.fontFromDict(Dict{"Subtype": Name("Type1"), "BaseFont": Name("Courier-Bold")}, "c")
	if got := c.widthOf('i'); got != 600 {
		t.Errorf("Courier i = %v, want 600 (fixed pitch)", got)
	}
}

// The content scanner splits operands from operators, including an inline
// image's own little stream.
func TestScanContent(t *testing.T) {
	ops := scanContent([]byte("1 0 0 1 10 20 cm /F1 12 Tf (hi) Tj "+
		"BI /W 2 /H 2 /BPC 8 /CS /G ID \x00\x01\x02\x03 EI Q"), &Document{})
	var names []string
	for _, o := range ops {
		names = append(names, o.name)
	}
	want := []string{"cm", "Tf", "Tj", "INLINE_IMAGE", "Q"}
	if len(names) != len(want) {
		t.Fatalf("operators %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("operator %d = %q, want %q", i, names[i], want[i])
		}
	}
	if got := numArgs(ops[0], 6); got[4] != 10 || got[5] != 20 {
		t.Errorf("cm operands = %v", got)
	}
	img := ops[3].inline
	if img == nil || len(img.Raw) != 4 {
		t.Fatalf("inline image data = %v", img)
	}
	if w := intOr(&Document{}, img.Dict["Width"], 0); w != 2 {
		t.Errorf("inline /W did not expand to /Width (%d)", w)
	}
}

// The real documents the corpus ships open and report their structure.
// Skipped when they are not checked out beside this repo.
func TestRealDocuments(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	dir := home + "/workspace/qml-examples/pdf_files/"
	for _, tc := range []struct {
		file      string
		wantPages int
	}{
		{"qt6book-with-frontpage.pdf", 728},
		{"bronson-c++.pdf", 845},
	} {
		b, err := os.ReadFile(dir + tc.file)
		if err != nil {
			t.Skipf("%s not available", tc.file)
		}
		doc, err := Load(b)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		if doc.PageCount() != tc.wantPages {
			t.Errorf("%s: %d pages, want %d", tc.file, doc.PageCount(), tc.wantPages)
		}
		p := doc.Page(1)
		if p == nil {
			t.Fatalf("%s: no page 1", tc.file)
		}
		if w, h := p.Size(); w < 100 || h < 100 {
			t.Errorf("%s: page 1 is %vx%v", tc.file, w, h)
		}
		if len(p.Content()) < 100 {
			t.Errorf("%s: page 1 content is %d bytes", tc.file, len(p.Content()))
		}
		if doc.Info("Producer") == "" {
			t.Errorf("%s: no /Producer", tc.file)
		}
	}
}

// A type-4 function is a PostScript calculator program: arithmetic, stack
// juggling and conditionals, with every output clipped to its range.
func TestPostScriptFunctions(t *testing.T) {
	run := func(prog string, rng []Object, in ...float32) []float32 {
		rd := &Renderer{doc: &Document{cache: map[int]Object{}}}
		f := rd.functionFrom(Dict{
			"FunctionType": int64(4),
			"Domain":       Array{int64(0), int64(1), int64(0), int64(1)},
			"Range":        Array(rng),
		}, []byte(prog))
		if f == nil {
			t.Fatalf("program %q did not compile", prog)
		}
		return f.eval(in)
	}
	one := []Object{int64(0), int64(1)}
	// A CMYK tint transform, which is what a Separation actually carries:
	// one input becomes four components.
	cmykRange := []Object{int64(0), int64(1), int64(0), int64(1), int64(0), int64(1), int64(0), int64(1)}
	got := run("{ dup 0.2 mul exch dup 0.4 mul exch dup 0.6 mul exch 0.8 mul }", cmykRange, 0.5)
	want := []float32{0.1, 0.2, 0.3, 0.4}
	if len(got) != 4 {
		t.Fatalf("tint transform returned %v, want four components", got)
	}
	for i := range want {
		if absf(got[i]-want[i]) > 1e-5 {
			t.Errorf("component %d = %v, want %v", i, got[i], want[i])
		}
	}
	// Arithmetic and the transcendentals, in degrees as PostScript uses.
	// The range here is wide, since these are arithmetic results rather
	// than colour components (the clipping is checked on its own below).
	wide := []Object{float64(-1000), float64(1000)}
	for _, tc := range []struct {
		prog string
		in   float32
		want float32
	}{
		{"{ 2 mul 0.1 add }", 0.4, 0.9},
		{"{ pop 90 sin }", 0, 1},
		{"{ pop 0 cos }", 0, 1},
		{"{ pop 2 3 exp }", 0, 8},
		{"{ pop 16 sqrt }", 0, 4},
		{"{ pop 7 2 idiv }", 0, 3},
		{"{ pop 7 2 mod }", 0, 1},
		{"{ pop -3 abs }", 0, 3},
		{"{ pop 2.7 floor }", 0, 2},
		{"{ pop 2.2 ceiling }", 0, 3},
		{"{ pop 2.5 round }", 0, 3},
		{"{ pop 1 0 atan }", 0, 90},
		{"{ pop 100 log }", 0, 2},
		{"{ pop 1 3 bitshift }", 0, 8},
		// Conditionals: the branch that runs decides the output.
		{"{ 0.5 gt { 1 } { 0 } ifelse }", 0.8, 1},
		{"{ 0.5 gt { 1 } { 0 } ifelse }", 0.2, 0},
		{"{ dup 0.5 lt { pop 0.25 } if }", 0.1, 0.25},
		{"{ dup 0.5 lt { pop 0.25 } if }", 0.9, 0.9},
		// Stack operators.
		{"{ pop 1 2 3 3 1 roll pop pop }", 0, 3},
		{"{ pop 1 2 exch pop }", 0, 2}, // exch swaps, so the 2 is left
		{"{ pop 5 dup mul }", 0, 25},
		{"{ pop 9 8 1 index pop pop }", 0, 9},
		{"{ pop true not { 0.5 } { 0.75 } ifelse }", 0, 0.75},
	} {
		got := run(tc.prog, wide, tc.in)
		if len(got) != 1 {
			t.Errorf("%s with %v returned %v, want one value", tc.prog, tc.in, got)
			continue
		}
		if absf(got[0]-tc.want) > 1e-4 {
			t.Errorf("%s with %v = %v, want %v", tc.prog, tc.in, got[0], tc.want)
		}
	}
	// An output beyond its range is clipped, not passed through.
	if got := run("{ pop 5 }", one, 0); len(got) != 1 || got[0] != 1 {
		t.Errorf("out-of-range output = %v, want it clipped to 1", got)
	}
	// A division by zero yields a colour rather than an error.
	if got := run("{ pop 1 0 div }", one, 0); len(got) != 1 || got[0] != 0 {
		t.Errorf("1 0 div = %v, want 0", got)
	}
	// Two inputs, as a DeviceN transform takes.
	rd := &Renderer{doc: &Document{cache: map[int]Object{}}}
	f2 := rd.functionFrom(Dict{
		"FunctionType": int64(4),
		"Domain":       Array{int64(0), int64(1), int64(0), int64(1)},
		"Range":        Array{int64(0), int64(1)},
	}, []byte("{ add 2 div }"))
	if got := f2.eval([]float32{0.2, 0.6}); len(got) != 1 || absf(got[0]-0.4) > 1e-5 {
		t.Errorf("two-input average = %v, want [0.4]", got)
	}
}

// A Separation colour runs its tint transform: full ink through a CMYK
// transform is the colour the transform says, not a grey guess.
func TestSeparationUsesItsTintTransform(t *testing.T) {
	doc := &Document{cache: map[int]Object{}}
	rd := &Renderer{doc: doc, fonts: map[string]*fontRes{}}
	// A Separation whose transform paints pure cyan at full tint.
	tint := &Stream{
		Dict: Dict{
			"FunctionType": int64(4),
			"Domain":       Array{int64(0), int64(1)},
			"Range":        Array{int64(0), int64(1), int64(0), int64(1), int64(0), int64(1), int64(0), int64(1)},
		},
		// The tint itself becomes cyan; magenta, yellow and black are 0.
		Raw: []byte("{ 0 0 0 }"),
		doc: doc,
	}
	cs := rd.colorSpaceObj(Array{
		Name("Separation"), Name("Spot"), Name("DeviceCMYK"), tint,
	}, nil, 0)
	if cs == nil || cs.fn == nil {
		t.Fatal("the Separation did not compile its tint transform")
	}
	// The transform above returns c=tint, m=0, y=0, k=0 for any tint, so
	// full ink is pure cyan: (1-c)*(1-k) = 0 red, full green and blue.
	got, ok := cs.color([]Object{float64(1)})
	if !ok {
		t.Fatal("no colour from the Separation")
	}
	if got.R > 0.01 || got.G < 0.99 || got.B < 0.99 {
		t.Errorf("full tint = %+v, want pure cyan through the transform", got)
	}
}

// A rectangular clip needs no coverage mask — the scissor is exact — while
// anything else does. Getting this wrong is either a wrong picture (no mask
// where one is needed) or a mask render per rectangle, which is most clips
// in most documents.
func TestBoxPathDetection(t *testing.T) {
	box := render.NewPath()
	box.MoveTo(10, 10)
	box.LineTo(60, 10)
	box.LineTo(60, 40)
	box.LineTo(10, 40)
	box.Close()
	if !isBoxPath(box) {
		t.Error("an axis-aligned rectangle wants no mask")
	}

	tri := render.NewPath()
	tri.MoveTo(10, 10)
	tri.LineTo(60, 10)
	tri.LineTo(35, 40)
	tri.Close()
	if isBoxPath(tri) {
		t.Error("a triangle needs a mask")
	}

	two := render.NewPath()
	two.MoveTo(0, 0)
	two.LineTo(10, 0)
	two.LineTo(10, 10)
	two.LineTo(0, 10)
	two.Close()
	two.MoveTo(20, 20)
	two.LineTo(30, 20)
	two.LineTo(30, 30)
	two.LineTo(20, 30)
	two.Close()
	if isBoxPath(two) {
		t.Error("two rectangles are not one rectangle")
	}
}

// Nested clips intersect, so the chain grows outermost-first and a saved
// state keeps its own view of it: `q`, clip, `Q` must not leave the extra
// clip behind.
func TestClipChainNests(t *testing.T) {
	a, b := render.NewPath(), render.NewPath()
	outer := clipPathOf(a, render.NonZero, nil)
	inner := clipPathOf(b, render.EvenOdd, outer)

	if outer.path != a || outer.next != nil {
		t.Fatal("the first clip is the whole chain")
	}
	if inner.path != a || inner.next == nil || inner.next.path != b {
		t.Fatal("the chain runs outermost-first")
	}
	if inner.next.rule != render.EvenOdd {
		t.Error("each clip keeps its own fill rule")
	}
	if outer.next != nil {
		t.Error("appending to a chain must not mutate the shorter one")
	}
}

// /BM names map onto the modes the compositor can actually express, and an
// unknown one is Normal rather than a guess. An ARRAY of names is the
// spec's own compatibility escape: the first known name wins.
func TestBlendModeNames(t *testing.T) {
	rd := &Renderer{doc: &Document{}}
	for _, tc := range []struct {
		obj  Object
		want render.BlendMode
	}{
		{Name("Multiply"), render.BlendMultiply},
		{Name("Screen"), render.BlendScreen},
		{Name("Darken"), render.BlendDarken},
		{Name("Lighten"), render.BlendLighten},
		{Name("Normal"), render.BlendNormal},
		{Name("Luminosity"), render.BlendNormal}, // needs a backdrop read
		{Array{Name("Overlay"), Name("Screen")}, render.BlendScreen},
		{Array{Name("HardLight")}, render.BlendNormal},
		{nil, render.BlendNormal},
	} {
		if got := rd.blendModeOf(tc.obj); got != tc.want {
			t.Errorf("blendModeOf(%v) = %v, want %v", tc.obj, got, tc.want)
		}
	}
}

// An /SMask entry resolves to the form that draws the mask, how its
// coverage is taken, and the backdrop a luminosity mask is measured
// against. /None — the default — is no mask at all.
func TestSoftMaskEntry(t *testing.T) {
	rd := &Renderer{doc: &Document{}}
	form := &Stream{Dict: Dict{
		"Type": Name("XObject"), "Subtype": Name("Form"),
		"Group": Dict{"S": Name("Transparency"), "CS": Name("DeviceGray")},
	}}
	if m := rd.softMaskOf(Name("None"), render.Affine2{}); m != nil {
		t.Error("/None must clear the soft mask")
	}
	if m := rd.softMaskOf(Dict{"S": Name("Luminosity")}, render.Affine2{}); m != nil {
		t.Error("an /SMask with no /G draws nothing, so it is no mask")
	}
	// Luminosity is the default, and its backdrop is black unless /BC says
	// otherwise — which matters: a white backdrop masks nothing away.
	m := rd.softMaskOf(Dict{"G": form}, render.Affine2{A: 2})
	if m == nil || m.form != form {
		t.Fatal("the mask did not resolve to its form")
	}
	if m.kind != render.SoftMaskLuminosity {
		t.Errorf("kind = %v, want luminosity", m.kind)
	}
	if m.backdrop != (render.RGBA{A: 1}) {
		t.Errorf("backdrop = %v, want black", m.backdrop)
	}
	if m.ctm.A != 2 {
		t.Error("the mask must keep the CTM in force when gs ran")
	}
	if bc := rd.softMaskOf(Dict{"G": form, "BC": Array{int64(1)}}, render.Affine2{}); bc == nil ||
		bc.backdrop != (render.RGBA{R: 1, G: 1, B: 1, A: 1}) {
		t.Errorf("/BC [1] in DeviceGray gave %v, want white", bc.backdrop)
	}
	// An alpha mask takes coverage from what the drawing covered, and has
	// no backdrop of its own.
	if a := rd.softMaskOf(Dict{"G": form, "S": Name("Alpha")}, render.Affine2{}); a == nil ||
		a.kind != render.SoftMaskAlpha {
		t.Error("/S /Alpha must select the alpha mask")
	}
}
