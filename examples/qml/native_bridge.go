//go:build linux || windows

package main

// PropertyBridge / MethodBridge / SignalBridge: the corpus' three QObject
// bridge demos, as Go backends. Between them they cover the whole
// QML<->host contract — Q_PROPERTY read/write with computed values,
// Q_INVOKABLE methods returning values and objects, and signals emitted from
// a worker goroutine with progress.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/script/gml"
)

func registerBridges() {
	registerPropertyBridge()
	registerMethodBridge()
	registerSignalBridge()
}

// registerPropertyBridge exposes plain properties plus a computed `summary`
// that recomputes whenever one of its inputs changes — the Q_PROPERTY with a
// NOTIFY signal pattern.
func registerPropertyBridge() {
	gml.RegisterType("PropertyBridge", func(emit gml.EmitFunc) *gml.NativeObject {
		counter := &core.Property[float32]{}
		userName := &core.Property[string]{}
		temperature := &core.Property[float32]{}
		active := &core.Property[bool]{}
		summary := &core.Property[string]{}
		tags := gml.NewVarProp([]any{"qml", "go", "bridge"})
		userName.Set("Ada")
		temperature.Set(21.5)
		active.Set(true)

		recompute := func() {
			state := "inactive"
			if active.Get() {
				state = "active"
			}
			summary.Set(fmt.Sprintf("%s: %d clicks, %.1f C, %s",
				userName.Get(), int(counter.Get()), temperature.Get(), state))
		}
		recompute()
		counter.Changed().Connect(func(float32) { recompute() })
		userName.Changed().Connect(func(string) { recompute() })
		temperature.Changed().Connect(func(float32) { recompute() })
		active.Changed().Connect(func(bool) { recompute() })

		return &gml.NativeObject{
			Props: map[string]any{
				"counter":     gml.BindFloat(counter),
				"userName":    gml.BindString(userName),
				"temperature": gml.BindFloat(temperature),
				"active":      gml.BindBool(active),
				"summary":     gml.BindString(summary),
				"tags":        gml.BindVar(tags),
			},
			Methods: map[string]any{
				"increment": func([]any) (any, error) {
					counter.Set(counter.Get() + 1)
					return nil, nil
				},
				"decrement": func([]any) (any, error) {
					counter.Set(counter.Get() - 1)
					return nil, nil
				},
				"reset": func([]any) (any, error) {
					counter.Set(0)
					return nil, nil
				},
				"addTag": func(args []any) (any, error) {
					tag := strings.TrimSpace(argStr(args, 0))
					if tag == "" {
						return nil, nil
					}
					cur, _ := tags.Get().([]any)
					for _, v := range cur {
						if str(v, "") == tag {
							return nil, nil
						}
					}
					tags.Set(append(append([]any{}, cur...), tag))
					emit("tagsChanged")
					return nil, nil
				},
				"removeTag": func(args []any) (any, error) {
					tag := argStr(args, 0)
					cur, _ := tags.Get().([]any)
					out := make([]any, 0, len(cur))
					for _, v := range cur {
						if str(v, "") != tag {
							out = append(out, v)
						}
					}
					tags.Set(out)
					emit("tagsChanged")
					return nil, nil
				},
			},
		}
	})
}

// registerMethodBridge exposes Q_INVOKABLE-style methods: a string
// transform driven by an enum, a fibonacci over an int, an email check, and
// one returning a whole object.
func registerMethodBridge() {
	gml.RegisterType("MethodBridge", func(emit gml.EmitFunc) *gml.NativeObject {
		return &gml.NativeObject{
			Props: map[string]any{
				// The Q_ENUM values markup passes as MethodBridge.Uppercase
				// or as plain integers.
				"Uppercase":  float64(0),
				"Lowercase":  float64(1),
				"Capitalize": float64(2),
				"Reverse":    float64(3),
			},
			Methods: map[string]any{
				"transformText": func(args []any) (any, error) {
					text := argStr(args, 0)
					switch int(num(argAt(args, 1), 0)) {
					case 1:
						return strings.ToLower(text), nil
					case 2:
						return capitalizeWords(text), nil
					case 3:
						return reverseString(text), nil
					default:
						return strings.ToUpper(text), nil
					}
				},
				"fibonacci": func(args []any) (any, error) {
					n := int(num(argAt(args, 0), 0))
					if n < 0 {
						n = 0
					}
					a, b := uint64(0), uint64(1)
					for i := 0; i < n; i++ {
						a, b = b, a+b
					}
					return float64(a), nil
				},
				"validateEmail": func(args []any) (any, error) {
					s := strings.TrimSpace(argStr(args, 0))
					at := strings.IndexByte(s, '@')
					dot := strings.LastIndexByte(s, '.')
					return at > 0 && dot > at+1 && dot < len(s)-1 &&
						!strings.ContainsAny(s, " \t"), nil
				},
				// analyzeText returns an OBJECT, which markup reads by role.
				"analyzeText": func(args []any) (any, error) {
					s := argStr(args, 0)
					words := strings.Fields(s)
					letters, digits, spaces := 0, 0, 0
					for _, r := range s {
						switch {
						case unicode.IsLetter(r):
							letters++
						case unicode.IsDigit(r):
							digits++
						case unicode.IsSpace(r):
							spaces++
						}
					}
					longest := ""
					for _, w := range words {
						if len(w) > len(longest) {
							longest = w
						}
					}
					vowels := 0
					for _, r := range strings.ToLower(s) {
						if strings.ContainsRune("aeiou", r) {
							vowels++
						}
					}
					return map[string]any{
						// `length` and `vowels` are what the page's badges
						// read; the rest are the analyzer's own summary.
						"length":      float64(len([]rune(s))),
						"vowels":      float64(vowels),
						"characters":  float64(len([]rune(s))),
						"words":       float64(len(words)),
						"letters":     float64(letters),
						"digits":      float64(digits),
						"spaces":      float64(spaces),
						"longestWord": longest,
						"summary": strconv.Itoa(len(words)) + " words, " +
							strconv.Itoa(len([]rune(s))) + " characters",
					}, nil
				},
			},
		}
	})
}

// registerSignalBridge runs a task on a goroutine and reports it through
// signals — dataReceived / taskCompleted / errorOccurred — with live
// progress, which is the cross-thread emission path these examples show.
func registerSignalBridge() {
	gml.RegisterType("SignalBridge", func(emit gml.EmitFunc) *gml.NativeObject {
		running := &core.Property[bool]{}
		progress := &core.Property[float32]{}
		stop := make(chan struct{})

		return &gml.NativeObject{
			Props: map[string]any{
				"running":  gml.BindBool(running),
				"progress": gml.BindFloat(progress),
			},
			Methods: map[string]any{
				"startTask": func([]any) (any, error) {
					if running.Get() {
						return nil, nil
					}
					running.Set(true)
					progress.Set(0)
					stop = make(chan struct{})
					done := stop
					go func() {
						for i := 1; i <= 10; i++ {
							select {
							case <-done:
								emit("errorOccurred", "Task cancelled at step "+strconv.Itoa(i))
								gml.Post(func() { running.Set(false) })
								return
							case <-time.After(250 * time.Millisecond):
							}
							p := float32(i) / 10
							gml.Post(func() { progress.Set(p) })
							emit("dataReceived", "step "+strconv.Itoa(i)+" of 10 done")
						}
						gml.Post(func() {
							running.Set(false)
							progress.Set(1)
						})
						emit("taskCompleted", "Task finished in a Go goroutine")
					}()
					return nil, nil
				},
				"stopTask": func([]any) (any, error) {
					if !running.Get() {
						return nil, nil
					}
					close(stop)
					return nil, nil
				},
				"emitError": func(args []any) (any, error) {
					msg := argStr(args, 0)
					if msg == "" {
						msg = "Something went wrong (from Go)"
					}
					emit("errorOccurred", msg)
					return nil, nil
				},
			},
		}
	})
}

// capitalizeWords upper-cases the first letter of each word.
func capitalizeWords(s string) string {
	var b strings.Builder
	start := true
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			start = true
			b.WriteRune(r)
		case start:
			b.WriteRune(unicode.ToUpper(r))
			start = false
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func reverseString(s string) string {
	rs := []rune(s)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}
