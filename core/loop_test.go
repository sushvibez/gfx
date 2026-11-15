package core

import (
	"sync"
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

// A callback on the loop goroutine may post more work than any fixed queue
// holds: Post never waits for room, since the only goroutine that could
// make room is the one posting.
func TestLoopPostFromTheLoopNeverBlocks(t *testing.T) {
	l := NewLoop()
	const n = 1000
	ran := 0
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		l.Post(func() {
			for range n {
				l.Post(func() { ran++ })
			}
		})
		l.Step(0)
		l.Step(0)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		l.Quit() // unblock the stuck Post so the goroutine ends
		t.Fatal("Post from a posted callback blocked the loop")
	}
	if ran != n {
		t.Fatalf("%d of %d posted callbacks ran", ran, n)
	}
}

// The same from a Frame callback.
func TestLoopPostFromAFrameCallbackNeverBlocks(t *testing.T) {
	l := NewLoop()
	const n = 1000
	ran := 0
	c := l.Frame().Connect(func(time.Duration) {
		for range n {
			l.Post(func() { ran++ })
		}
	})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		l.Step(0)
		c.Disconnect()
		l.Step(0)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		l.Quit()
		t.Fatal("Post from a Frame callback blocked the loop")
	}
	if ran != n {
		t.Fatalf("%d of %d posted callbacks ran", ran, n)
	}
}

// Work that reposts itself cannot hold a frame up: a Step runs what was
// queued when it began and leaves the rest for the next one.
func TestLoopStepRunsABoundedSnapshot(t *testing.T) {
	l := NewLoop()
	runs, frames := 0, 0
	var again func()
	again = func() {
		runs++
		if runs < 100000 { // far more than a test waits for, were it one frame
			l.Post(again)
		}
	}
	l.Post(again)
	l.Frame().Connect(func(time.Duration) { frames++ })
	for range 3 {
		l.Step(0)
	}
	if frames != 3 || runs != 3 {
		t.Fatalf("after 3 steps: %d frames, %d runs of the reposting callback; want 3 and 3", frames, runs)
	}
}

// Many goroutines post at once; every callback runs once, on the loop.
func TestLoopConcurrentProducers(t *testing.T) {
	l := NewLoop()
	const producers, each = 8, 500
	ran := 0
	var wg sync.WaitGroup
	for range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				l.Post(func() { ran++ })
			}
		}()
	}
	sent := make(chan struct{})
	go func() { wg.Wait(); close(sent) }()
	deadline := time.After(10 * time.Second)
	for done := false; !done; {
		l.Step(0)
		select {
		case <-sent:
			done = true
		case <-deadline:
			l.Quit()
			t.Fatal("producers never finished posting")
		default:
		}
	}
	l.Step(0)
	if ran != producers*each {
		t.Fatalf("%d callbacks ran, want %d", ran, producers*each)
	}
}

// A Post after Quit returns at once; the work stays queued for a Step
// that may still come.
func TestLoopPostAfterQuit(t *testing.T) {
	l := NewLoop()
	l.Quit()
	ran := 0
	returned := make(chan struct{})
	go func() {
		for range 1000 {
			l.Post(func() { ran++ })
		}
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Post blocked after Quit")
	}
	l.Step(0)
	if ran != 1000 {
		t.Fatalf("%d callbacks ran in the Step after Quit, want 1000", ran)
	}
}
