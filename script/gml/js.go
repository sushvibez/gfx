package gml

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
)

// JavaScript-subset builtins (phase D1). QML expressions and handlers are
// JavaScript; this provides the most-used global objects — Math, console,
// JSON — as namespace maps of jsFunc, reached through the same member-access
// path used by model rows. Statement-level JS (functions, var, control flow)
// is a later phase.

// jsFunc is a builtin callable: variadic args in, one value out.
type jsFunc func(args []any) (any, error)

// arg returns args[i] coerced to float64, erroring if absent.
func argNum(args []any, i int) (float64, error) {
	if i >= len(args) {
		return 0, fmt.Errorf("missing numeric argument %d", i+1)
	}
	return toFloat64(args[i])
}

// mathNS is the JavaScript Math global: constants and numeric functions.
func mathNS() map[string]any {
	unary := func(f func(float64) float64) jsFunc {
		return func(a []any) (any, error) {
			x, err := argNum(a, 0)
			if err != nil {
				return nil, err
			}
			return f(x), nil
		}
	}
	reduce := func(seed float64, f func(acc, x float64) float64) jsFunc {
		return func(a []any) (any, error) {
			acc := seed
			for i := range a {
				x, err := argNum(a, i)
				if err != nil {
					return nil, err
				}
				acc = f(acc, x)
			}
			return acc, nil
		}
	}
	binary := func(f func(x, y float64) float64) jsFunc {
		return func(a []any) (any, error) {
			x, err := argNum(a, 0)
			if err != nil {
				return nil, err
			}
			y, err := argNum(a, 1)
			if err != nil {
				return nil, err
			}
			return f(x, y), nil
		}
	}
	return map[string]any{
		"PI":      math.Pi,
		"E":       math.E,
		"LN2":     math.Ln2,
		"LN10":    math.Log(10),
		"LOG2E":   1 / math.Ln2,
		"LOG10E":  1 / math.Log(10),
		"SQRT2":   math.Sqrt2,
		"SQRT1_2": math.Sqrt(0.5),
		"abs":     unary(math.Abs),
		"floor":   unary(math.Floor),
		"ceil":    unary(math.Ceil),
		"round":   unary(math.Round),
		"trunc":   unary(math.Trunc),
		"sqrt":    unary(math.Sqrt),
		"cbrt":    unary(math.Cbrt),
		"exp":     unary(math.Exp),
		"expm1":   unary(math.Expm1),
		"log":     unary(math.Log),
		"log2":    unary(math.Log2),
		"log10":   unary(math.Log10),
		"log1p":   unary(math.Log1p),
		"sin":     unary(math.Sin),
		"cos":     unary(math.Cos),
		"tan":     unary(math.Tan),
		"asin":    unary(math.Asin),
		"acos":    unary(math.Acos),
		"atan":    unary(math.Atan),
		"sinh":    unary(math.Sinh),
		"cosh":    unary(math.Cosh),
		"tanh":    unary(math.Tanh),
		"atan2":   binary(math.Atan2),
		"hypot":   binary(math.Hypot),
		"sign": unary(func(x float64) float64 {
			switch {
			case x > 0:
				return 1
			case x < 0:
				return -1
			}
			return x // preserves 0 and NaN, as JS does
		}),
		"max": reduce(math.Inf(-1), math.Max),
		"min": reduce(math.Inf(1), math.Min),
		"pow": jsFunc(func(a []any) (any, error) {
			x, err := argNum(a, 0)
			if err != nil {
				return nil, err
			}
			y, err := argNum(a, 1)
			if err != nil {
				return nil, err
			}
			return math.Pow(x, y), nil
		}),
		"random": jsFunc(func([]any) (any, error) { return rand.Float64(), nil }),
	}
}

// consoleNS is the JavaScript console global; all levels write to stderr.
func consoleNS() map[string]any {
	emit := jsFunc(func(a []any) (any, error) {
		parts := make([]string, len(a))
		for i, v := range a {
			parts[i] = str(v)
		}
		fmt.Fprintln(os.Stderr, strings.Join(parts, " "))
		return nil, nil
	})
	return map[string]any{"log": emit, "warn": emit, "error": emit, "info": emit}
}

// jsonNS is the JavaScript JSON global: stringify / parse over the value
// types this engine uses (float64, string, bool, []any, map[string]any).
func jsonNS() map[string]any {
	return map[string]any{
		"stringify": jsFunc(func(a []any) (any, error) {
			if len(a) == 0 {
				return "undefined", nil
			}
			b, err := json.Marshal(a[0])
			if err != nil {
				return nil, err
			}
			return string(b), nil
		}),
		"parse": jsFunc(func(a []any) (any, error) {
			if len(a) == 0 {
				return nil, fmt.Errorf("JSON.parse needs a string")
			}
			var v any
			if err := json.Unmarshal([]byte(str(a[0])), &v); err != nil {
				return nil, err
			}
			return v, nil
		}),
	}
}
