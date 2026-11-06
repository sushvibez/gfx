// QML Timer + signals. The Timer fires onTriggered every interval; here it
// emits a custom `tick` signal whose `onTick` handler advances the counter.
// `running` is bound to a property so the buttons can pause/resume. Edit
// live; it hot-reloads on save.
import QtQuick 2.15

Column {
    padding: 20; spacing: 14
    color: "#131318"
    property int seconds: 0
    property bool running: true

    // A custom signal; onTick (below) is its handler, run with `value` bound.
    signal tick(value)
    onTick: seconds = value

    Timer {
        interval: 1000
        running: running   // bound to the property, so pause/resume works
        repeat: true
        onTriggered: tick(seconds + 1)
    }

    Text { text: "Timer + signals"; color: "#f1f1f7" }

    Rectangle {
        padding: 16; spacing: 8
        color: "#1d1d27"; radius: 12
        border.width: 1; border.color: "#32323f"
        Text { text: "elapsed: " + seconds + "s"; color: "#69db7c" }
        Text { text: running ? "running" : "paused"; color: "#91a7ff" }
    }

    Row {
        spacing: 10
        Button { text: running ? "pause" : "resume"; width: 120; onClicked: running = !running }
        Button { text: "reset"; width: 90; color: "#3a3a4a"; onClicked: { seconds = 0 } }
    }
}
