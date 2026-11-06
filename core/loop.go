package core

import "time"

// Loop is the event loop that owns all properties, signals and UI state.
// Exactly one goroutine runs it; everything else communicates via Post —
// Qt's main thread and queued connections, in Go terms.
//
// The loop itself does no pacing or vsync: the app module drives Step from
// the platform's frame callback (requestAnimationFrame on web, present on
// native), and headless tests call Step directly for deterministic
// single-frame control (DESIGN.md §8.1).
type Loop struct {
	posted chan func()
	quit   chan struct{}
	frame  Signal[time.Duration]
}

func NewLoop() *Loop {
	return &Loop{
		posted: make(chan func(), 256),
		quit:   make(chan struct{}),
	}
}

// Post schedules fn to run on the loop goroutine before the next frame.
// Safe to call from any goroutine; this is the only cross-goroutine entry
// point into the loop.
func (l *Loop) Post(fn func()) {
	select {
	case l.posted <- fn:
	case <-l.quit:
	}
}

// Frame is emitted once per Step with the time since the previous frame.
// Connect Process-style callbacks here.
func (l *Loop) Frame() *Signal[time.Duration] { return &l.frame }

// Step runs one frame: drain posted work, emit Frame, then flush property
// bindings so layout/paint observe a consistent world.
func (l *Loop) Step(dt time.Duration) {
	for {
		select {
		case fn := <-l.posted:
			fn()
		default:
			l.frame.Emit(dt)
			FlushBindings()
			return
		}
	}
}

// Quit makes Run return after the current frame and unblocks pending Posts.
func (l *Loop) Quit() {
	select {
	case <-l.quit:
	default:
		close(l.quit)
	}
}

// Quitting reports whether Quit has been called.
func (l *Loop) Quitting() bool {
	select {
	case <-l.quit:
		return true
	default:
		return false
	}
}

// Run steps the loop at the given rate until Quit. This is the fallback
// driver for headless servers and tests; windowed platforms pace the loop
// themselves via Step.
func (l *Loop) Run(hz int) {
	if hz <= 0 {
		hz = 60
	}
	tick := time.NewTicker(time.Second / time.Duration(hz))
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-l.quit:
			return
		case now := <-tick.C:
			l.Step(now.Sub(last))
			last = now
		}
	}
}
