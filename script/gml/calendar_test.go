package gml

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/ui"
)

// TestTumblerWindow: the wheel shows visibleItemCount slots centred on the
// current index, wraps around the model when it wraps, and leaves the slots
// past the ends empty when it does not.
func TestTumblerWindow(t *testing.T) {
	got := tumblerWindow(7, 12, 3, true)
	want := []wheelSlot{{6, -1}, {7, 0}, {8, 1}}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("slot %d = %v, want %v", i, got[i], w)
		}
	}
	// Wrapping past the start: index 0 shows the last item above the first.
	if wrapped := tumblerWindow(0, 12, 3, true); wrapped[0].index != 11 {
		t.Errorf("wrapped slot above 0 = %d, want 11", wrapped[0].index)
	}
	// Not wrapping: that slot is empty instead.
	if fixed := tumblerWindow(0, 12, 3, false); fixed[0].index != -1 {
		t.Errorf("non-wrapping slot above 0 = %d, want -1 (empty)", fixed[0].index)
	}
	// A five-item window reaches two steps out either way.
	if wide := tumblerWindow(5, 12, 5, false); len(wide) != 5 ||
		wide[0].index != 3 || wide[4].index != 7 || wide[4].displacement != 2 {
		t.Errorf("five-slot window = %v", wide)
	}
}

// TestTumblerState: a Tumbler's currentIndex is engine state — readable by a
// sibling binding and writable from a handler — and an integer model gives
// the delegate the index as its modelData.
func TestTumblerState(t *testing.T) {
	src := `
Item {
    width: 200; height: 200
    Tumbler {
        id: wheel
        model: 12
        visibleItemCount: 3
        delegate: Text { required property int modelData; text: "m" + modelData }
    }
    Component.onCompleted: wheel.currentIndex = 7
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(200, 200)
	u.Layout(200, 200) // onCompleted's write lands on the next frame

	obj := d.doc.elemObjs.get(d.doc.intern("wheel"))
	if obj == nil {
		t.Fatal("no elemObject for the wheel's id")
	}
	ref, ok := obj.props["currentIndex"].(Ref)
	if !ok {
		t.Fatalf("wheel.currentIndex = %T, want a Ref", obj.props["currentIndex"])
	}
	if got := refInt(ref); got != 7 {
		t.Errorf("currentIndex = %d after the handler wrote 7", got)
	}
	if got, _ := toFloat64(obj.props["count"]); got != 12 {
		t.Errorf("wheel.count = %v, want 12", got)
	}
}

// TestMonthCells: the grid is six weeks starting on the Sunday of the week
// containing the 1st, and days from the adjacent months keep their own month.
func TestMonthCells(t *testing.T) {
	// August 2026 starts on a Saturday, so the grid opens with July 26.
	today := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	cells := monthCells(7, 2026, today)
	if len(cells) != 42 {
		t.Fatalf("got %d cells, want 42 (6 weeks x 7 days)", len(cells))
	}
	check := func(i int, day, month float64) {
		if cells[i]["day"] != day || cells[i]["month"] != month {
			t.Errorf("cell %d = day %v month %v, want day %v month %v",
				i, cells[i]["day"], cells[i]["month"], day, month)
		}
	}
	check(0, 26, 6) // July 26 — the Sunday before
	check(6, 1, 7)  // Saturday, August 1
	check(41, 5, 8) // September 5 — the trailing week
	todayCount := 0
	for i, c := range cells {
		if c["today"] == true {
			todayCount++
			if c["day"] != float64(26) {
				t.Errorf("cell %d marked today but is day %v", i, c["day"])
			}
		}
	}
	if todayCount != 1 {
		t.Errorf("%d cells marked today, want exactly 1", todayCount)
	}
}

// TestDayOfWeekCells: the header names its seven days, Sunday first.
func TestDayOfWeekCells(t *testing.T) {
	cells := dayOfWeekCells()
	if len(cells) != 7 {
		t.Fatalf("got %d cells, want 7", len(cells))
	}
	if cells[0]["shortName"] != "Sun" || cells[6]["shortName"] != "Sat" {
		t.Errorf("names run %v..%v, want Sun..Sat",
			cells[0]["shortName"], cells[6]["shortName"])
	}
	if cells[1]["longName"] != "Monday" || cells[1]["narrowName"] != "M" {
		t.Errorf("Monday = %v / %v", cells[1]["longName"], cells[1]["narrowName"])
	}
}

// TestUseSiteAttrFreeze: a plain use-site attribute is evaluated in the USE
// SITE's scope — the scope QML compiles it in — because the component file it
// lands on may declare ids of its own (a style's `id: root`) that would
// otherwise shadow the caller's. Values freeze; anything object-valued
// (anchors.fill: parent) keeps its expression for the instance scope.
func TestUseSiteAttrFreeze(t *testing.T) {
	env := NewEnv()
	env.vars["root"] = &elemObject{props: map[string]any{"year": float64(2026)}}
	env.vars["parent"] = &elemObject{}

	text := parseAttrExpr(t, `"year " + root.year`, "text")
	frozen := useSiteAttr(text, env)
	lit, ok := frozen.expr.(*litExpr)
	if !ok {
		t.Fatalf("text kept its expression (%T); the component scope would reshadow root", frozen.expr)
	}
	if got := str(lit.v); got != "year 2026" {
		t.Errorf("frozen text = %q, want %q", got, "year 2026")
	}

	// Object-valued: must NOT freeze — anchors resolve against the instance.
	anchors := parseAttrExpr(t, `parent`, "anchors.fill")
	if _, frozen := useSiteAttr(anchors, env).expr.(*litExpr); frozen {
		t.Error("anchors.fill: parent was frozen to a value")
	}

	// Unresolvable out here: keep the expression and let the instance try.
	unknown := parseAttrExpr(t, `styleRoot.width`, "width")
	if _, frozen := useSiteAttr(unknown, env).expr.(*litExpr); frozen {
		t.Error("an unresolved use-site expression was frozen")
	}
}

// parseAttrExpr parses one attribute value expression.
func parseAttrExpr(t *testing.T, src, name string) attr {
	t.Helper()
	doc, err := Parse("Item { " + name + ": " + src + " }")
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	for _, a := range doc.attrs {
		if a.name == name {
			return a
		}
	}
	t.Fatalf("no %q attribute parsed", name)
	return attr{}
}
