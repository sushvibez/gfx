package gml

import "testing"

// TestTableEditDelegate: with the edit cell set, the TableView builds its
// TableView.editDelegate over that cell (creating the cell's `edit`
// property), and committing writes the edited value into the model.
func TestTableEditDelegate(t *testing.T) {
	src := `
Column {
    TableModel {
        id: tm
        TableModelColumn { display: "name" }
        rows: [ { "name": "Ada" }, { "name": "Bob" } ]
    }
    TableView {
        id: tv
        height: 90
        model: tm
        selectionModel: ItemSelectionModel { id: sel }
        delegate: Rectangle {
            id: cellRect
            required property bool editing
            required property var display
            implicitWidth: 120; implicitHeight: 28
            Text { visible: !cellRect.editing; text: "" + cellRect.display; color: "#ffffff" }
            TableView.editDelegate: TextField {
                anchors.fill: parent
                required property var edit
                text: "" + cellRect.display
                onAccepted: { edit = text; TableView.commit() }
            }
        }
    }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	if el := d.View()(); el == nil {
		t.Fatal("nil view")
	}
	// No editor yet: the edit cell is unset.
	if _, ok := d.doc.synthRefs["tv|edit:1:0"]; ok {
		t.Fatal("editor state exists before editing started")
	}
	// Open the editor on row 1 (what a second click on the current row does).
	st := d.doc.tables["tv"]
	if st == nil {
		t.Fatal("the table keeps no ui.TableState")
	}
	st.Edit(1, 0)
	if el := d.View()(); el == nil {
		t.Fatal("nil view while editing")
	}
	edit, ok := d.doc.synthRefs["tv|edit:1:0"]
	if !ok {
		t.Fatal("edit delegate did not build (no `edit` property for the cell)")
	}
	// Commit an edited value the way the delegate's onAccepted does.
	if err := edit.set("Grace"); err != nil {
		t.Fatalf("set edit: %v", err)
	}
	m, ok := d.env.vars["tm"].(*tableModel)
	if !ok {
		t.Fatal("tm is not a TableModel")
	}
	if !m.setCell(1, 0, edit.get()) {
		t.Fatal("setCell refused the write")
	}
	if got := str(m.cellDisplay(1, 0)); got != "Grace" {
		t.Fatalf("cell after commit = %q, want Grace", got)
	}
}
