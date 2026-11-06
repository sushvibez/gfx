// input — header label + Reset + a light text field (GPUI input)
Rectangle {
    color: "#ffffff"; spacing: 0
    Row { items: "center"
        Rectangle { grow: true; padding: 6; Text { text: "Keyboard English (US)"; color: "#000000" } }
        Rectangle { color: "#ffff00"; padding: 6; Text { text: "Reset"; color: "#000000" } }
    }
    Rectangle { height: 1; color: "#00000033" }
    Rectangle { padding: 8; TextField { id: f; width: 270; color: "#ffffff"; border.color: "#00000033"; textColor: "#000000"; radius: 2; placeholderText: "Type here..." } }
}
