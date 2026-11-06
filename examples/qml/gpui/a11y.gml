// a11y — dark page, bold heading, Count/Reset pills, feature toggle,
// numbered task list (GPUI a11y resting state)
Rectangle {
    color: "#1e1e2e"; padding: 16; spacing: 16
    Text { text: "Accessibility Demo"; color: "#cdd6f4"; font.pixelSize: 22; font.bold: true }
    Row { spacing: 12; items: "center"
        Rectangle { radius: 6; padding: 8; color: "#89b4fa"; Text { text: "Count: 0"; color: "#1e1e2e"; font.pixelSize: 14 } }
        Rectangle { radius: 6; padding: 8; color: "#45475a"; Text { text: "Reset"; color: "#cdd6f4"; font.pixelSize: 14 } }
    }
    Row { spacing: 12; items: "center"
        Rectangle { width: 44; height: 24; radius: 12; padding: 3; color: "#45475a"; justify: "center"
            Rectangle { width: 18; height: 18; radius: 9; color: "#ffffff" }
        }
        Text { text: "Enable feature"; color: "#cdd6f4"; font.pixelSize: 15 }
    }
    Column { spacing: 10
        Text { text: "1. Write code"; color: "#cdd6f4"; font.pixelSize: 15 }
        Text { text: "2. Run tests"; color: "#cdd6f4"; font.pixelSize: 15 }
        Text { text: "3. Ship it"; color: "#cdd6f4"; font.pixelSize: 15 }
    }
}
