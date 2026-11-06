//go:build linux || windows

package main

// The corpus' QQuickPaintedItem subclasses — GaugeItem, WaveformItem,
// AnalogClock, DrawCanvas — as Go painters (gml.RegisterPaintedType). Each
// exposes bindable properties markup drives, and draws itself with the 2D
// renderer, which is exactly the division of labour those examples are
// demonstrating: drawing in the host language, configuration in markup.

import (
	"math"
	"strconv"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/script/gml"
	"github.com/sushvibez/gfx/ui"
)

func registerPaintedTypes() {
	registerGaugeItem()
	registerWaveformItem()
	registerAnalogClock()
	registerDrawCanvas()
}

// arcPath builds a circular arc as a stroked path (our renderer strokes
// polylines; a full circle is just the 0..2π case).
func arcPath(cx, cy, radius float32, from, to float64) *render.Path {
	p := render.NewPath()
	steps := 12 + int(math.Abs(to-from)/(math.Pi/24))
	for i := 0; i <= steps; i++ {
		a := from + (to-from)*float64(i)/float64(steps)
		p.LineTo(cx+radius*float32(math.Cos(a)), cy+radius*float32(math.Sin(a)))
	}
	return p
}

// line is a two-point path, for needles and tick marks.
func line(x0, y0, x1, y1 float32) *render.Path {
	return render.NewPath().MoveTo(x0, y0).LineTo(x1, y1)
}

func colorOf(s string, def ui.RGBA) ui.RGBA {
	if c, err := ui.ParseColor(s); err == nil {
		return c
	}
	return def
}

// registerGaugeItem draws a dial gauge: a track arc, a coloured value arc,
// tick marks, a needle, and the value plus label in the middle.
func registerGaugeItem() {
	gml.RegisterPaintedType("GaugeItem", func(emit gml.EmitFunc) *gml.PaintedObject {
		value := &core.Property[float32]{}
		minValue := &core.Property[float32]{}
		maxValue := &core.Property[float32]{}
		label := &core.Property[string]{}
		gaugeColor := &core.Property[string]{}
		maxValue.Set(100)
		gaugeColor.Set("#00D1A9")

		return &gml.PaintedObject{
			Props: map[string]any{
				"value":      gml.BindFloat(value),
				"minValue":   gml.BindFloat(minValue),
				"maxValue":   gml.BindFloat(maxValue),
				"label":      gml.BindString(label),
				"gaugeColor": gml.BindString(gaugeColor),
			},
			Draw: func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
				d := min(rect.W, rect.H)
				if d <= 0 {
					return
				}
				cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
				lw := max(d*0.07, 2)
				radius := d/2 - lw
				accent := colorOf(gaugeColor.Get(), ui.RGB(0x00d1a9))
				const start, sweep = 135.0, 270.0 // degrees, clockwise from 9:30
				rad := func(deg float64) float64 { return deg * math.Pi / 180 }

				// Track, then the value arc over it.
				r.StrokePathRound(arcPath(cx, cy, radius, rad(start), rad(start+sweep)),
					lw, ui.RGBA{R: 0.42, G: 0.44, B: 0.48, A: 0.5})
				span := maxValue.Get() - minValue.Get()
				frac := float64(0)
				if span > 0 {
					frac = float64((value.Get() - minValue.Get()) / span)
				}
				frac = math.Min(math.Max(frac, 0), 1)
				if frac > 0.001 {
					r.StrokePathRound(arcPath(cx, cy, radius, rad(start), rad(start+sweep*frac)),
						lw, accent)
				}
				// Ticks every tenth of the range.
				for i := 0; i <= 10; i++ {
					a := rad(start + sweep*float64(i)/10)
					inner := radius - lw*0.9
					outer := radius - lw*0.2
					r.StrokePath(line(
						cx+inner*float32(math.Cos(a)), cy+inner*float32(math.Sin(a)),
						cx+outer*float32(math.Cos(a)), cy+outer*float32(math.Sin(a))),
						max(d*0.012, 1), ui.RGBA{R: 0.6, G: 0.62, B: 0.66, A: 0.8})
				}
				// Needle and hub.
				na := rad(start + sweep*frac)
				r.StrokePathRound(line(cx, cy,
					cx+(radius-lw)*float32(math.Cos(na)),
					cy+(radius-lw)*float32(math.Sin(na))), max(d*0.02, 2), accent)
				r.FillPath(arcPath(cx, cy, max(d*0.045, 3), 0, 2*math.Pi).Close(), accent)

				if f == nil {
					return
				}
				// Value and label, centred under the hub.
				vf := f.AtSize(float64(max(d*0.14, 10)))
				text := formatGauge(value.Get())
				w, _ := r.MeasureText(vf, text)
				r.DrawText(vf, cx-w/2, cy+d*0.10, text, ui.RGB(0xe8e8f0))
				if label.Get() != "" {
					lf := f.AtSize(float64(max(d*0.08, 8)))
					lw2, _ := r.MeasureText(lf, label.Get())
					r.DrawText(lf, cx-lw2/2, cy+d*0.24, label.Get(), ui.RGB(0x868e96))
				}
			},
		}
	})
}

// formatGauge renders a gauge reading without a pointless decimal tail.
func formatGauge(v float32) string {
	if v == float32(int(v)) {
		return strconv.Itoa(int(v))
	}
	return strconv.FormatFloat(float64(v), 'f', 1, 64)
}

// registerWaveformItem draws a sine wave with an optional grid — frequency,
// amplitude and phase come from markup (a Timer advances the phase).
func registerWaveformItem() {
	gml.RegisterPaintedType("WaveformItem", func(emit gml.EmitFunc) *gml.PaintedObject {
		frequency := &core.Property[float32]{}
		amplitude := &core.Property[float32]{}
		phase := &core.Property[float32]{}
		lineWidth := &core.Property[float32]{}
		waveColor := &core.Property[string]{}
		showGrid := &core.Property[bool]{}
		frequency.Set(2)
		amplitude.Set(0.7)
		lineWidth.Set(2)
		waveColor.Set("#00D1A9")
		showGrid.Set(true)

		return &gml.PaintedObject{
			Props: map[string]any{
				"frequency": gml.BindFloat(frequency),
				"amplitude": gml.BindFloat(amplitude),
				"phase":     gml.BindFloat(phase),
				"lineWidth": gml.BindFloat(lineWidth),
				"waveColor": gml.BindString(waveColor),
				"showGrid":  gml.BindBool(showGrid),
			},
			Draw: func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
				if rect.W <= 0 || rect.H <= 0 {
					return
				}
				mid := rect.Y + rect.H/2
				if showGrid.Get() {
					grid := ui.RGBA{R: 0.4, G: 0.42, B: 0.46, A: 0.35}
					for i := 0; i <= 8; i++ {
						x := rect.X + rect.W*float32(i)/8
						r.StrokePath(line(x, rect.Y, x, rect.Y+rect.H), 1, grid)
					}
					for i := 0; i <= 4; i++ {
						y := rect.Y + rect.H*float32(i)/4
						r.StrokePath(line(rect.X, y, rect.X+rect.W, y), 1, grid)
					}
					r.StrokePath(line(rect.X, mid, rect.X+rect.W, mid), 1,
						ui.RGBA{R: 0.55, G: 0.57, B: 0.6, A: 0.6})
				}
				// The wave itself: one point per pixel column.
				p := render.NewPath()
				steps := int(rect.W)
				if steps < 2 {
					steps = 2
				}
				amp := amplitude.Get() * rect.H / 2 * 0.9
				for i := 0; i <= steps; i++ {
					t := float64(i) / float64(steps)
					a := 2*math.Pi*float64(frequency.Get())*t + float64(phase.Get())
					p.LineTo(rect.X+rect.W*float32(t), mid-amp*float32(math.Sin(a)))
				}
				r.StrokePathRound(p, max(lineWidth.Get(), 1),
					colorOf(waveColor.Get(), ui.RGB(0x00d1a9)))
			},
		}
	})
}

// registerAnalogClock draws a clock face. It reads the wall clock itself and
// publishes hours/minutes/seconds, which the card's digital readout binds.
func registerAnalogClock() {
	gml.RegisterPaintedType("AnalogClock", func(emit gml.EmitFunc) *gml.PaintedObject {
		hours := &core.Property[float32]{}
		minutes := &core.Property[float32]{}
		seconds := &core.Property[float32]{}
		accent := &core.Property[string]{}
		active := &core.Property[bool]{}
		accent.Set("#00D1A9")
		active.Set(true)

		// The clock owns its time: a goroutine ticks it once a second and
		// hands the write back to the UI thread.
		go func() {
			for range time.Tick(time.Second) {
				now := time.Now()
				gml.Post(func() {
					hours.Set(float32(now.Hour() % 12))
					minutes.Set(float32(now.Minute()))
					seconds.Set(float32(now.Second()))
				})
			}
		}()
		now := time.Now()
		hours.Set(float32(now.Hour() % 12))
		minutes.Set(float32(now.Minute()))
		seconds.Set(float32(now.Second()))

		return &gml.PaintedObject{
			Props: map[string]any{
				"hours":       gml.BindFloat(hours),
				"minutes":     gml.BindFloat(minutes),
				"seconds":     gml.BindFloat(seconds),
				"accentColor": gml.BindString(accent),
				"active":      gml.BindBool(active),
			},
			Draw: func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
				d := min(rect.W, rect.H)
				if d <= 0 {
					return
				}
				cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
				radius := d/2 - max(d*0.04, 2)
				acc := colorOf(accent.Get(), ui.RGB(0x00d1a9))
				// Face and rim.
				r.FillPath(arcPath(cx, cy, radius, 0, 2*math.Pi).Close(),
					ui.RGBA{R: 0.11, G: 0.12, B: 0.15, A: 1})
				r.StrokePathRound(arcPath(cx, cy, radius, 0, 2*math.Pi), max(d*0.02, 2), acc)
				// Hour marks.
				for i := 0; i < 12; i++ {
					a := float64(i) * math.Pi / 6
					long := i%3 == 0
					inner := radius * 0.82
					if long {
						inner = radius * 0.74
					}
					w := max(d*0.012, 1)
					if long {
						w = max(d*0.022, 2)
					}
					r.StrokePath(line(
						cx+inner*float32(math.Sin(a)), cy-inner*float32(math.Cos(a)),
						cx+radius*0.92*float32(math.Sin(a)), cy-radius*0.92*float32(math.Cos(a))),
						w, ui.RGB(0x9aa0ac))
				}
				// Hands: hour and minute advance smoothly, like a real clock.
				h := float64(hours.Get()) + float64(minutes.Get())/60
				m := float64(minutes.Get()) + float64(seconds.Get())/60
				hand := func(frac float64, length, width float32, c ui.RGBA) {
					a := frac * 2 * math.Pi
					r.StrokePathRound(line(cx, cy,
						cx+length*float32(math.Sin(a)), cy-length*float32(math.Cos(a))),
						width, c)
				}
				hand(h/12, radius*0.5, max(d*0.03, 3), ui.RGB(0xe8e8f0))
				hand(m/60, radius*0.72, max(d*0.02, 2), ui.RGB(0xe8e8f0))
				hand(float64(seconds.Get())/60, radius*0.82, max(d*0.01, 1), acc)
				r.FillPath(arcPath(cx, cy, max(d*0.035, 3), 0, 2*math.Pi).Close(), acc)
			},
		}
	})
}

// registerDrawCanvas is the freehand sketch pad: strokes accumulate as the
// pointer drags across it, clear() empties them, and strokeCount reports.
// Pointer input arrives through the element's own hover/press state, which
// markup exposes to us as property writes from its MouseArea — so the card
// drives `penDown`/`penX`/`penY` and we record the path.
func registerDrawCanvas() {
	gml.RegisterPaintedType("DrawCanvas", func(emit gml.EmitFunc) *gml.PaintedObject {
		penColor := &core.Property[string]{}
		penWidth := &core.Property[float32]{}
		strokeCount := &core.Property[float32]{}
		penDown := &core.Property[bool]{}
		penX := &core.Property[float32]{}
		penY := &core.Property[float32]{}
		penColor.Set("#00D1A9")
		penWidth.Set(3)

		type stroke struct {
			color ui.RGBA
			width float32
			pts   []core.Vec2
		}
		var strokes []stroke

		// Record points while the pen is down, starting a stroke on each
		// press. (A demo squiggle is drawn until the first stroke exists,
		// so the item is never a blank rectangle.)
		record := func() {
			p := core.Vec2{X: penX.Get(), Y: penY.Get()}
			if !penDown.Get() {
				return
			}
			if len(strokes) == 0 || len(strokes[len(strokes)-1].pts) == 0 {
				strokes = append(strokes, stroke{
					color: colorOf(penColor.Get(), ui.RGB(0x00d1a9)),
					width: max(penWidth.Get(), 1),
					pts:   []core.Vec2{p},
				})
				strokeCount.Set(float32(len(strokes)))
				return
			}
			last := &strokes[len(strokes)-1]
			last.pts = append(last.pts, p)
		}
		penX.Changed().Connect(func(float32) { record() })
		penY.Changed().Connect(func(float32) { record() })
		penDown.Changed().Connect(func(down bool) {
			if down {
				strokes = append(strokes, stroke{
					color: colorOf(penColor.Get(), ui.RGB(0x00d1a9)),
					width: max(penWidth.Get(), 1),
				})
				strokeCount.Set(float32(len(strokes)))
			}
		})

		return &gml.PaintedObject{
			Props: map[string]any{
				"penColor":    gml.BindString(penColor),
				"penWidth":    gml.BindFloat(penWidth),
				"strokeCount": gml.BindFloat(strokeCount),
				"penDown":     gml.BindBool(penDown),
				"penX":        gml.BindFloat(penX),
				"penY":        gml.BindFloat(penY),
			},
			Methods: map[string]any{
				"clear": func([]any) (any, error) {
					strokes = nil
					strokeCount.Set(0)
					return nil, nil
				},
			},
			Draw: func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
				if rect.W <= 0 || rect.H <= 0 {
					return
				}
				r.FillRoundRect(rect.X, rect.Y, rect.W, rect.H, 6,
					ui.RGBA{R: 0.07, G: 0.08, B: 0.1, A: 1})
				if len(strokes) == 0 {
					// Nothing drawn yet: a faint guide squiggle, so the pad
					// reads as a drawing surface rather than an empty box.
					p := render.NewPath()
					for i := 0; i <= 60; i++ {
						t := float64(i) / 60
						p.LineTo(rect.X+rect.W*float32(t),
							rect.Y+rect.H/2-rect.H*0.22*float32(math.Sin(t*4*math.Pi)))
					}
					r.StrokePathRound(p, 2, ui.RGBA{R: 0.4, G: 0.42, B: 0.46, A: 0.4})
					return
				}
				for _, s := range strokes {
					if len(s.pts) < 2 {
						continue
					}
					p := render.NewPath()
					for i, pt := range s.pts {
						if i == 0 {
							p.MoveTo(rect.X+pt.X, rect.Y+pt.Y)
						} else {
							p.LineTo(rect.X+pt.X, rect.Y+pt.Y)
						}
					}
					r.StrokePathRound(p, s.width, s.color)
				}
			},
		}
	})
}
