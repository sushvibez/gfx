// gradient — header + solids + linear-gradient rows (GPUI gradient)
Rectangle {
    color: "#ffffff"; padding: 16; spacing: 12
    Row { items: "center"
        Text { text: "Gradient Examples"; color: "#000000" }
        Spacer {}
        Rectangle { color: "#000000"; padding: 6; Text { text: "sRGB"; color: "#ffffff"; font.pixelSize: 13 } }
    }
    Row { spacing: 12; grow: true
        Rectangle { grow: true; radius: 10; color: "#ff0000"; justify: "center"; items: "center"; Text { text: "Solid Color"; color: "#ffffff"; font.pixelSize: 13 } }
        Rectangle { grow: true; radius: 10; color: "#0000ff"; justify: "center"; items: "center"; Text { text: "Solid Color"; color: "#ffffff"; font.pixelSize: 13 } }
    }
    Row { spacing: 12; grow: true
        Rectangle { grow: true; radius: 10; gradient: ["#ff0000", "#000000", 90] }
        Rectangle { grow: true; radius: 10; gradient: ["#000000", "#008000", 0] }
        Rectangle { grow: true; radius: 10; gradient: ["#000044", "#008000", 90] }
        Rectangle { grow: true; radius: 10; gradient: ["#99dd00", "#33aa00", 0] }
    }
    Row { spacing: 12; grow: true
        Rectangle { grow: true; radius: 10; gradient: ["#ff0000", "#ffffff", 90] }
        Rectangle { grow: true; radius: 10; gradient: ["#0000ff", "#ffffff", 0] }
        Rectangle { grow: true; radius: 10; gradient: ["#008000", "#ffffff", 90] }
        Rectangle { grow: true; radius: 10; gradient: ["#ffff00", "#ffff66", 0] }
    }
    Rectangle { grow: true; radius: 10; gradient: ["#99dd00", "#ffff00", 0] }
    Rectangle { grow: true; radius: 10; gradient: ["#0000ff", "#ff0000", 0] }
    Row { spacing: 12; grow: true
        // Hard stop: both GradientStops at 0.5 — a sharp blue|red split.
        Rectangle { grow: true; radius: 10
            gradient: Gradient { orientation: "Horizontal"
                GradientStop { position: 0.5; color: "#0000ff" }
                GradientStop { position: 0.5; color: "#ff0000" }
            }
        }
        Rectangle { grow: true; radius: 10; gradient: ["#008000", "#0000ff", 0] }
    }
    // Gradient on a custom path: the bottom trapezoid, via Canvas.
    Row { justify: "center"
        Canvas {
            width: 520; height: 40
            onPaint: {
                var ctx = getContext("2d")
                var g = ctx.createLinearGradient(0, 0, 520, 0)
                g.addColorStop(0, "#8c0000")
                g.addColorStop(1, "#00008c")
                ctx.fillStyle = g
                ctx.beginPath()
                ctx.moveTo(60, 0); ctx.lineTo(460, 0); ctx.lineTo(510, 40); ctx.lineTo(10, 40); ctx.closePath()
                ctx.fill()
            }
        }
    }
}
