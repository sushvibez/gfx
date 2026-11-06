package gml

import "testing"

// TestBindingElement: `Binding { target; property; value; when }` writes the
// target's property while its condition holds, and stops when it doesn't.
// (The target must be a real property — geometry is layout-owned here, so a
// Binding onto `width` has nothing to write.)
func TestBindingElement(t *testing.T) {
	src := `
Column {
    property int src: 7
    property bool on: true
    Slider { id: sl; from: 0; to: 10; value: 1 }
    Binding { target: sl; property: "value"; value: src; when: on }
    property string out: "" + sl.value
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	d.View()()
	d.View()()
	if got := str(d.doc.props["out"].get()); got != "7" {
		t.Fatalf("bound value = %q, want 7", got)
	}
	// With `when` false the binding stops writing: a new source value is
	// not pushed through.
	_ = d.doc.props["on"].set(false)
	_ = d.doc.props["src"].set(9)
	d.View()()
	d.View()()
	if got := str(d.doc.props["out"].get()); got != "7" {
		t.Fatalf("value after when=false = %q, want it to stay 7", got)
	}
}

// TestFrameAnimation: the handler runs once per frame tick with frameTime
// and elapsedTime in scope.
func TestFrameAnimation(t *testing.T) {
	src := `
Column {
    property int ticks: 0
    property real seen: 0
    FrameAnimation {
        running: true
        onTriggered: { ticks = ticks + 1; seen = elapsedTime }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	d.View()() // registers the animation
	// Two ticks of 16ms each, driven deterministically.
	d.doc.advanceFrameAnims(16)
	d.doc.advanceFrameAnims(16)
	if v, _ := toInt(d.doc.props["ticks"].get()); v != 2 {
		t.Fatalf("ticks = %v, want 2", v)
	}
	if f, _ := toFloat64(d.doc.props["seen"].get()); f < 0.03 || f > 0.04 {
		t.Fatalf("elapsedTime = %v, want ~0.032", f)
	}
	// A stopped animation doesn't fire.
	_ = d.doc.props["ticks"].set(0)
	d.doc.frameAnims = map[string]*frameAnim{}
	src2 := `Column { property int ticks: 0
        FrameAnimation { running: false; onTriggered: ticks = ticks + 1 } }`
	d2, err := LoadString(src2, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	d2.View()()
	d2.doc.advanceFrameAnims(16)
	if v, _ := toInt(d2.doc.props["ticks"].get()); v != 0 {
		t.Fatalf("stopped animation ticked %v times", v)
	}
}

// TestDropAreaBuilds: a DropArea renders its children and accepts a drop.
func TestDropAreaBuilds(t *testing.T) {
	src := `
Column {
    property string dropped: ""
    DropArea {
        width: 100; height: 40
        onDropped: dropped = drop.text
        Rectangle { width: 100; height: 40; color: "#223344" }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
}

// TestArrayMutators: JS array methods that mutate write back through the
// receiver — our arrays are Go slices, so `arr.push(x)` has to reassign the
// variable or property the array came from.
func TestArrayMutators(t *testing.T) {
	src := `
Column {
    property string out: run()
    function run() {
        var a = []
        a.push("x")
        a.push("y", "z")
        var popped = a.pop()
        a.unshift("w")
        var shifted = a.shift()
        var removed = a.splice(1, 1, "q")
        return a.join("|") + " pop=" + popped + " shift=" + shifted +
               " removed=" + removed.join(",") + " len=" + a.length
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	want := "x|q pop=z shift=w removed=y len=2"
	if got := str(d.doc.props["out"].get()); got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}

// TestArrayMutatorOnProperty: the write-back also lands on a `property var`
// holding the array, so bindings that read it re-evaluate.
func TestArrayMutatorOnProperty(t *testing.T) {
	src := `
Column {
    property var items: []
    property string out: items.length + ":" + items.join(",")
    function add(v) { items.push(v) }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	d.View()()
	fn, ok := d.env.Get("add")
	if !ok {
		t.Fatal("add() not registered")
	}
	if _, err := callValue(fn, []expr{&litExpr{"one"}}, d.env, 0, "add"); err != nil {
		t.Fatalf("add: %v", err)
	}
	d.View()()
	if got := str(d.doc.props["out"].get()); got != "1:one" {
		t.Fatalf("out = %q, want 1:one", got)
	}
}

// TestNullComparisons: comparing against null/undefined must not coerce —
// `model.active !== undefined` asks whether the role is set, and reading
// nil as a number would fail the whole binding.
func TestNullComparisons(t *testing.T) {
	src := `
Column {
    property var row: ({ "a": 1 })
    property string out: (row.a !== undefined) + "," + (row.b !== undefined) +
        "," + (row.b === undefined) + "," + (row.b === null) + "," + (row.a === null)
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	if got := str(d.doc.props["out"].get()); got != "true,false,true,true,false" {
		t.Fatalf("out = %q, want true,false,true,true,false", got)
	}
}

// TestDateObject: `new Date()` is an object with the JS getters, and
// Qt.formatTime accepts it (as well as bare epoch milliseconds).
func TestDateObject(t *testing.T) {
	src := `
Column {
    property string out: build()
    function build() {
        var d = new Date(86400000)
        return d.getFullYear() + "|" + d.getMonth() + "|" + d.getDate() +
               "|" + Qt.formatDateTime(d, "yyyy-MM-dd")
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	// 86400000 ms after the epoch is 1970-01-02 (JS months are 0-based).
	if got := str(d.doc.props["out"].get()); got != "1970|0|2|1970-01-02" {
		t.Fatalf("out = %q, want 1970|0|2|1970-01-02", got)
	}
}
