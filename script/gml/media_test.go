package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestMediaPlayerContract: a MediaPlayer is a real state machine on its id —
// and an honest one: with no decoder in this build, a source lands on
// InvalidMedia and play() leaves it stopped, which is the branch these pages
// are written to handle.
func TestMediaPlayerContract(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    MediaPlayer {
        id: player
        source: "https://example.invalid/clip.mp4"
        audioOutput: AudioOutput { volume: 0.4 }
    }
    property int status: player.mediaStatus
    property int playback: player.playbackState
    property real vol: player.volume
    VideoOutput { width: 160; height: 90 }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	u.Layout(200, 200)

	if got, _ := toFloat64(d.doc.props["status"].get()); got != mediaInvalidMedia {
		t.Errorf("mediaStatus = %v, want InvalidMedia (%v) — no decoder in this build",
			got, mediaInvalidMedia)
	}
	if got, _ := toFloat64(d.doc.props["vol"].get()); got < 0.39 || got > 0.41 {
		t.Errorf("volume = %v, want 0.4 from the AudioOutput", got)
	}
	obj := d.doc.elemObjs.get(d.doc.intern("player"))
	if obj == nil {
		t.Fatal("no elemObject for the player")
	}
	play, ok := obj.methods["play"].(jsFunc)
	if !ok {
		t.Fatal("player.play() is not callable")
	}
	if _, err := play(nil); err != nil {
		t.Fatal(err)
	}
	u.Layout(200, 200)
	if got, _ := toFloat64(d.doc.props["playback"].get()); got != mediaStoppedState {
		t.Errorf("playbackState = %v after play() on unplayable media, want StoppedState", got)
	}

	// position is writable — a seek slider assigns it.
	ref, ok := obj.props["position"].(Ref)
	if !ok {
		t.Fatalf("player.position = %T, want a Ref", obj.props["position"])
	}
	if err := ref.set(1500); err != nil {
		t.Fatal(err)
	}
	if got, _ := toFloat64(ref.get()); got != 1500 {
		t.Errorf("position = %v after seeking to 1500", got)
	}
}

// TestMediaDevicesEmpty: MediaDevices reports no hardware, so a camera page
// takes its "no camera" branch instead of failing.
func TestMediaDevicesEmpty(t *testing.T) {
	src := `
Item {
    width: 100; height: 100
    MediaDevices { id: devices }
    property int cameras: devices.videoInputs.length
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	u.Layout(100, 100)
	if got, _ := toFloat64(d.doc.props["cameras"].get()); got != 0 {
		t.Errorf("videoInputs.length = %v, want 0", got)
	}
}

// TestPdfDocumentStatus: a PdfDocument is a real object with a writable
// source — and honest: this build has no PDF renderer, so a source lands on
// Error and the page takes its "drop a file here" branch.
func TestPdfDocumentStatus(t *testing.T) {
	src := `
Item {
    width: 300; height: 200
    PdfDocument { id: pdf }
    property int status: pdf.status
    property int pages: pdf.pageCount
    PdfMultiPageView { id: view; width: 300; height: 150 }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	u.Layout(300, 200)
	if got, _ := toFloat64(d.doc.props["status"].get()); got != pdfNull {
		t.Errorf("status with no source = %v, want Null", got)
	}
	obj := d.doc.elemObjs.get(d.doc.intern("pdf"))
	if obj == nil {
		t.Fatal("no elemObject for the document")
	}
	ref, ok := obj.props["source"].(Ref)
	if !ok {
		t.Fatalf("source = %T, want a Ref", obj.props["source"])
	}
	if err := ref.set("file:///tmp/book.pdf"); err != nil {
		t.Fatal(err)
	}
	// One frame refreshes the document, the next re-evaluates the binding
	// that reads its status.
	u.Layout(300, 200)
	u.Layout(300, 200)
	if got, _ := toFloat64(d.doc.props["status"].get()); got != pdfError {
		t.Errorf("status after a source = %v, want Error (no renderer here)", got)
	}
	if got, _ := toFloat64(d.doc.props["pages"].get()); got != 0 {
		t.Errorf("pageCount = %v, want 0", got)
	}
	// The viewer's page navigation is real state its toolbar drives.
	view := d.doc.elemObjs.get(d.doc.intern("view"))
	if view == nil {
		t.Fatal("no elemObject for the viewer")
	}
	next, ok := view.methods["goToNextPage"].(jsFunc)
	if !ok {
		t.Fatal("goToNextPage is not callable")
	}
	if _, err := next(nil); err != nil {
		t.Fatal(err)
	}
	page, _ := view.props["currentPage"].(Ref)
	if got, _ := toFloat64(page.get()); got != 1 {
		t.Errorf("currentPage = %v after goToNextPage, want 1", got)
	}
}
