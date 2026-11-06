// opacity — bg text with a faded panel overlaid on top (GPUI opacity)
Item {
    color: "#e0e0e0"
    Text { anchors.centerIn: parent; text: "This is background text."; color: "#0000ff"; font.pixelSize: 18 }
    Rectangle {
        anchors.fill: parent; anchors.margins: 32
        opacity: 0.5; color: "#ffffff"; border.width: 3; border.color: "#ff0000"
        justify: "center"; items: "center"
        Rectangle {
            width: 300; height: 300; color: "#0000ff"; border.width: 3; border.color: "#000000"
            justify: "center"; items: "center"; spacing: 8; shadow: [2, 10, 10, "#00000080"]
            Image { source: "../../images/assets/logo.png"; width: 32; height: 32 }
            Text { text: "Opacity Panel (Click to test)"; color: "#ffff00" }
            Rectangle { color: "#000000"; padding: 8; Text { text: "opacity: 0.50"; color: "#ffffff" } }
            Text { text: "🎊✈🎨🎈🎁🎂"; color: "#ffffff"; font.pixelSize: 16 }
            Image { source: "../../../build/images/img2.jpg"; width: 56; height: 36 }
        }
    }
}
