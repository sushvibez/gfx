package gml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestFileDialogOpens: open() shows the dialog (which needs a property
// write, or the document's build cache would keep the closed tree), the
// panel lists the directory, and the id exposes the QML contract.
func TestFileDialogOpens(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := `
Column {
    property string picked: ""
    FileDialog {
        id: dlg
        title: "Open"
        nameFilters: ["Go files (*.go)"]
        currentFolder: "file://` + dir + `"
        onAccepted: picked = dlg.selectedFile
    }
    Text { text: "picked " + picked; color: "#ffffff" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	d.View()()
	st := d.doc.fileDialogs[d.doc.key(findNode(d.root, "FileDialog"))]
	if st == nil {
		t.Fatal("no dialog state")
	}
	if st.isOpen() {
		t.Fatal("dialog starts open")
	}
	// open() through the id, the way markup calls it.
	obj := d.doc.elemObjs.get(d.doc.intern("dlg"))
	if obj == nil || obj.methods["open"] == nil {
		t.Fatal("dlg.open() not exposed")
	}
	if _, err := obj.methods["open"].(jsFunc)(nil); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !st.isOpen() {
		t.Fatal("open() did not open the dialog")
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view while open")
	}
	// Picking a file publishes it as selectedFile, in URL form.
	st.pick(filepath.Join(dir, "b.go"), false)
	d.View()()
	got, _ := d.doc.elemObjs.get(d.doc.intern("dlg")).member("selectedFile")
	if want := "file://" + filepath.ToSlash(filepath.Join(dir, "b.go")); str(got) != want {
		t.Fatalf("selectedFile = %q, want %q", got, want)
	}
}

// TestFileDialogNameFilters: QML name filters ("PDF files (*.pdf)") select
// what the listing shows (the matching itself lives in ui, with the
// browser widget).
func TestFileDialogNameFilters(t *testing.T) {
	cases := []struct {
		name    string
		filters []string
		want    bool
	}{
		{"report.pdf", []string{"PDF files (*.pdf)"}, true},
		{"report.txt", []string{"PDF files (*.pdf)"}, false},
		{"report.txt", []string{"PDF files (*.pdf)", "All files (*)"}, true},
		{"anything", nil, true},
		{"a.tar.gz", []string{"Archives (*.gz *.zip)"}, true},
	}
	for _, c := range cases {
		if got := ui.MatchesNameFilters(c.name, c.filters); got != c.want {
			t.Errorf("MatchesNameFilters(%q, %v) = %v, want %v", c.name, c.filters, got, c.want)
		}
	}
}
