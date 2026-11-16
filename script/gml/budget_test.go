package gml

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// runBudgeted loads an Item whose body is the given declarations, calls
// its function run() under budget b, and returns the call's result and
// error, and how long it took.
func runBudgeted(t *testing.T, b Budget, decls string) (any, error, time.Duration) {
	t.Helper()
	d, err := LoadString("Item {\n"+decls+"\n}", NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	fn, ok := d.env.Get("run")
	if !ok {
		t.Fatal("run() not declared")
	}
	defer SetBudget(SetBudget(b))
	start := time.Now()
	v, err := callValue(fn, nil, d.env, 0, "run")
	return v, err, time.Since(start)
}

// Runaway script ends with an error, whatever shape it has: one loop,
// loops inside loops, functions calling each other without ever going
// deep, a callback, a catch that swallows everything.
func TestBudgetStopsRunawayScript(t *testing.T) {
	small := Budget{Steps: 50_000, Time: -1}
	for _, c := range []struct{ name, decls string }{
		{"while true", `function run() { while (true) { } }`},
		{"for ever", `function run() { for (;;) { } }`},
		{"nested loops", `function run() {
            var n = 0
            for (var i = 0; i < 10000; i++) { for (var j = 0; j < 10000; j++) { n++ } }
            return n
        }`},
		{"many short loops", `function run() {
            var n = 0
            while (true) { for (var j = 0; j < 10; j++) { n++ } }
        }`},
		{"mutual recursion below the depth limit", `
        function a(n) { if (n > 0) { b(n - 1); b(n - 1) } }
        function b(n) { if (n > 0) { a(n - 1); a(n - 1) } }
        function run() { a(40) }`},
		{"repeated calls", `
        function f() { return 1 }
        function run() { var n = 0; while (true) { n += f() } }`},
		{"for of", `function run() {
            var xs = [1, 2, 3]
            while (true) { for (var x of xs) { } }
        }`},
		{"callback", `function run() { [1, 2, 3].forEach(function(x) { while (true) { } }) }`},
		{"map callbacks", `function run() {
            var xs = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
            while (true) { xs = xs.map(x => x + 1) }
        }`},
		{"catch cannot swallow it", `function run() {
            while (true) { try { while (true) { } } catch (e) { } }
        }`},
		{"finally cannot outlive it", `function run() {
            try { while (true) { } } finally { while (true) { } }
        }`},
		{"recursion caught and retried", `
        function f() { try { f() } catch (e) { f() } }
        function run() { f() }`},
	} {
		_, err, took := runBudgeted(t, small, c.decls)
		if err == nil {
			t.Errorf("%s: no error", c.name)
			continue
		}
		if !errors.Is(err, ErrBudget) {
			t.Errorf("%s: error %q is not ErrBudget", c.name, err)
		}
		if took > 5*time.Second {
			t.Errorf("%s: took %v", c.name, took)
		}
	}
	if n := callDepth.Load(); n != 0 {
		t.Errorf("the call depth is %d after the calls returned", n)
	}
	if n := budgetNest.Load(); n != 0 {
		t.Errorf("the activation depth is %d after the calls returned", n)
	}
}

// The budget is per entry into the evaluator: a script that ran out does
// not take the next one's budget with it, and work well inside the budget
// is untouched.
func TestBudgetIsPerActivation(t *testing.T) {
	b := Budget{Steps: 50_000, Time: -1}
	if _, err, _ := runBudgeted(t, b, `function run() { while (true) { } }`); err == nil {
		t.Fatal("the runaway loop returned")
	}
	for i := 0; i < 3; i++ {
		v, err, _ := runBudgeted(t, b, `function run() {
            var n = 0
            for (var i = 0; i < 20000; i++) { n += 2 }
            return n
        }`)
		if err != nil || v != 40000.0 {
			t.Fatalf("run %d after a runaway: %v, %v", i, v, err)
		}
	}
	// Each handler statement run is one activation, so two loops in a row
	// share a budget: 30 000 + 30 000 steps is over 50 000.
	_, err, _ := runBudgeted(t, b, `function run() {
        for (var i = 0; i < 30000; i++) { }
        for (var j = 0; j < 30000; j++) { }
    }`)
	if !errors.Is(err, ErrBudget) {
		t.Errorf("two loops of 30 000 under a budget of 50 000: %v", err)
	}
}

// A deadline stops a script the step count would let run, and a host can
// stop one from another goroutine.
func TestBudgetDeadlineAndInterrupt(t *testing.T) {
	_, err, took := runBudgeted(t, Budget{Steps: -1, Time: 50 * time.Millisecond},
		`function run() { while (true) { } }`)
	if !errors.Is(err, ErrBudget) || !strings.Contains(err.Error(), "longer than") {
		t.Errorf("deadline: err = %v", err)
	}
	if took < 50*time.Millisecond || took > 5*time.Second {
		t.Errorf("deadline of 50ms: took %v", took)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(30 * time.Millisecond)
		Interrupt()
	}()
	_, err, took = runBudgeted(t, Budget{Steps: -1, Time: -1}, `function run() { while (true) { } }`)
	<-done
	if !errors.Is(err, ErrInterrupted) {
		t.Errorf("interrupt: err = %v", err)
	}
	if took > 5*time.Second {
		t.Errorf("interrupt: took %v", took)
	}
	// An interrupt is for the script that was running: the next one runs.
	if v, err, _ := runBudgeted(t, Budget{}, `function run() { return 7 }`); err != nil || v != 7.0 {
		t.Errorf("after an interrupt: %v, %v", v, err)
	}
	// And one sent while nothing runs does not stop what starts later.
	Interrupt()
	v, err, _ := runBudgeted(t, Budget{}, `function run() {
        var n = 0
        for (var i = 0; i < 20000; i++) { n++ }
        return n
    }`)
	if err != nil || v != 20000.0 {
		t.Errorf("after an interrupt with nothing running: %v, %v", v, err)
	}
}

// A runaway binding is an error from the page too, not only a handler.
func TestBudgetBoundsBindings(t *testing.T) {
	defer SetBudget(SetBudget(Budget{Steps: 50_000, Time: -1}))
	d, err := LoadString(`Item {
    property var result: (function() { while (true) { } })()
    property var block: { var n = 0; while (true) { n++ } return n }
}`, NewEnv())
	if err == nil {
		if got := d.Property("result"); got != nil {
			t.Errorf("a runaway binding produced %v", got)
		}
		if got := d.Property("block"); got != nil {
			t.Errorf("a runaway block binding produced %v", got)
		}
	}
}

// The default budget: generous enough for real work, and still an end to
// `while (true) {}` with nothing set by the host.
func TestBudgetDefault(t *testing.T) {
	if b := SetBudget(Budget{}); b != (Budget{}) {
		SetBudget(b)
		t.Fatalf("the budget at rest is %+v, want the default", b)
	}
	v, err, _ := runBudgeted(t, Budget{}, `function run() {
        var n = 0
        for (var i = 0; i < 1000; i++) { for (var j = 0; j < 300; j++) { n++ } }
        return n
    }`)
	if err != nil || v != 300000.0 {
		t.Fatalf("300 000 iterations under the default budget: %v, %v", v, err)
	}
	if testing.Short() {
		t.Skip("the default budget takes seconds to run out")
	}
	_, err, took := runBudgeted(t, Budget{}, `function run() { while (true) { } }`)
	if !errors.Is(err, ErrBudget) {
		t.Errorf("while (true) {} under the default budget: %v", err)
	}
	t.Logf("the default budget ran out after %v", took)
}
