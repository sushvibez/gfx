package gml

import (
	"errors"
	"strings"
	"testing"
)

// A host function with a mixed parameter list is adapted at registration:
// markup values convert per parameter, results and errors come back.
func TestHostFuncAdapted(t *testing.T) {
	env := NewEnv()
	var (
		gotKind string
		gotIdx  int
		gotW    float32
		gotOn   bool
		gotAny  any
	)
	env.Func("set", func(kind string, idx int, w float32, on bool, v any) {
		gotKind, gotIdx, gotW, gotOn, gotAny = kind, idx, w, on, v
	})
	env.Func("label", func(name string, n int) string { return name + "#" + str(float64(n)) })
	env.Func("check", func(ok bool) (int, error) {
		if !ok {
			return 0, errors.New("refused")
		}
		return 7, nil
	})

	evalStr(t, env, `set("wall", 3, 1.5, true, null)`)
	if gotKind != "wall" || gotIdx != 3 || gotW != 1.5 || !gotOn || gotAny != nil {
		t.Errorf("set got (%q, %d, %v, %v, %#v)", gotKind, gotIdx, gotW, gotOn, gotAny)
	}
	evalStr(t, env, `set(12, "4", "2.25", 0, "x")`)
	if gotKind != "12" || gotIdx != 4 || gotW != 2.25 || gotOn || gotAny != "x" {
		t.Errorf("converted set got (%q, %d, %v, %v, %#v)", gotKind, gotIdx, gotW, gotOn, gotAny)
	}
	if s := evalStr(t, env, `label("hostile", 2)`); s != "hostile#2" {
		t.Errorf(`label = %q, want "hostile#2"`, s)
	}
	if s := evalStr(t, env, `check(true) + 1`); s != "8" {
		t.Errorf(`check(true) + 1 = %q, want "8"`, s)
	}

	fn, _ := env.Get("check")
	if _, err := fn.(func([]any) (any, error))([]any{false}); err == nil || err.Error() != "refused" {
		t.Errorf("check(false) error = %v, want refused", err)
	}
	if _, err := fn.(func([]any) (any, error))(nil); err == nil || !strings.Contains(err.Error(), "takes 1 arguments") {
		t.Errorf("check() error = %v, want an arity error", err)
	}
}

// Shapes that cannot be adapted still panic at registration.
func TestHostFuncRejected(t *testing.T) {
	for name, fn := range map[string]any{
		"struct param":   func(struct{ X int }) {},
		"pointer param":  func(*int) {},
		"variadic":       func(...string) {},
		"three results":  func() (int, int, error) { return 0, 0, nil },
		"second not err": func() (int, int) { return 0, 0 },
		"not a func":     42,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Env.Func accepted %T", fn)
				}
			}()
			NewEnv().Func("f", fn)
		})
	}
}
