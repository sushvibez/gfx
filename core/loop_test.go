package core

import (
	"testing"
	"time"
)

func TestLoopStepRunsPostedThenFrame(t *testing.T) {
	l := NewLoop()
	var order []string
	l.Post(func() { order = append(order, "posted") })
	l.Frame().Connect(func(time.Duration) { order = append(order, "frame") })
	l.Step(time.Millisecond)
	if len(order) != 2 || order[0] != "posted" || order[1] != "frame" {
		t.Fatalf("order = %v", order)
	}
}

func TestLoopStepFlushesBindings(t *testing.T) {
	l := NewLoop()
	var a, b Property[int]
	evals := 0
	b.Bind(func() int { evals++; return a.Get() })
	a.Set(7)
	l.Step(0)
	if evals != 2 { // initial + flush
		t.Fatalf("evals = %d, want 2", evals)
	}
	if b.Get() != 7 {
		t.Fatal("binding not flushed by Step")
	}
}

func TestLoopPostFromOtherGoroutine(t *testing.T) {
	l := NewLoop()
	done := make(chan struct{})
	go l.Post(func() { close(done) })
	deadline := time.After(time.Second)
	for {
		l.Step(0)
		select {
		case <-done:
			return
		case <-deadline:
			t.Fatal("posted func never ran")
		default:
		}
	}
}

func TestLoopRunQuit(t *testing.T) {
	l := NewLoop()
	frames := 0
	l.Frame().Connect(func(time.Duration) {
		frames++
		if frames == 3 {
			l.Quit()
		}
	})
	finished := make(chan struct{})
	go func() { l.Run(240); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after Quit")
	}
	if !l.Quitting() {
		t.Fatal("Quitting() should be true")
	}
}
