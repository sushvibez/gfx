// text_layout — L/C/R aligned text × plain/underline/line-through (GPUI text_layout)
Rectangle {
    color: "#ffffff"; padding: 16; spacing: 8
    Text { text: "Text left"; color: "#000000"; width: 784; font.pixelSize: 15 }
    Text { text: "Text center"; color: "#000000"; width: 784; horizontalAlignment: "center"; font.pixelSize: 15 }
    Text { text: "Text right"; color: "#000000"; width: 784; horizontalAlignment: "right"; font.pixelSize: 15 }
    Text { text: "Text left (underline)"; color: "#000000"; width: 784; font.underline: true; font.pixelSize: 15 }
    Text { text: "Text center (underline)"; color: "#000000"; width: 784; horizontalAlignment: "center"; font.underline: true; font.pixelSize: 15 }
    Text { text: "Text right (underline)"; color: "#000000"; width: 784; horizontalAlignment: "right"; font.underline: true; font.pixelSize: 15 }
    Text { text: "Text left (line_through)"; color: "#000000"; width: 784; font.strikeout: true; font.pixelSize: 15 }
    Text { text: "Text center (line_through)"; color: "#000000"; width: 784; horizontalAlignment: "center"; font.strikeout: true; font.pixelSize: 15 }
    Text { text: "Text right (line_through)"; color: "#000000"; width: 784; horizontalAlignment: "right"; font.strikeout: true; font.pixelSize: 15 }
    Row { spacing: 12
        Rectangle { width: 360; border.width: 1; border.color: "#0000ff"; padding: 6; justify: "center"; Text { text: "A long non-wrapping text align center"; color: "#000000"; font.pixelSize: 14 } }
        Rectangle { width: 120; border.width: 1; border.color: "#0000ff"; padding: 6; Text { text: "100%"; color: "#000000"; width: 108; horizontalAlignment: "right"; font.pixelSize: 14 } }
    }
    Text { text: "ABCD"; color: "#000000"; font.pixelSize: 14 }
}
