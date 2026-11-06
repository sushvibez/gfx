//go:build linux || windows || js

// Pong — a minimal real-time game on the 2D renderer, showing that the stack
// has what a game needs: a continuous frame loop with real delta-time, held-key
// input, and immediate-mode drawing (rects + text).
//
//	go run ./examples/pong
//
// Controls: left paddle = W / S, right paddle = Up / Down, Space to serve,
// R to reset the score, Esc to quit.
package main

import (
	"flag"
	"fmt"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui/shell"
)

func rgb(hex uint32) render.RGBA {
	return render.RGBA{R: float32(hex>>16&0xff) / 255, G: float32(hex>>8&0xff) / 255, B: float32(hex&0xff) / 255, A: 1}
}

const (
	paddleW   = 14
	paddleH   = 96
	ballSize  = 14
	paddleSpd = 460 // px/sec
	margin    = 36
)

type game struct {
	started                      bool
	ballX, ballY, ballVX, ballVY float32
	leftY, rightY                float32
	scoreL, scoreR               int
	w, h                         float32
}

func (g *game) reset(serveDir float32) {
	g.ballX, g.ballY = g.w/2, g.h/2
	speed := float32(360)
	g.ballVX = speed * serveDir
	g.ballVY = speed * 0.45
}

func (g *game) update(f *shell.Frame) bool {
	w, h := float32(f.W), float32(f.H)
	dt := float32(f.Dt)
	first := g.w == 0
	g.w, g.h = w, h
	if first {
		g.leftY, g.rightY = h/2-paddleH/2, h/2-paddleH/2
		g.reset(1)
	}

	if f.In.Down(app.KeyEscape) {
		return false
	}
	if f.In.Pressed(app.KeyNone, 'r') {
		g.scoreL, g.scoreR = 0, 0
	}
	if !g.started && f.In.Pressed(app.KeyNone, ' ') {
		g.started = true
	}

	// Paddle movement (held keys).
	if f.In.Rune('w') {
		g.leftY -= paddleSpd * dt
	}
	if f.In.Rune('s') {
		g.leftY += paddleSpd * dt
	}
	if f.In.Down(app.KeyUp) {
		g.rightY -= paddleSpd * dt
	}
	if f.In.Down(app.KeyDown) {
		g.rightY += paddleSpd * dt
	}
	g.leftY = clamp(g.leftY, 0, h-paddleH)
	g.rightY = clamp(g.rightY, 0, h-paddleH)

	// Ball physics.
	if g.started {
		g.ballX += g.ballVX * dt
		g.ballY += g.ballVY * dt
		// Top / bottom walls.
		if g.ballY <= 0 {
			g.ballY = 0
			g.ballVY = -g.ballVY
		}
		if g.ballY+ballSize >= h {
			g.ballY = h - ballSize
			g.ballVY = -g.ballVY
		}
		// Left paddle.
		if g.ballX <= margin+paddleW && g.ballX >= margin &&
			g.ballY+ballSize >= g.leftY && g.ballY <= g.leftY+paddleH && g.ballVX < 0 {
			g.ballX = margin + paddleW
			g.bounce(g.leftY)
		}
		// Right paddle.
		rx := w - margin - paddleW
		if g.ballX+ballSize >= rx && g.ballX+ballSize <= rx+paddleW &&
			g.ballY+ballSize >= g.rightY && g.ballY <= g.rightY+paddleH && g.ballVX > 0 {
			g.ballX = rx - ballSize
			g.bounce(g.rightY)
		}
		// Scoring.
		if g.ballX < -ballSize {
			g.scoreR++
			g.started = false
			g.reset(1)
		}
		if g.ballX > w {
			g.scoreL++
			g.started = false
			g.reset(-1)
		}
	} else {
		g.ballX, g.ballY = w/2, h/2
	}

	g.draw(f)
	return true
}

// bounce reflects the ball off a paddle and adds spin based on where it hit.
func (g *game) bounce(paddleY float32) {
	g.ballVX = -g.ballVX * 1.04 // speed up slightly each hit
	offset := (g.ballY + ballSize/2 - (paddleY + paddleH/2)) / (paddleH / 2)
	g.ballVY = offset * 320
}

func (g *game) draw(f *shell.Frame) {
	w, h := float32(f.W), float32(f.H)
	white := rgb(0xf1f3f5)
	dim := rgb(0x3a3f52)
	// Center net (dashed).
	for y := float32(8); y < h; y += 30 {
		f.G.FillRect(w/2-2, y, 4, 16, dim)
	}
	// Paddles + ball.
	f.G.FillRect(margin, g.leftY, paddleW, paddleH, white)
	f.G.FillRect(w-margin-paddleW, g.rightY, paddleW, paddleH, white)
	f.G.Quad(g.ballX, g.ballY, ballSize, ballSize, white, ballSize/2, 0, white)
	// Score.
	f.G.DrawText(f.Font, w/2-90, 28, fmt.Sprintf("%d", g.scoreL), white)
	f.G.DrawText(f.Font, w/2+70, 28, fmt.Sprintf("%d", g.scoreR), white)
	if !g.started {
		f.G.DrawText(f.Font, w/2-120, h-40, "press SPACE to serve  ·  W/S  ↑/↓", dim)
	}
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func main() {
	shot := flag.String("screenshot", "", "render a scripted frame headlessly and write a PNG")
	flag.Parse()
	g := &game{}
	opts := shell.Options{Title: "Pong", Width: 800, Height: 500, ClearHex: 0x0d0d12}
	if *shot != "" {
		// Serve on frame 0, hold the right paddle down, step ~70 frames so the
		// ball is mid-rally for a representative capture.
		prep := func(step int, in *shell.Input) {
			if step == 0 {
				in.SetEvents(app.KeyEvent{Down: true, Rune: ' '})
			}
			in.SetDown(app.KeyDown, true)
			in.SetRune('w', true)
		}
		if err := shell.ScreenshotGame(opts, 70, 1.0/60, g.update, prep, *shot); err != nil {
			panic(err)
		}
		return
	}
	if err := shell.RunGame(opts, g.update); err != nil {
		panic(err)
	}
}
