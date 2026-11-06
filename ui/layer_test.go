package ui

import "testing"

// A layer the UI stops painting is released after layerIdleFrames frames
// (its texture freed, its content marked stale) and forgotten; one that
// keeps being painted is kept.
func TestIdleLayersAreReleased(t *testing.T) {
	u := &UI{}
	kept, dropped := NewLayerState(), NewLayerState()
	kept.ready, dropped.ready = true, true
	u.ctx.layers = []*LayerEl{{st: kept}, {st: dropped}}
	u.sweepLayers()
	u.ctx.layers = []*LayerEl{{st: kept}}
	for range layerIdleFrames {
		u.sweepLayers()
	}
	if !dropped.ready {
		t.Fatal("released before the idle limit")
	}
	u.sweepLayers()
	if dropped.ready {
		t.Error("an idle layer was not released")
	}
	if _, ok := u.layerSeen[dropped]; ok {
		t.Error("a released layer is still tracked")
	}
	if !kept.ready {
		t.Error("a layer painted every frame was released")
	}
	u.ReleaseLayers()
	if kept.ready || len(u.layerSeen) != 0 {
		t.Error("ReleaseLayers left a layer")
	}
}
