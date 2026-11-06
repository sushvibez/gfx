// painting — canvas with swatches/star/pie/zigzag/curves (GPUI painting)
Rectangle {
    color: "#ffffff"; padding: 16; spacing: 12
    Row { items: "center"; spacing: 12
        Text { text: "Mouse down and drag to draw lines (Shift for straight lines)"; color: "#222222"; font.pixelSize: 13 }
        Spacer {}
        Rectangle { color: "#111111"; radius: 6; padding: 8; Text { text: "Dashed"; color: "#ffffff"; font.pixelSize: 13 } }
        Rectangle { color: "#111111"; radius: 6; padding: 8; Text { text: "Clear"; color: "#ffffff"; font.pixelSize: 13 } }
    }
    Canvas {
        width: 660; height: 360
        onPaint: {
            var ctx = getContext("2d")
            var cols = ["#e63333", "#3366f2", "#33b359", "#000000"]
            for (var i = 0; i < 4; i = i + 1) {
                ctx.fillStyle = cols[i]
                ctx.fillRect(10 + i * 44, 10, 36, 36)
            }
            // star
            ctx.fillStyle = "#f5b800"
            ctx.beginPath()
            for (var k = 0; k < 11; k = k + 1) {
                var ang = -1.5708 + k * 0.62832
                var rr = 38
                if (k % 2 == 1) { rr = 16 }
                var px = 70 + rr * Math.cos(ang)
                var py = 150 + rr * Math.sin(ang)
                if (k == 0) { ctx.moveTo(px, py) } else { ctx.lineTo(px, py) }
            }
            ctx.closePath()
            ctx.fill()
            // pie chart (4 wedges)
            var wc = ["#e63333", "#3366f2", "#33b359", "#f5b800"]
            for (var w = 0; w < 4; w = w + 1) {
                ctx.fillStyle = wc[w]
                ctx.beginPath()
                ctx.moveTo(260, 150)
                ctx.arc(260, 150, 70, w * 1.5708, w * 1.5708 + 1.5708)
                ctx.closePath()
                ctx.fill()
            }
            // zigzag
            ctx.strokeStyle = "#33b359"
            ctx.lineWidth = 2
            ctx.beginPath()
            ctx.moveTo(10, 260)
            for (var z = 0; z < 12; z = z + 1) {
                var yy = 260
                if (z % 2 == 0) { yy = 290 }
                ctx.lineTo(10 + (z + 1) * 18, yy)
            }
            ctx.stroke()
            // curves
            var cc = ["#3366f2", "#e63333", "#33b359"]
            for (var q = 0; q < 3; q = q + 1) {
                ctx.strokeStyle = cc[q]
                ctx.lineWidth = 3
                var y = 250 + q * 36
                ctx.beginPath()
                ctx.moveTo(400, y)
                ctx.bezierCurveTo(480, y - 40, 560, y + 40, 650, y - 10)
                ctx.stroke()
            }
        }
    }
}
