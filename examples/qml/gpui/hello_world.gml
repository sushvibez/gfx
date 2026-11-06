// hello_world — gray panel, centered text + 6 swatches (GPUI hello_world)
Rectangle {
    color: "#505050"
    border.width: 1; border.color: "#0000ff"
    justify: "center"; items: "center"; spacing: 12
    Text { text: "Hello, World!"; color: "#ffffff"; font.pixelSize: 20 }
    Row {
        spacing: 8; justify: "center"
        Rectangle { width: 32; height: 32; radius: 6; color: "#ff0000"; border.width: 1; border.color: "#ffffff"; border.style: "dashed" }
        Rectangle { width: 32; height: 32; radius: 6; color: "#008000"; border.width: 1; border.color: "#ffffff"; border.style: "dashed" }
        Rectangle { width: 32; height: 32; radius: 6; color: "#0000ff"; border.width: 1; border.color: "#ffffff"; border.style: "dashed" }
        Rectangle { width: 32; height: 32; radius: 6; color: "#ffff00"; border.width: 1; border.color: "#ffffff"; border.style: "dashed" }
        Rectangle { width: 32; height: 32; radius: 6; color: "#000000"; border.width: 1; border.color: "#ffffff"; border.style: "dashed" }
        Rectangle { width: 32; height: 32; radius: 6; color: "#ffffff"; border.width: 1; border.color: "#000000"; border.style: "dashed" }
    }
}
