// animation — "Hello Animation" over a spinning arrow icon + footer panel
// (GPUI animation: with_animation rotating an SVG). The NumberAnimation
// free-runs the angle; the Image rotates around its center.
Rectangle {
    color: "#ffffff"
    property real angle: 0
    NumberAnimation { property: "angle"; from: 0; to: 360; duration: 2000; loops: Animation.Infinite; running: true }
    Column { grow: true; justify: "center"; items: "center"; spacing: 16
        Text { text: "Hello Animation"; color: "#000000"; font.pixelSize: 20 }
        Image { source: "arrow_circle.svg"; width: 80; height: 80; rotation: angle }
    }
    Rectangle { width: 300; height: 1; color: "#0000001a" }
    Row { height: 64; padding: 8; justify: "center"; items: "center"; color: "#0000000d"
        Text { text: "Other Panel"; color: "#000000" }
    }
}
