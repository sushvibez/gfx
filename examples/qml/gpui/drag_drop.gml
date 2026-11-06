// drag_drop — title, 3 colour-bordered items, a drop zone (GPUI drag_drop)
Rectangle {
    color: "#ffffff"; justify: "center"; items: "center"; spacing: 24; padding: 24
    Text { text: "Drop & Drop"; color: "#000000"; font.pixelSize: 20 }
    Row { spacing: 16; justify: "center"
        Rectangle { width: 120; height: 120; radius: 4; color: "#ffffff"; border.width: 2; border.color: "#0000ff"; justify: "center"; items: "center"; Text { text: "Item (0)"; color: "#0000ff" } }
        Rectangle { width: 120; height: 120; radius: 4; color: "#ffffff"; border.width: 2; border.color: "#ff0000"; justify: "center"; items: "center"; Text { text: "Item (1)"; color: "#ff0000" } }
        Rectangle { width: 120; height: 120; radius: 4; color: "#ffffff"; border.width: 2; border.color: "#008000"; justify: "center"; items: "center"; Text { text: "Item (2)"; color: "#008000" } }
    }
    Rectangle { width: 440; height: 130; color: "#ffffff"; border.width: 2; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "Drop items here"; color: "#555555" } }
}
