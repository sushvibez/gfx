// pattern — "Pattern Example" + diagonal slash boxes via Canvas (GPUI pattern).
// The pale panel + its label are real elements (canvas draws only the hatch
// bands); the bottom bars are real Rectangles so the second gets a true
// red→blue gradient.
Rectangle {
    color: "#ffffff"; justify: "center"; items: "center"; spacing: 12
    Text { text: "Pattern Example"; color: "#000000"; font.pixelSize: 20 }
    Canvas {
        width: 60; height: 78
        onPaint: {
            var ctx = getContext("2d")
            var x = 0; var lo = 0; var hi = 0
            ctx.strokeStyle = "#0000ff"; ctx.lineWidth = 1; ctx.strokeRect(2, 2, 56, 74)
            ctx.strokeStyle = "#ff0000"; ctx.lineWidth = 2
            var base = 20
            while (base < 78) {
                x = -18
                while (x < 54) { lo = Math.max(-x, 0); hi = Math.min(54 - x, 18); if (hi > lo) { ctx.beginPath(); ctx.moveTo(3 + x + lo, base - lo); ctx.lineTo(3 + x + hi, base - hi); ctx.stroke() } x = x + 4.5 }
                base = base + 18
            }
        }
    }
    Rectangle {
        width: 340; color: "#0080801f"; border.width: 1; border.color: "#0000ff"; padding: 4; spacing: 4
        Text { text: "Elements the same height should align"; color: "#111111"; font.pixelSize: 13 }
        Row {
            Canvas {
                width: 200; height: 170
                onPaint: {
                    var ctx = getContext("2d")
                    var x = 0; var lo = 0; var hi = 0
                    var colors = ["#ff0000", "#008000", "#0000ff"]
                    for (var band = 0; band < 3; band = band + 1) {
                        ctx.strokeStyle = colors[band]; ctx.lineWidth = 3
                        var base = 48 * (band + 1)
                        x = -48
                        while (x < 200) { lo = Math.max(-x, 0); hi = Math.min(200 - x, 48); if (hi > lo) { ctx.beginPath(); ctx.moveTo(x + lo, base - lo); ctx.lineTo(x + hi, base - hi); ctx.stroke() } x = x + 9.3 }
                    }
                    ctx.strokeStyle = "#ffff00"; ctx.lineWidth = 3; x = -24
                    while (x < 200) { lo = Math.max(-x, 0); hi = Math.min(200 - x, 24); if (hi > lo) { ctx.beginPath(); ctx.moveTo(x + lo, 168 - lo); ctx.lineTo(x + hi, 168 - hi); ctx.stroke() } x = x + 9.3 }
                }
            }
            Spacer { }
        }
    }
    Column { spacing: 6
        Rectangle { width: 240; height: 26; color: "#ff0000"; border.width: 1; border.color: "#0000ff" }
        Rectangle { width: 240; height: 26; gradient: ["#ff0000", "#0000ff", 0] }
    }
}
