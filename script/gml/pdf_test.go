package gml

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// writeTestPDF puts a one-page PDF on disk and returns its path.
func writeTestPDF(t *testing.T) string {
	t.Helper()
	content := "0 0 1 rg 10 10 180 80 re f BT /F1 24 Tf 20 40 Td (Hello) Tj ET"
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
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	// Forward slashes: the path is spliced into a .gml string literal, where
	// a Windows backslash would read as an escape.
	return filepath.ToSlash(path)
}

// A PdfDocument reads the file it is given: status Ready, the real page
// count, and the metadata a page's header binds to.
func TestPdfDocumentLoads(t *testing.T) {
	path := writeTestPDF(t)
	env := NewEnv()
	src := `
Item {
    property string report: doc.status + "/" + doc.pageCount + "/" + doc.errorString
    property real pw: doc.pagePointSize(0).width
    PdfDocument { id: doc; source: "` + path + `" }
}`
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	ui.New(nil, nil, d.View()).Layout(300, 200)
	ui.New(nil, nil, d.View()).Layout(300, 200)
	// PdfDocument.Ready is 2.
	if got := str(env.vars["report"].(Ref).get()); got != "2/1/" {
		t.Errorf("report = %q, want \"2/1/\" (Ready, one page, no error)", got)
	}
	if got := str(env.vars["pw"].(Ref).get()); got != "200" {
		t.Errorf("pagePointSize(0).width = %q, want 200", got)
	}
}

// A source that is not a PDF reports Error with a reason, which is the
// branch a page's drop target shows.
func TestPdfDocumentBadSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not.pdf")
	if err := os.WriteFile(path, []byte("this is not a PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := NewEnv()
	d, err := LoadString(`
Item {
    property string st: doc.status + ""
    property bool hasErr: doc.errorString.length > 0
    PdfDocument { id: doc; source: "`+path+`" }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	ui.New(nil, nil, d.View()).Layout(300, 200)
	ui.New(nil, nil, d.View()).Layout(300, 200)
	if got := str(env.vars["st"].(Ref).get()); got != "4" {
		t.Errorf("status = %q, want 4 (Error)", got)
	}
	if got := str(env.vars["hasErr"].(Ref).get()); got != "true" {
		t.Error("an error status must come with an errorString")
	}
}

// A viewer takes its pages from the document it is pointed at, clamps
// currentPage to the page count, and its navigation methods move it.
func TestPdfViewNavigation(t *testing.T) {
	path := writeTestPDF(t)
	env := NewEnv()
	d, err := LoadString(`
Item {
    width: 300; height: 200
    property string report: view.pageCount + "/" + view.currentPage
    PdfDocument { id: doc; source: "`+path+`" }
    PdfMultiPageView { id: view; anchors.fill: parent; document: doc }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	build := func() { ui.New(nil, nil, d.View()).Layout(300, 200) }
	build()
	build()
	if got := str(env.vars["report"].(Ref).get()); got != "1/0" {
		t.Errorf("report = %q, want \"1/0\"", got)
	}
	obj, ok := env.vars["view"].(*elemObject)
	if !ok {
		t.Fatalf("view is %T", env.vars["view"])
	}
	next, ok := obj.methods["goToNextPage"].(jsFunc)
	if !ok {
		t.Fatal("the viewer has no goToNextPage()")
	}
	if _, err := next(nil); err != nil {
		t.Fatalf("goToNextPage: %v", err)
	}
	build()
	// One page: the page number cannot run past the end.
	if got := str(env.vars["report"].(Ref).get()); got != "1/0" {
		t.Errorf("after goToNextPage on a one-page document: %q, want \"1/0\"", got)
	}
}
