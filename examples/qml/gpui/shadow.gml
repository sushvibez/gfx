// shadow — rounded variants (pink), size/blur/spread scales (grey), grid lines (GPUI shadow)
Rectangle {
    color: "#ffffff"; spacing: 0
    Row { grow: true
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 0; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [8, 0, 8, "#bf40404d"] }
            Text { text: "Square"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 4; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [8, 0, 8, "#bf40404d"] }
            Text { text: "Rounded 4"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 8; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [8, 0, 8, "#bf40404d"] }
            Text { text: "Rounded 8"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 16; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [8, 0, 8, "#bf40404d"] }
            Text { text: "Rounded 16"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [8, 0, 8, "#bf40404d"] }
            Text { text: "Circle"; color: "#000000"; font.pixelSize: 11 } }
    }
    Row { grow: true
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [0, 0, 8, "#0000004d"] }
            Text { text: "None"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [2, 0, 8, "#0000004d"] }
            Text { text: "2X Small"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [4, 0, 8, "#0000004d"] }
            Text { text: "Extra Small"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [6, 0, 8, "#0000004d"] }
            Text { text: "Small"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [10, 0, 8, "#0000004d"] }
            Text { text: "Medium"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [14, 0, 8, "#0000004d"] }
            Text { text: "Large"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [16, 0, 8, "#0000004d"] }
            Text { text: "Extra Large"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [20, 0, 8, "#0000004d"] }
            Text { text: "2X Large"; color: "#000000"; font.pixelSize: 11 } }
    }
    Row { grow: true
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [0, 0, 8, "#0000004d"] }
            Text { text: "Blur 0"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [2, 0, 8, "#0000004d"] }
            Text { text: "Blur 2"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [4, 0, 8, "#0000004d"] }
            Text { text: "Blur 4"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [8, 0, 8, "#0000004d"] }
            Text { text: "Blur 8"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [16, 0, 8, "#0000004d"] }
            Text { text: "Blur 16"; color: "#000000"; font.pixelSize: 11 } }
    }
    Row { grow: true
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [8, 0, 8, "#0000004d"] }
            Text { text: "Spread 0"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [10, 0, 8, "#0000004d"] }
            Text { text: "Spread 2"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [12, 0, 8, "#0000004d"] }
            Text { text: "Spread 4"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [16, 0, 8, "#0000004d"] }
            Text { text: "Spread 8"; color: "#000000"; font.pixelSize: 11 } }
        Column { grow: true; items: "center"; spacing: 8; border.width: 1; border.color: "#000000cc"
            Rectangle { width: 64; height: 64; radius: 32; color: "#ffffff"; border.width: 1; border.color: "#0000001a"; shadow: [24, 0, 8, "#0000004d"] }
            Text { text: "Spread 16"; color: "#000000"; font.pixelSize: 11 } }
    }
}
