// Phase-C elements: a MouseArea-driven header, a Grid of tappable swatches,
// and a Flickable scroll viewport — all positioned with anchors. Tapping a
// swatch bumps the counter via MouseArea.onClicked. Edit live; hot-reloads.
import QtQuick 2.15

Rectangle {
    color: "#131318"
    property int taps: 0

    // Header bar (anchored across the top) with a MouseArea reset button.
    Rectangle {
        id: header
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        height: 56
        color: "#1d1d27"

        Text {
            anchors.left: parent.left
            anchors.verticalCenter: parent.verticalCenter
            anchors.leftMargin: 16
            text: "swatch taps: " + taps
            color: "#e8e8f0"
        }

        Rectangle {
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            anchors.rightMargin: 12
            width: 80; height: 34; radius: 8
            color: "#3b5bdb"
            Text { anchors.centerIn: parent; text: "reset"; color: "#ffffff" }
            MouseArea { anchors.fill: parent; onClicked: taps = 0 }
        }
    }

    // A scrollable Grid of swatches filling the area under the header.
    Flickable {
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: header.bottom
        anchors.bottom: parent.bottom
        contentHeight: 600

        Grid {
            columns: 4
            spacing: 12
            padding: 16
            Repeater {
                model: 16
                Rectangle {
                    width: 86; height: 86; radius: 12
                    color: "#24242f"
                    Text { anchors.centerIn: parent; text: "" + index; color: "#8f8fa3" }
                    MouseArea { anchors.fill: parent; onClicked: taps = taps + 1 }
                }
            }
        }
    }
}
