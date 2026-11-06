// paths_bench — a gold star drawn on a Canvas 2D context (GPUI paths_bench)
Rectangle {
    color: "#000000"
    Canvas {
        width: 700; height: 520
        onPaint: {
            var ctx = getContext("2d")
            var grad = ctx.createLinearGradient(189, 88, 189, 244)
            grad.addColorStop(0, "#fcc419")
            grad.addColorStop(1, "#f76707")
            ctx.fillStyle = grad
            ctx.beginPath()
            var cx = 189
            var cy = 166
            for (var k = 0; k < 11; k = k + 1) {
                var ang = -1.5708 + k * 0.62832
                var rr = 78
                if (k % 2 == 1) { rr = 32 }
                var px = cx + rr * Math.cos(ang)
                var py = cy + rr * Math.sin(ang)
                if (k == 0) { ctx.moveTo(px, py) } else { ctx.lineTo(px, py) }
            }
            ctx.closePath()
            ctx.fill()
        }
    }
}
