// anchor — "Popover with Anchor" + 3x3 grid of outlined buttons (GPUI anchor)
Rectangle {
    color: "#ffffff"; justify: "center"; items: "center"; spacing: 16
    Text { text: "Popover with Anchor"; color: "#000000" }
    Column { spacing: 24; items: "center"
        Row { spacing: 24; justify: "center"
            Rectangle { width: 120; height: 65; radius: 8; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "TopLeft"; color: "#000000"; font.pixelSize: 13 } }
            Rectangle { width: 120; height: 65; radius: 8; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "TopCenter"; color: "#000000"; font.pixelSize: 13 } }
            Rectangle { width: 120; height: 65; radius: 8; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "TopRight"; color: "#000000"; font.pixelSize: 13 } }
        }
        Row { spacing: 24; justify: "center"
            Rectangle { width: 120; height: 65; radius: 8; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "LeftCenter"; color: "#000000"; font.pixelSize: 13 } }
            Rectangle { width: 120; height: 65 }
            Rectangle { width: 120; height: 65; radius: 8; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "RightCenter"; color: "#000000"; font.pixelSize: 13 } }
        }
        Row { spacing: 24; justify: "center"
            Rectangle { width: 120; height: 65; radius: 8; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "BottomLeft"; color: "#000000"; font.pixelSize: 13 } }
            Rectangle { width: 120; height: 65; radius: 8; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "BottomCenter"; color: "#000000"; font.pixelSize: 13 } }
            Rectangle { width: 120; height: 65; radius: 8; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "BottomRight"; color: "#000000"; font.pixelSize: 13 } }
        }
    }
}
