// FileSystemTreeModel (script/gml/native.go, registered in
// examples/qml/native.go) proven with a ListView-based browser — the
// corpus' own treeview page binds this same Go type to a TreeView, which
// isn't implemented yet, so this demo stands in until that control lands.
Item {
    id: root
    property string cwd: "."
    property var rows: []

    FileSystemTreeModel { id: fs }

    Column {
        padding: 16; spacing: 8
        color: "#131318"

        Text { text: "cwd: " + root.cwd; color: "#91a7ff" }
        Row {
            spacing: 8
            Button {
                text: ".."
                onClicked: {
                    root.cwd = fs.parentDir(root.cwd)
                    root.rows = fs.listDir(root.cwd)
                }
            }
            Button {
                text: "Refresh"
                onClicked: root.rows = fs.listDir(root.cwd)
            }
        }
        Repeater {
            model: root.rows
            Row {
                spacing: 8
                Text {
                    text: (model.isDir ? "[dir] " : "      ") + model.name
                    color: model.isDir ? "#e8e8f0" : "#9aa0ac"
                }
                MouseArea {
                    width: 60; height: 16
                    onClicked: {
                        if (model.isDir) {
                            root.cwd = model.path
                            root.rows = fs.listDir(root.cwd)
                        }
                    }
                }
            }
        }
    }

    Component.onCompleted: root.rows = fs.listDir(root.cwd)
}
