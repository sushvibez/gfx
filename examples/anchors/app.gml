// QML anchors: this Rectangle is an absolute anchor layout (it has anchored
// children), not a stack. Children position themselves with anchors.fill,
// anchors.centerIn, edge anchors (parent.top / sibling.right) and margins —
// the Qt Quick positioning model. Edit live; it hot-reloads on save.
import QtQuick 2.15

Rectangle {
    color: "#131318"

    // A header bar pinned across the top.
    Rectangle {
        id: header
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        height: 56
        color: "#7048e8"
        Text { anchors.centerIn: parent; text: "anchors"; color: "#ffffff" }
    }

    // A sidebar pinned down the left under the header.
    Rectangle {
        id: sidebar
        anchors.left: parent.left
        anchors.top: header.bottom
        anchors.bottom: parent.bottom
        width: 120
        color: "#1d1d27"
        Text { anchors.centerIn: parent; text: "side"; color: "#8f8fa3" }
    }

    // The content fills the area right of the sidebar, below the header.
    Rectangle {
        anchors.left: sidebar.right
        anchors.right: parent.right
        anchors.top: header.bottom
        anchors.bottom: parent.bottom
        anchors.margins: 16
        color: "#24242f"
        radius: 12

        Text {
            anchors.centerIn: parent
            text: "content — centered in the remaining space"
            color: "#e8e8f0"
        }

        // A badge tucked into the bottom-right corner with a margin.
        Rectangle {
            anchors.right: parent.right
            anchors.bottom: parent.bottom
            anchors.margins: 14
            width: 54; height: 54; radius: 27
            color: "#e64980"
        }
    }
}
