// focus_visible — title, message, 3 sections each a label + full-width button
Rectangle {
    color: "#f3f4f6"; padding: 32; spacing: 24
    Text { text: "CSS focus-visible Demo"; color: "#111827"; font.pixelSize: 24; font.bold: true }
    Rectangle { color: "#dbeafe"; radius: 6; padding: 16
        Text { text: "Try clicking vs tabbing! Click shows no border, Tab shows border."; color: "#1e3a8a" } }
    Column { spacing: 16
        Column { spacing: 8
            Text { text: "1. Regular .focus() - always visible:"; color: "#374151"; font.pixelSize: 13; font.bold: true }
            Rectangle { color: "#2563eb"; radius: 6; height: 64; justify: "center"; items: "center"
                Text { text: "Button with .focus() - always shows border when focused"; color: "#ffffff" } } }
        Column { spacing: 8
            Text { text: "2. New .focus_visible() - only keyboard:"; color: "#374151"; font.pixelSize: 13; font.bold: true }
            Rectangle { color: "#2563eb"; radius: 6; height: 64; justify: "center"; items: "center"
                Text { text: "Button with .focus_visible() - only shows border with keyboard"; color: "#ffffff" } } }
        Column { spacing: 8
            Text { text: "3. Both .focus() (yellow) and .focus_visible() (green):"; color: "#374151"; font.pixelSize: 13; font.bold: true }
            Rectangle { color: "#2563eb"; radius: 6; height: 64; justify: "center"; items: "center"
                Text { text: "Button with both .focus() and .focus_visible()"; color: "#ffffff" } } }
    }
}
