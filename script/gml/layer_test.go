package gml

import "testing"

// TestCapturedLayerHasItsOwnState: the same markup is built a SECOND time
// inside an effect's capture, in that effect's coordinate space. Sharing one
// layer state between the two made them overwrite each other's texture —
// the capture's content ended up composited at the page-space rect, which
// drew a hard rectangular edge around every blurred blob on the lens-tabs
// hero.
func TestCapturedLayerHasItsOwnState(t *testing.T) {
	doc := newDocState()
	page := doc.layerFor("k|layer")
	doc.capturing = map[string]bool{"bg": true}
	captured := doc.layerFor("k|layer|cap")
	if page == captured {
		t.Fatal("the captured copy shares the page's layer state")
	}
	delete(doc.capturing, "bg")
	if again := doc.layerFor("k|layer"); again != page {
		t.Error("the page's own state was not reused")
	}
}
