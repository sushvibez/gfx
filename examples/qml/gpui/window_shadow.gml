// window_shadow — lavender backdrop, white window, green titlebar box (GPUI window_shadow)
Rectangle {
    color: "#ccccff"; justify: "center"; items: "center"
    Rectangle {
        width: 300; height: 300; color: "#ffffff"; border.width: 1; border.color: "#0000ff"
        justify: "center"; items: "center"; shadow: [10, 0, 4, "#00000066"]
        Rectangle {
            width: 190; height: 90; color: "#008000"; justify: "center"; items: "center"
            shadow: [14, 0, 6, "#00000099"]
            Text { text: "this is the custom titlebar"; color: "#000000"; font.pixelSize: 13 }
        }
    }
}
