// tab_stop — message + tab_index rows + dimmed + button row + grouped boxes (GPUI tab_stop)
Rectangle {
    color: "#ffffff"; padding: 16; spacing: 12
    Text { text: "Press `Tab`, `Shift-Tab` to switch focus."; color: "#000000" }
    Rectangle { height: 40; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "tab_index: 1"; color: "#000000" } }
    Rectangle { height: 40; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "tab_index: 2"; color: "#000000" } }
    Rectangle { height: 40; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "tab_index: 3"; color: "#000000" } }
    Rectangle { height: 40; border.width: 1; border.color: "#000000"; opacity: 0.4; justify: "center"; items: "center"; Text { text: "tab_stop: false"; color: "#000000" } }
    Rectangle { height: 40; border.width: 1; border.color: "#000000"; justify: "center"; items: "center"; Text { text: "tab_index: 2"; color: "#000000" } }
    Row { spacing: 12
        Rectangle { grow: true; height: 40; color: "#000000"; justify: "center"; items: "center"; Text { text: "Button 1"; color: "#ffffff" } }
        Rectangle { grow: true; height: 40; color: "#000000"; justify: "center"; items: "center"; Text { text: "Button 2"; color: "#ffffff" } }
    }
    Rectangle { color: "#000000"; padding: 8; spacing: 6
        Rectangle { justify: "center"; Text { text: "Tab index [6, 1]"; color: "#ffffff" } }
        Rectangle { justify: "center"; Text { text: "Tab index [6, 2]"; color: "#ffffff" } }
        Rectangle { justify: "center"; Text { text: "Tab index [6, 3]"; color: "#ffffff" } }
    }
    Rectangle { color: "#000000"; padding: 8; spacing: 6
        Rectangle { justify: "center"; Text { text: "Tab index [7, 1]"; color: "#ffffff" } }
        Rectangle { justify: "center"; Text { text: "Tab index [7, 2]"; color: "#ffffff" } }
        Rectangle { justify: "center"; Text { text: "Tab index [7, 3]"; color: "#ffffff" } }
    }
}
