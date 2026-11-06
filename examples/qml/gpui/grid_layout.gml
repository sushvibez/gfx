// grid_layout — header / sidebar+content+ad / footer on dark page (GPUI grid_layout)
Rectangle {
    color: "#505050"; padding: 8; spacing: 6
    Rectangle { grow: true; height: 70; color: "#ffffff"; radius: 6; border.width: 1; border.color: "#ffffff"; justify: "center"; items: "center"; Text { text: "Header"; color: "#000000"; font.pixelSize: 13 } }
    Row { spacing: 6; height: 300
        Column { width: 120
            Rectangle { height: 200; color: "#ff0000"; radius: 6; border.width: 1; border.color: "#ffffff"; justify: "center"; items: "center"; Text { text: "Table of contents"; color: "#ffffff"; font.pixelSize: 13 } }
            Rectangle { grow: true; color: "#505050" }
        }
        Rectangle { grow: true; color: "#008000"; radius: 6; border.width: 1; border.color: "#ffffff"; justify: "center"; items: "center"; Text { text: "Content"; color: "#ffffff"; font.pixelSize: 13 } }
        Rectangle { width: 120; color: "#0000ff"; radius: 6; border.width: 1; border.color: "#ffffff"; justify: "center"; items: "center"; Text { text: "AD :("; color: "#ffffff"; font.pixelSize: 13 } }
    }
    Rectangle { grow: true; height: 70; color: "#000000"; radius: 6; border.width: 1; border.color: "#ffffff"; justify: "center"; items: "center"; Text { text: "Footer"; color: "#ffffff"; font.pixelSize: 13 } }
}
