// The same 2D kitchen sink as sink.go, written QML-style. Run with
//   go run ./sink2d -gml
// and compare to the Go-builder version (no flag). Hot-reloadable.
Column {
    color: "#14141c"
    property int count: 0
    property real pct: 0.6
    property bool dark: true

    Rectangle {
        height: 64
        gradient: ["#7048e8", "#4263eb", 90]
        justify: "center"; items: "center"
        Text { text: "2D kitchen sink — QML / .gml"; color: "#ffffff" }
    }

    Column {
        padding: 16; spacing: 14

        Rectangle {
            padding: 16; spacing: 10; color: "#1d1d27"; radius: 12
            border.width: 1; border.color: "#32323f"
            shadow: [14, 0, 6, "#00000080"]
            Text { text: "Counter, slider, checkbox"; color: "#b6b6c8" }
            Row {
                spacing: 10; items: "center"
                Button { text: "-"; width: 44; onClicked: count = count - 1 }
                Button { text: "+"; width: 44; onClicked: count = count + 1 }
                Text { text: "count: " + count; color: "#e8e8f0" }
            }
            Row {
                spacing: 10; items: "center"
                Slider { value: pct; width: 200 }
                Text { text: pct * 100 + "%"; color: "#e8e8f0" }
            }
            CheckBox { text: "Dark mode"; checked: dark }
        }

        Rectangle {
            padding: 16; spacing: 10; color: "#1d1d27"; radius: 12
            border.width: 1; border.color: "#32323f"
            shadow: [14, 0, 6, "#00000080"]
            Text { text: "Text input (greeting is a binding)"; color: "#b6b6c8" }
            TextField { id: name; width: 240 }
            Text {
                text: name.text == "" ? "type a name above" : "Hello, " + name.text + "!"
                color: "#91a7ff"
            }
        }

        Rectangle {
            padding: 16; spacing: 10; color: "#1d1d27"; radius: 12
            border.width: 1; border.color: "#32323f"
            Text { text: "Virtualized list (5,000 rows)"; color: "#b6b6c8" }
            ListView {
                id: rows; height: 160; rowHeight: 32; count: 5000
                Row {
                    height: 32; padding: 8; items: "center"
                    color: index % 2 == 0 ? "#1e1e28" : "#24242f"
                    Text { text: "Row " + index; color: "#d4d4e0" }
                }
            }
        }
    }
}
