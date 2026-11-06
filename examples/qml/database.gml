// DatabaseManager (script/gml/native.go, registered in
// examples/qml/native.go) proven with a ListView-based table — the
// corpus' own database/tableview pages bind this same Go type to
// TableView, which isn't implemented yet, so this demo stands in until
// that control lands.
Item {
    property var rows: []

    DatabaseManager { id: db }

    Column {
        padding: 16; spacing: 8
        color: "#131318"

        Text { text: "Employees (Go-backed in-memory table)"; color: "#91a7ff" }
        Repeater {
            model: rows
            Row {
                spacing: 12
                Text { text: model.name; color: "#e8e8f0" }
                Text { text: model.role; color: "#9aa0ac" }
                Text { text: "$" + model.salary; color: "#69db7c" }
            }
        }
        Row {
            spacing: 8
            Button { text: "Add engineer"; onClicked: rows = db.addEmployee("New Hire", "Engineer", 110000) }
        }
    }

    Component.onCompleted: rows = db.queryEmployees()
}
