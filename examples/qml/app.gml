// Edit this file while the app is running — it hot-reloads on save.
Column {
    padding: 20; spacing: 14
    color: "#131318"
    property int count: 0
    property real progress: 0.3

    Text { text: "GML: QML-compatible markup with hot reload"; color: "#f1f1f7" }

    Rectangle {
        padding: 16; spacing: 10
        color: "#1d1d27"; radius: 12
        border.width: 1; border.color: "#32323f"
        shadow: [14, 0, 6, "#00000080"]

        Row {
            spacing: 10; items: "center"
            Button { text: "count: " + count; onClicked: count = count + 1 }
            Button { text: "reset"; color: "#3a3a4a"; onClicked: count = 0 }
            Text { text: count % 2 == 0 ? "even" : "odd"; color: "#91a7ff" }
        }
    }

    Rectangle {
        padding: 16; spacing: 12
        color: "#1d1d27"; radius: 12
        border.width: 1; border.color: "#32323f"

        Row {
            spacing: 12; items: "center"
            Slider { value: progress; width: 200 }
            // JS builtins in a binding: Math.round for a percentage label.
            Text { text: Math.round(progress * 100) + "%"; color: "#e8e8f0" }
            Button { text: "animate"; width: 110; onClicked: animate(progress, progress < 0.5 ? 1 : 0, 600) }
        }
        TextField { id: name; width: 240 }
        Text {
            text: name.text == "" ? "type your name above" : "hello, " + name.text + "!"
            color: "#b6b6c8"
        }
    }

    // A ListModel drives a Repeater: each ListElement is a row, and the
    // delegate reads its roles (name, cost) plus the row index directly.
    ListModel {
        id: cart
        ListElement { name: "Coffee"; cost: 4 }
        ListElement { name: "Bagel";  cost: 3 }
        ListElement { name: "Juice";  cost: 5 }
    }

    Rectangle {
        padding: 16; spacing: 8
        color: "#1d1d27"; radius: 12
        border.width: 1; border.color: "#32323f"

        Text { text: "ListModel + Repeater (role delegates)"; color: "#b6b6c8" }
        Repeater {
            model: cart
            Row {
                spacing: 10
                Text { text: (index + 1) + ". " + name; color: "#e8e8f0" }
                Text { text: "$" + model.cost; color: "#91a7ff" }
            }
        }
    }
}
