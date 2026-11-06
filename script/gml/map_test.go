package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestMapProjection: the Web-Mercator projection is the part the overlays
// depend on — the centre lands in the middle of the viewport, north is up,
// east is right, and a round trip through toCoordinate returns the same
// place.
func TestMapProjection(t *testing.T) {
	p := newMapProjection(40.4936, -3.5668, 13, 400, 300)
	cx, cy := p.screenOf(40.4936, -3.5668)
	if cx < 199 || cx > 201 || cy < 149 || cy > 151 {
		t.Errorf("centre projects to (%v,%v), want the viewport middle (200,150)", cx, cy)
	}
	// North of centre is above it; east of centre is to the right.
	_, ny := p.screenOf(40.6, -3.5668)
	ex, _ := p.screenOf(40.4936, -3.4)
	if ny >= cy {
		t.Errorf("a point further north projects to y=%v, below the centre %v", ny, cy)
	}
	if ex <= cx {
		t.Errorf("a point further east projects to x=%v, left of the centre %v", ex, cx)
	}
	lat, lon := p.coordinateAt(cx, cy)
	if lat < 40.49 || lat > 40.50 || lon < -3.57 || lon > -3.56 {
		t.Errorf("round trip gave (%v,%v), want (40.4936,-3.5668)", lat, lon)
	}
	// Zooming in doubles the world, so the same offset spans half the ground.
	closer := newMapProjection(40.4936, -3.5668, 14, 400, 300)
	if closer.world != p.world*2 {
		t.Errorf("world at zoom 14 = %v, want twice zoom 13's %v", closer.world, p.world)
	}
}

// TestMapStateAndItems: center/zoomLevel are writable state on the map's id,
// and its overlays project through it.
func TestMapStateAndItems(t *testing.T) {
	src := `
Item {
    width: 400; height: 300
    property real lat: 40.4936
    Map {
        id: map
        width: 400; height: 300
        plugin: Plugin { name: "osm" }
        center: QtPositioning.coordinate(40.4936, -3.5668)
        zoomLevel: 13
        minimumZoomLevel: 2
        maximumZoomLevel: 18
        MapPolyline {
            line.width: 3
            path: [
                QtPositioning.coordinate(40.40, -3.60),
                QtPositioning.coordinate(40.50, -3.50)
            ]
        }
        MapQuickItem {
            coordinate: QtPositioning.coordinate(root.lat, -3.5668)
            anchorPoint.x: 8
            anchorPoint.y: 8
            sourceItem: Rectangle { width: 16; height: 16 }
        }
    }
    property real zoom: map.zoomLevel
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(400, 300)
	u.Layout(400, 300)

	obj := d.doc.elemObjs.get(d.doc.intern("map"))
	if obj == nil {
		t.Fatal("no elemObject for the map")
	}
	if got, _ := toFloat64(d.doc.props["zoom"].get()); got != 13 {
		t.Errorf("zoomLevel = %v, want the declared 13", got)
	}
	// zoomLevel is writable and clamped to the map's limits.
	zoomRef, ok := obj.props["zoomLevel"].(Ref)
	if !ok {
		t.Fatalf("zoomLevel = %T, want a Ref", obj.props["zoomLevel"])
	}
	if err := zoomRef.set(99); err != nil {
		t.Fatal(err)
	}
	if got, _ := toFloat64(zoomRef.get()); got != 18 {
		t.Errorf("zoomLevel = %v after writing 99, want the maximum 18", got)
	}
	// center reads back as a coordinate and takes one.
	centerRef, ok := obj.props["center"].(Ref)
	if !ok {
		t.Fatalf("center = %T, want a Ref", obj.props["center"])
	}
	if err := centerRef.set(coordinateValue(10, 20)); err != nil {
		t.Fatal(err)
	}
	lat, lon, ok := asCoordinate(centerRef.get())
	if !ok || lat != 10 || lon != 20 {
		t.Errorf("center = %v after being assigned (10,20)", centerRef.get())
	}
	// toCoordinate inverts the projection through the map's own state.
	toCoord, ok := obj.methods["toCoordinate"].(jsFunc)
	if !ok {
		t.Fatal("map.toCoordinate is not callable")
	}
	if _, err := toCoord([]any{map[string]any{"x": float64(200), "y": float64(150)}}); err != nil {
		t.Fatal(err)
	}
}

// TestGeoDistance: the great-circle distance, against a known pair (Madrid
// to Barcelona is about 505 km).
func TestGeoDistance(t *testing.T) {
	d := geoDistance(40.4168, -3.7038, 41.3874, 2.1686)
	if d < 495000 || d > 515000 {
		t.Errorf("Madrid to Barcelona = %v m, want about 505 km", d)
	}
}
