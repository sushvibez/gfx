// QML Image element: `source` is a path relative to this .gml file; bytes
// are read once and the GPU texture is decoded lazily on first paint. Images
// are tinted/fit to their box and compose with anchors like any element.
// Edit live; it hot-reloads on save.
import QtQuick 2.15

Rectangle {
    color: "#131318"

    Column {
        anchors.centerIn: parent
        spacing: 16
        items: "center"

        Text { text: "QML Image element"; color: "#f1f1f7" }

        // Three sizes of the same source, fit with preserved aspect ratio.
        Row {
            spacing: 16; items: "center"
            Image { source: "assets/logo.png"; width: 64;  height: 64 }
            Image { source: "assets/logo.png"; width: 110; height: 110 }
            Image { source: "assets/logo.png"; width: 64;  height: 64 }
        }

        // A missing source falls back to a muted placeholder box.
        Row {
            spacing: 12; items: "center"
            Image { source: "assets/missing.png"; width: 60; height: 60 }
            Text { text: "(missing source -> placeholder)"; color: "#8f8fa3" }
        }
    }
}
