package gml

import "testing"

// The evaluator's own cost, apart from any page: what a binding pays per
// frame for arithmetic, member reads and string methods, and what a handler
// pays for a loop and for calls. These are the yardstick for any change to
// the value representation or to the statement loop (the execution budget
// is charged there).
func benchEval(b *testing.B, src string) {
	env := NewEnv()
	env.Set("a", float64(12))
	env.Set("b", float64(7.5))
	env.Set("c", float64(100))
	env.Set("s", "hello, world")
	env.Set("long", "The quick brown fox jumps over the lazy dog, twice over and then once more for luck.")
	env.Set("bmp", "héllo, wörld — 日本語のテキスト")
	env.Set("astral", "héllo 😀 wörld 😀 日本語")
	env.Set("obj", map[string]any{"x": float64(1), "y": float64(4), "name": "n"})
	env.Set("xs", []any{float64(3), float64(9), float64(4), float64(1)})
	e, err := parseExprSrc(src)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := e.eval(env); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.eval(env); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvalArith(b *testing.B)  { benchEval(b, `a * 2 + b / 3 - (c % 7) > 10 && a !== b`) }
func BenchmarkEvalMember(b *testing.B) { benchEval(b, `obj.x + obj.y > 3 ? obj.name : "b"`) }
func BenchmarkEvalString(b *testing.B) {
	benchEval(b, `s.toUpperCase().slice(1, 4) + s.indexOf("l") + s.length + s.charAt(2)`)
}

// The same string work on a longer ASCII string, on text with accents and
// CJK (still one UTF-16 unit a character), and on text with emoji (two).
func BenchmarkEvalStringLong(b *testing.B) {
	benchEval(b, `long.slice(10, 40) + long.indexOf("lazy") + long.length + long.charAt(60) + long.substring(4, 9)`)
}
func BenchmarkEvalStringBMP(b *testing.B) {
	benchEval(b, `bmp.slice(1, 9) + bmp.indexOf("日") + bmp.length + bmp.charAt(12) + bmp.substring(4, 9)`)
}
func BenchmarkEvalStringAstral(b *testing.B) {
	benchEval(b, `astral.slice(1, 9) + astral.indexOf("日") + astral.length + astral.charAt(12) + astral.substring(4, 9)`)
}
func BenchmarkEvalArray(b *testing.B) {
	benchEval(b, `xs.map(v => v * 2).filter(v => v > 4).length + xs[1]`)
}
func BenchmarkEvalLoop(b *testing.B) {
	benchEval(b, `(function() { var t = 0; for (var i = 0; i < 1000; i++) { t += i % 7 } return t })()`)
}
func BenchmarkEvalCalls(b *testing.B) {
	benchEval(b, `(function() { function fib(n) { return n < 2 ? n : fib(n - 1) + fib(n - 2) } return fib(15) })()`)
}
