package gml

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// The execution budget.
//
// A page's script runs on the UI goroutine, inside a frame. The call-depth
// limit (maxCallDepth) keeps it off the end of the Go stack, but depth is
// not work: `while (true) {}` never calls anything, two functions can call
// each other a billion times without going deep, and a loop cap counted
// per loop says nothing about loops inside loops. So every entry into the
// evaluator runs under one shared budget, counted in STEPS — a loop
// iteration or a function call, the two things script can repeat without
// writing more source — and, because one step can be slow (an array
// copied on every push), in wall-clock time as well.
//
// One ACTIVATION is the outermost thing running: a handler's statements, a
// block binding, a call of a user function from Go, or a loop statement
// run on its own. Everything nested inside it — its loops, the functions
// it calls, their callbacks — draws on the same budget, and the budget is
// new for the next activation. Once it runs out every further step fails
// too, so a `catch` cannot swallow the error and carry on, and a `finally`
// cannot loop in its place.
//
// The state is process-wide and atomic: the engine belongs to one
// goroutine at a time, and a second document on another goroutine must
// share the budget rather than corrupt it. The steps left and the call
// depth live in ONE word (budgetState), so that a call — which already
// paid one atomic add on the way in and one on the way out for its depth
// — pays nothing more for its step; a loop charges its iterations
// budgetLoopBatch at a time. The clock and the interrupt flag are read
// every budgetCheckEvery steps, and the clock is not read at all by an
// activation shorter than that (most are a few steps: `Style.resize` is
// called for nearly every size on a page).

// Budget is how much script one entry into the evaluator may run. The
// zero value is the default budget.
type Budget struct {
	// Steps is the number of loop iterations plus function calls: 0 for
	// the default (DefaultBudgetSteps), negative for no limit.
	Steps int64
	// Time is the wall-clock time: 0 for the default (DefaultBudgetTime),
	// negative for no limit.
	Time time.Duration
}

// The default budget. Ten million steps is the cap a single loop used to
// have, now shared by everything one activation runs: about a second of
// empty loop, several of real work. Loading and laying out every page of
// the QML corpus and the .gml examples comes out the same under a budget
// of 2 000 steps (TestCorpusSweep with GFX_SWEEP_STEPS), so the default is
// some thousands of times what a page's bindings and completion handlers
// use; canvas painters and click handlers are not part of that sweep. Ten
// seconds is for what steps do not measure, and is long because a
// wall-clock limit is the one that depends on the machine: it must not
// fire on a slow phone doing honest work.
const (
	DefaultBudgetSteps = 10_000_000
	DefaultBudgetTime  = 10 * time.Second
)

// ErrBudget is the error (under errors.Is) of a script that ran out of
// its budget; ErrInterrupted that of one stopped by Interrupt.
var (
	ErrBudget      = errors.New("gml: script ran out of its execution budget")
	ErrInterrupted = errors.New("gml: script interrupted")
)

// SetBudget sets the budget every later activation runs under, for every
// document in the process, and returns the one it replaces. Call it from
// the goroutine that runs the documents.
func SetBudget(b Budget) Budget {
	old := Budget{Steps: budgetSet.steps.Load(), Time: time.Duration(budgetSet.time.Load())}
	budgetSet.steps.Store(b.Steps)
	budgetSet.time.Store(int64(b.Time))
	// The budget is filled when an activation ends, ready for the next
	// (budgetRefill); with nothing running, that is now.
	if budgetState.Load()&budgetDepthMask == 0 && budgetNest.Load() == 0 {
		budgetRefill()
	}
	return old
}

// Interrupt stops the script that is running now: its next budget check
// fails with ErrInterrupted. It is safe to call from any goroutine — a
// watchdog, a Stop button's handler on another thread. It does nothing to
// a script that starts later.
func Interrupt() { budgetStop.Store(true) }

const budgetCheckEvery = 4096 // steps between clock and interrupt checks; a power of two

// budgetState packs the running activation's two counters into one word:
// the steps it has left in the high bits, the depth of user-function calls
// in the low budgetDepthBits. Entering a call adds budgetCall — one step
// less, one call deeper — in a single atomic add. Steps below zero mean
// the budget has run out: every step fails from then on.
const (
	budgetDepthBits = 16 // maxCallDepth fits with room to spare
	budgetDepthMask = 1<<budgetDepthBits - 1
	budgetOneStep   = 1 << budgetDepthBits
	budgetCall      = 1 - budgetOneStep
	// budgetNoLimit is the step count that stands for "no limit": years
	// of evaluation, and still far from the top of the word.
	budgetNoLimit = 1 << 45
	// budgetSpend, added to the state, puts the steps below zero for
	// good: more than any budget holds, less than the word does.
	budgetSpend = -(1 << 62)
)

var (
	// budgetSet is the budget as the host set it (zero for the defaults).
	budgetSet   struct{ steps, time atomic.Int64 }
	budgetState atomic.Int64
	// budgetNest counts the handlers, block bindings and loop statements
	// being run; with the call depth it tells the outermost one from the
	// rest.
	budgetNest atomic.Int32
	// budgetStart is when the running activation first read the clock
	// (UnixNano), 0 before that.
	budgetStart atomic.Int64
	// budgetStop is the flag Interrupt sets.
	budgetStop atomic.Bool
	// budgetWhy is why the running activation ran out (a budgetReason).
	budgetWhy atomic.Int32
	// budgetSlow records that the running activation reached budgetCheck,
	// so budgetStart and budgetWhy are to be reset when it ends.
	budgetSlow atomic.Bool
)

func init() { budgetRefill() }

// callDepth reads the depth of user-function calls out of budgetState.
var callDepth callDepthView

type callDepthView struct{}

func (callDepthView) Load() int32 { return int32(budgetState.Load() & budgetDepthMask) }

type budgetReason int32

const (
	budgetOK budgetReason = iota
	budgetSteps
	budgetTime
	budgetInterrupted
)

// budgetError is the error of a spent budget. It unwraps to ErrBudget or
// ErrInterrupted.
type budgetError struct {
	why   budgetReason
	limit Budget
}

func (e *budgetError) Error() string {
	switch e.why {
	case budgetTime:
		return fmt.Sprintf("gml: script ran longer than %v (a loop that does not end?)", e.limit.Time)
	case budgetInterrupted:
		return ErrInterrupted.Error()
	}
	return fmt.Sprintf("gml: script ran more than %d loop iterations and calls (a loop that does not end?)", e.limit.Steps)
}

func (e *budgetError) Unwrap() error {
	if e.why == budgetInterrupted {
		return ErrInterrupted
	}
	return ErrBudget
}

// budgetLimits is the budget in force, defaults filled in.
func budgetLimits() Budget {
	b := Budget{Steps: budgetSet.steps.Load(), Time: time.Duration(budgetSet.time.Load())}
	if b.Steps == 0 {
		b.Steps = DefaultBudgetSteps
	}
	if b.Time == 0 {
		b.Time = DefaultBudgetTime
	}
	return b
}

// budgetRefill ends the outermost activation: the budget is made whole
// for the next one. Filling it at the end rather than the start is what
// lets a top-level call — the commonest activation by far — begin with
// the one atomic add it needs anyway.
func budgetRefill() {
	steps := budgetSet.steps.Load()
	switch {
	case steps == 0:
		steps = DefaultBudgetSteps
	case steps < 0 || steps > budgetNoLimit:
		steps = budgetNoLimit // never reached; the periodic checks still run
	}
	budgetState.Store(steps << budgetDepthBits)
	// Only an activation long enough to reach the slow path has clock
	// state to put back.
	if budgetSlow.Load() {
		budgetSlow.Store(false)
		budgetStart.Store(0)
		budgetWhy.Store(int32(budgetOK))
	}
}

// budgetBegin starts the outermost activation. The budget is already full
// (budgetRefill); what is left is that an interrupt is for the script
// that was running when it was sent, not for this one.
func budgetBegin() {
	if budgetStop.Load() { // read before written: a load is a plain read
		budgetStop.Store(false)
	}
}

// budgetEnter marks a handler, block binding or loop statement as running
// and starts a budget if nothing else is; budgetLeave undoes it.
func budgetEnter() {
	if budgetNest.Add(1) == 1 && callDepth.Load() == 0 {
		budgetBegin()
	}
}

func budgetLeave() {
	if budgetNest.Add(-1) == 0 && callDepth.Load() == 0 {
		budgetRefill()
	}
}

// budgetLoopBatch is how many iterations a loop runs between charges. A
// loop counts its own iterations and charges them budgetLoopBatch at a
// time, and the remainder when it ends (budgetLoopEnd), so the count is
// exact and an iteration costs no atomic operation of its own.
const budgetLoopBatch = 64 // divides budgetCheckEvery

// budgetLoopStep is called once per iteration with the loop's own counter.
func budgetLoopStep(n *int) error {
	*n++
	if *n&(budgetLoopBatch-1) != 0 {
		return nil
	}
	left := budgetState.Add(-budgetLoopBatch*budgetOneStep) >> budgetDepthBits
	// Checked when the batch reached or crossed a multiple of the check
	// interval, and always once the budget is gone.
	if left < 0 || (left+budgetLoopBatch)/budgetCheckEvery != left/budgetCheckEvery || left%budgetCheckEvery == 0 {
		return budgetCheck(left)
	}
	return nil
}

// budgetLoopEnd charges the iterations a finished loop has not charged
// yet and closes its activation (budgetLeave).
func budgetLoopEnd(n int) {
	if rest := int64(n & (budgetLoopBatch - 1)); rest != 0 {
		budgetState.Add(-rest * budgetOneStep)
	}
	budgetLeave()
}

// budgetCheck is the slow path of a step: the budget has run out, or it
// is time to look at the clock and the interrupt flag.
func budgetCheck(left int64) error {
	budgetSlow.Store(true)
	why := budgetReason(budgetWhy.Load())
	if why == budgetOK {
		switch {
		case left <= 0:
			why = budgetSteps
		case budgetStop.Load():
			why = budgetInterrupted
		default:
			limit := budgetSet.time.Load()
			if limit == 0 {
				limit = int64(DefaultBudgetTime)
			}
			if limit < 0 {
				return nil
			}
			now := time.Now().UnixNano()
			start := budgetStart.Load()
			if start == 0 {
				budgetStart.Store(now)
				return nil
			}
			if now-start <= limit {
				return nil
			}
			why = budgetTime
		}
		// Spent for good (the call depth in the low bits is untouched).
		budgetWhy.Store(int32(why))
		budgetState.Add(budgetSpend)
	}
	return &budgetError{why: why, limit: budgetLimits()}
}

// budgetSpent reports whether the running activation has run out — which
// a `catch` must not swallow.
func budgetSpent() bool { return budgetState.Load() < 0 }
