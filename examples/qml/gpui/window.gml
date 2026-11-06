// window — white page, two rows of window-option buttons (GPUI window default)
Rectangle {
    color: "#ffffff"; justify: "center"; padding: 16; spacing: 8
    Row { spacing: 8
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Normal"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Popup"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Floating"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Dialog"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Custom Titlebar"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Invisible"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Unmovable"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Unresizable"; color: "#111111"; font.pixelSize: 13 } }
    }
    Row { spacing: 8
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Unminimizable"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Hide Application"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Resize"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Prompt"; color: "#111111"; font.pixelSize: 13 } }
        Rectangle { radius: 4; padding: 6; border.width: 1; border.color: "#00000040"; Text { text: "Prompt (non-English)"; color: "#111111"; font.pixelSize: 13 } }
    }
}
