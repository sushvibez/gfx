// States & transitions: the panel below switches between a "collapsed" base
// state and a named "expanded" state. The Transition animates the numeric
// properties (height, radius) so the change tweens instead of snapping.
// Edit this file while the app runs — it hot-reloads on save.
Column {
    padding: 20; spacing: 16
    color: "#131318"
    property bool open: true

    Text { text: "QML states & transitions"; color: "#f1f1f7" }

    Row {
        spacing: 10; items: "center"
        Button {
            width: 130
            text: open ? "collapse" : "expand"
            onClicked: open = !open
        }
        Text { text: open ? "state: expanded" : "state: \"\" (base)"; color: "#91a7ff" }
    }

    // The stateful Item: its `state` picks a named State; PropertyChanges in
    // that state override the targeted panel; the Transition animates them.
    Item {
        state: open ? "expanded" : ""
        states: [
            State {
                name: "expanded"
                PropertyChanges { target: panel; height: 220; radius: 24; color: "#7048e8" }
            }
        ]
        transitions: [
            Transition {
                NumberAnimation { properties: "height,radius"; duration: 280; easing.type: "OutCubic" }
            }
        ]

        Rectangle {
            id: panel
            width: 320; height: 70; radius: 10
            color: "#3b5bdb"
            padding: 16
            Text { text: "I grow and round my corners"; color: "#ffffff" }
        }
    }
}
