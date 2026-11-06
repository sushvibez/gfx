// popover — dark trigger buttons + an open popover FLOATING over the text
// below it (GPUI popover renders the panel in a deferred/floating layer).
Rectangle {
    color: "#ffffff"; justify: "center"; items: "center"; spacing: 4
    Row { spacing: 6; justify: "center"
        Rectangle { color: "#111111"; padding: 6; Text { text: "Opened Popover"; color: "#ffffff"; font.pixelSize: 12 } }
        Rectangle { color: "#111111"; padding: 6; Text { text: "Open Popover"; color: "#ffffff"; font.pixelSize: 12 } }
    }
    Text { text: "Here is an example text under the popover layer."; color: "#444444"; font.pixelSize: 13 }
    Column { spacing: 3
        Rectangle { width: 260; height: 4; color: "#ffb3b3" }
        Rectangle { width: 260; height: 4; color: "#b3c6ff" }
        Rectangle { width: 260; height: 4; color: "#b3ffb3" }
        Rectangle { width: 260; height: 4; color: "#ffffb3" }
    }
    // The opened popover floats above the flow content (overlapping the text),
    // like Zed's deferred layer — x/y position it just under the trigger row.
    Popup {
        visible: true; x: 190; y: 208
        width: 220; color: "#ffffff"; border.width: 1; border.color: "#0000ff"; padding: 8
        justify: "center"; items: "center"; shadow: [12, 0, 6, "#0000004d"]
        Text { text: "This is a default opened Popover, we can use deferred to render it in a floating layer."; color: "#1e3a8a"; font.pixelSize: 12; width: 200; wrapMode: "WordWrap"; horizontalAlignment: "center" }
    }
}
