package gml

import (
	"strings"
	"testing"

	"github.com/chewxy/math32"
	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/physics/p3"
)

const richDoc = `
Column {
    property int count: 0
    property string label: "hi \"there\""
    signal fired(x, y)

    Text { text: count > 3 ? "many" : ` + "`n=${count}`" + `; color: "#ffffff" }
    Rectangle {
        width: (count ?? 1) * 20
        height: -(count + 2)
        gradient: Gradient {
            GradientStop { position: 0.0; color: "#ff0000" }
            GradientStop { position: 1.0; color: "#0000ff" }
        }
        onClicked: count = count + 1
    }
    Button {
        text: "go"
        onClicked: {
            var xs = [1, 2, 3].map((v) => v * 2)
            for (var x of xs) { console.log(x) }
            if (count > 0) { fired(count, xs.length) } else { count = 1 }
        }
    }
    function total(items) {
        var sum = 0
        for (var i = 0; i < items.length; i = i + 1) { sum = sum + items[i] }
        return sum
    }
}`

func TestFormatIdempotentAndReparses(t *testing.T) {
	once, err := Format(richDoc)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatalf("Format(Format(x)) failed to parse:\n%s\nerr: %v", once, err)
	}
	if once != twice {
		t.Fatalf("Format not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
	// The formatted document still builds as a ui doc.
	env := NewEnv()
	if _, err := LoadString(once, env); err != nil {
		t.Fatalf("formatted doc failed to load: %v", err)
	}
}

// TestWriterParenthesization checks precedence-sensitive expressions keep
// their meaning through a print+parse round trip.
func TestWriterParenthesization(t *testing.T) {
	env := NewEnv()
	env.vars["a"] = 2.0
	env.vars["b"] = 3.0
	cases := []string{
		"(a + b) * 2",
		"a + b * 2",
		"-(a + b)",
		"-a + b",
		"(a ?? b) * 2",
		"a > 1 ? b : a * 5",
		"(a > 1 ? b : a) * 5",
		"a - (b - 1)",
		"a - b - 1",
		"a / (b * 2)",
	}
	for _, src := range cases {
		e1, err := parseExprSrc(src)
		if err != nil {
			t.Fatalf("%s: parse: %v", src, err)
		}
		printed := exprString(e1, 0)
		e2, err := parseExprSrc(printed)
		if err != nil {
			t.Fatalf("%s -> %s: reparse: %v", src, printed, err)
		}
		v1, err1 := e1.eval(env)
		v2, err2 := e2.eval(env)
		if err1 != nil || err2 != nil {
			t.Fatalf("%s: eval errs %v / %v", src, err1, err2)
		}
		if v1 != v2 {
			t.Fatalf("%s -> %s: value changed %v -> %v", src, printed, v1, v2)
		}
	}
}

// TestExponentLiterals covers the lexer's JS exponent support (found by the
// corpus test: %g output like 1.718e+09 must re-parse) and the writer's
// plain-notation preference.
func TestExponentLiterals(t *testing.T) {
	env := NewEnv()
	for src, want := range map[string]float64{
		"1.7e2": 170, "2E+3": 2000, "5e-1": 0.5, "1.718e+09": 1.718e9,
	} {
		e, err := parseExprSrc(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		v, err := e.eval(env)
		if err != nil || v != want {
			t.Fatalf("%s = %v (%v), want %v", src, v, err, want)
		}
	}
	// Writer prints big integers plainly, and they round-trip.
	if s := litString(1.718e9); s != "1718000000" {
		t.Fatalf("litString(1.718e9) = %s", s)
	}
	if s := litString(1e30); s != "1e+30" { // extreme: %g fallback
		t.Fatalf("litString(1e30) = %s", s)
	}
	if _, err := parseExprSrc(litString(1e30)); err != nil {
		t.Fatalf("extreme literal does not re-parse: %v", err)
	}
}

func TestFormatSceneSemanticsPreserved(t *testing.T) {
	formatted, err := Format(testScene)
	if err != nil {
		t.Fatalf("Format(scene): %v", err)
	}
	v1, err := LoadScene(testScene, NewEnv())
	if err != nil {
		t.Fatal(err)
	}
	v2, err := LoadScene(formatted, NewEnv())
	if err != nil {
		t.Fatalf("formatted scene failed to load:\n%s\nerr: %v", formatted, err)
	}
	f1, _ := v1.Frame()
	f2, _ := v2.Frame()
	if len(f1.Draws) != len(f2.Draws) {
		t.Fatalf("draw count changed: %d -> %d", len(f1.Draws), len(f2.Draws))
	}
	for i := range f1.Draws {
		if f1.Draws[i].Transform != f2.Draws[i].Transform || f1.Draws[i].Color != f2.Draws[i].Color {
			t.Fatalf("draw %d changed", i)
		}
	}
	// Physics preserved: same ray behavior.
	h1, ok1 := v1.World().RayCast(core.V3(1, 5, 1), core.V3(0, -1, 0), 100, p3.LayerAll)
	h2, ok2 := v2.World().RayCast(core.V3(1, 5, 1), core.V3(0, -1, 0), 100, p3.LayerAll)
	if ok1 != ok2 || math32.Abs(h1.T-h2.T) > 1e-4 {
		t.Fatal("physics changed through the round trip")
	}
}

func TestBuilderProducesLoadableScene(t *testing.T) {
	b := NewBuilder("Scene3D")
	b.Attr("lightDir", []float64{-1, -1, 0})
	b.Prop("real", "doorOpen", 0)

	floor := b.Child("Box")
	floor.Attr("size", []float64{20, 0.5, 20}).Attr("position", []float64{0, -0.25, 0}).Attr("color", "#404448")

	door := b.Child("Box").ID("door")
	door.Attr("size", []float64{4, 2.5, 0.5}).Attr("color", "#994b47")
	door.AttrExpr("position", "[0, 1.25 + doorOpen * 2.4, 8]")

	b.Child("Trigger").ID("plate").Attr("size", []float64{2, 1, 2}).Attr("position", []float64{0, 0.5, 4})

	src, err := b.String()
	if err != nil {
		t.Fatalf("Builder: %v", err)
	}
	// The built document is stable under Format.
	if formatted, err := Format(src); err != nil || formatted != src {
		t.Fatalf("builder output not canonical:\n%s\nvs\n%s\nerr=%v", src, formatted, err)
	}
	// And it loads with a working binding.
	env := NewEnv()
	v, err := LoadScene(src, env)
	if err != nil {
		t.Fatalf("built scene failed to load:\n%s\nerr: %v", src, err)
	}
	doorID, ok := v.Body("door")
	if !ok {
		t.Fatal("door not resolvable")
	}
	_ = env.vars["doorOpen"].(Ref).set(1.0)
	if _, err := v.Frame(); err != nil {
		t.Fatal(err)
	}
	p, _, _ := v.World().Pose(doorID)
	if math32.Abs(p.Y-3.65) > 1e-3 {
		t.Fatalf("built binding broken: door y = %v, want 3.65", p.Y)
	}
	// Bad expression surfaces via String.
	bad := NewBuilder("Scene3D")
	bad.AttrExpr("position", "[1, ,")
	if _, err := bad.String(); err == nil {
		t.Fatal("bad AttrExpr not reported")
	}
	// Sanity: builder output looks like gml.
	if !strings.Contains(src, "property real doorOpen: 0") {
		t.Fatalf("missing property decl:\n%s", src)
	}
}
