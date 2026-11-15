package core

import (
	"sync"
	"time"
)

// Loop is the event loop that owns all properties, signals and UI state.
// Exactly one goroutine runs it; everything else communicates via Post —
// Qt's main thread and queued connections, in Go terms.
//
// The loop itself does no pacing or vsync: the app module drives Step from
// the platform's frame callback (requestAnimationFrame on web, present on
// native), and headless tests call Step directly for deterministic
// single-frame control (DESIGN.md §8.1).
type Loop struct {
	mu      sync.Mutex
	posted  []func() // waiting for the next Step
	running []func() // the batch a Step is running; reused between steps
	quit    chan struct{}
	frame   Signal[time.Duration]
}

func NewLoop() *Loop {
	return &Loop{quit: make(chan struct{})}
}

// Post schedules fn to run on the loop goroutine before the next frame.
// Safe to call from any goroutine, the loop's own included; this is the
// only cross-goroutine entry point into the loop.
//
// Post never blocks. The queue has no limit: a bounded one would have to
// make the loop goroutine wait on itself when a callback posts, and
// dropping work silently is worse than holding it. A producer that can
// outrun the frame rate indefinitely must pace itself. Work posted after
// Quit stays queued for a Step that may still come.
func (l *Loop) Post(fn func()) {
	l.mu.Lock()
	l.posted = append(l.posted, fn)
	l.mu.Unlock()
}

// Frame is emitted once per Step with the time since the previous frame.
// Connect Process-style callbacks here.
func (l *Loop) Frame() *Signal[time.Duration] { return &l.frame }

// Step runs one frame: run the work posted before it began, advance the
// tweens and springs by dt, emit Frame, then flush property bindings so
// layout/paint observe a consistent world. Work posted while the step runs
// (by a posted callback, a Frame slot or another goroutine) waits for the
// next step, so callbacks that keep posting cannot hold a frame up.
//
// A callback that panics ends the step there, and the panic reaches the
// caller. It is not run again; the callbacks queued behind it are kept, in
// order, and run first in the next Step, so a driver that recovers loses
// no posted work. The rest of that step (animations, Frame, bindings) is
// skipped.
// (ui/shell runs its own frame loop and ticks the animations itself; it
// does not call Step.)
func (l *Loop) Step(dt time.Duration) {
	l.mu.Lock()
	batch := l.posted
	// running is given away here and taken back below, so a step cut short
	// by a panic leaves no slice that is both queue and batch.
	l.posted, l.running = l.running[:0], nil
	l.mu.Unlock()
	next := 0
	defer func() {
		if next < len(batch) {
			// A callback panicked. The ones behind it go back to the front
			// of the queue, ahead of anything posted meanwhile.
			l.mu.Lock()
			l.posted = append(append([]func(){}, batch[next:]...), l.posted...)
			l.mu.Unlock()
		}
	}()
	for next < len(batch) {
		fn := batch[next]
		batch[next] = nil // run once, and do not keep the closure alive
		next++
		fn()
	}
	l.running = batch
	TickAnimations(dt)
	l.frame.Emit(dt)
	FlushBindings()
}

// Quit makes Run return after the current frame.
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
