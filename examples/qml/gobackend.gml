// Go-registered QML types in action (script/gml/native.go): the
// WebSocketClient element below is implemented in Go (native.go in this
// example) — its properties are live core.Property bindings, its methods
// are invokable from markup, and its signals (emitted from Go, including
// from goroutines) run the on<Signal> handlers here, which append to a
// ListModel at runtime.
Column {
    padding: 20; spacing: 10
    color: "#131318"

    WebSocketClient {
        id: ws
        onMessageReceived: function(m) { log.append({text: "<- " + m}) }
        onMessageSent: function(m) { log.append({text: "-> " + m}) }
    }
    ListModel { id: log }

    Text { text: "Go backend status: " + ws.statusText; color: "#91a7ff" }
    Row {
        spacing: 8
        Button { text: "Connect"; onClicked: ws.connectToServer() }
        Button { text: "Send ping"; onClicked: ws.sendMessage("ping") }
        Button { text: "Disconnect"; onClicked: ws.disconnectFromServer() }
    }
    Repeater {
        model: log
        Text { text: model.text; color: "#e8e8f0" }
    }

    Component.onCompleted: {
        ws.connectToServer()
        ws.sendMessage("hello from markup")
    }
}
