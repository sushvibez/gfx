package gml

import (
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestChart3DProxies: the ItemModel*DataProxy roles resolve against their
// ListModel, categorical row/column labels become grid indices, and a proxy
// re-reads its model, so rows a handler appends after the first build are
// plotted.
func TestChart3DProxies(t *testing.T) {
	src := `
Item {
    width: 400; height: 300
    ListModel {
        id: sales
        ListElement { quarter: "Q1"; region: "North"; sales: 10 }
        ListElement { quarter: "Q1"; region: "South"; sales: 20 }
        ListElement { quarter: "Q2"; region: "North"; sales: 30 }
    }
    ListModel { id: cloud }
    function fill() {
        cloud.clear()
        for (var i = 0; i < 5; i++)
            cloud.append({ "xv": i, "yv": i * 2, "zv": -i })
    }
    Component.onCompleted: fill()
    Bars3D {
        id: bars
        width: 200; height: 150
        valueAxis.min: 0
        valueAxis.max: 40
        Bar3DSeries {
            baseColor: "#00d1a9"
            ItemModelBarDataProxy {
                itemModel: sales
                rowRole: "quarter"
                columnRole: "region"
                valueRole: "sales"
            }
        }
    }
    Scatter3D {
        id: dots
        width: 200; height: 150
        Scatter3DSeries {
            ItemModelScatterDataProxy {
                itemModel: cloud
                xPosRole: "xv"
                yPosRole: "yv"
                zPosRole: "zv"
            }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(400, 300)
	u.Layout(400, 300) // onCompleted's rows land on a later frame

	proxy := func(elem string) proxy3D {
		n := findNode(d.root, elem)
		if n == nil {
			t.Fatalf("no %s node", elem)
		}
		for _, series := range n.children {
			for _, sub := range series.children {
				if p, ok := readProxy(sub, d.env); ok {
					return p
				}
			}
		}
		t.Fatalf("%s has no data proxy", elem)
		return proxy3D{}
	}

	bars := proxy("Bars3D").cells()
	if len(bars) != 3 {
		t.Fatalf("bars: got %d cells, want 3", len(bars))
	}
	// Q1/North, Q1/South, Q2/North → rows 0,0,1 and columns 0,1,0.
	want := []cell3D{{row: 0, col: 0, y: 10}, {row: 0, col: 1, y: 20}, {row: 1, col: 0, y: 30}}
	for i, w := range want {
		got := bars[i]
		if got.row != w.row || got.col != w.col || got.y != w.y {
			t.Errorf("bar %d: got row=%d col=%d y=%v, want row=%d col=%d y=%v",
				i, got.row, got.col, got.y, w.row, w.col, w.y)
		}
	}

	dots := proxy("Scatter3D").cells()
	if len(dots) != 5 {
		t.Fatalf("scatter: got %d cells, want 5 (appended rows must be re-read)", len(dots))
	}
	if dots[3].x != 3 || dots[3].y != 6 || dots[3].z != -3 {
		t.Errorf("scatter[3] = (%v,%v,%v), want (3,6,-3)", dots[3].x, dots[3].y, dots[3].z)
	}
}

// TestListModelSurvivesRebuild: a ListModel declared inside a file component
// is re-declared every time that component rebuilds; rows appended at
// runtime must survive, and declared ListElements must not be duplicated.
func TestListModelSurvivesRebuild(t *testing.T) {
	src := `
Item {
    width: 100; height: 100
    ListModel {
        id: m
        ListElement { a: 1 }
    }
    Component.onCompleted: m.append({ "a": 2 })
    Text { text: "count " + m.count }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(100, 100)
	u.Layout(100, 100) // onCompleted runs once the tree has geometry
	if err := declareProps(d.root, d.env, d.doc); err != nil {
		t.Fatalf("re-declare: %v", err)
	}
	m := d.doc.models[d.doc.rowPrefix+"m"]
	if m == nil {
		t.Fatal("no model registered")
	}
	if len(m.rows) != 2 {
		t.Fatalf("after re-declaring: %d rows, want 2 (1 declared + 1 appended)", len(m.rows))
	}
}
