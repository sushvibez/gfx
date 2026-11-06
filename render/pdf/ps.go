package pdf

// Type-4 functions: the PostScript calculator language a PDF embeds for a
// tint transform or a shading. It is a small stack machine — numbers,
// booleans and procedures on one operand stack, no variables, no loops
// beyond `if`/`ifelse` — so it evaluates directly, with a step budget as
// the only guard a program with no loops still needs (a deeply nested
// conditional could otherwise be made expensive).

import (
	"math"
	"strconv"
)

// psKind is what a value on the operand stack is.
type psKind int

const (
	psNum psKind = iota
	psBool
	psProc
)

// psVal is one operand-stack entry.
type psVal struct {
	kind psKind
	num  float64
	b    bool
	proc []psTok
}

// psTok is one token of a compiled program: a literal to push, or an
// operator to run.
type psTok struct {
	op  string // "" for a literal
	val psVal  // the literal
}

// parsePostScript compiles a calculator program. The whole program is
// wrapped in braces, which this strips.
func parsePostScript(src []byte) []psTok {
	l := &lexer{b: src}
	l.skipSpace()
	if l.pos < len(src) && src[l.pos] == '{' {
		l.pos++
	}
	toks, _ := parsePSBlock(l, 0)
	return toks
}

// parsePSBlock reads tokens up to the matching '}'.
func parsePSBlock(l *lexer, depth int) ([]psTok, bool) {
	if depth > 32 { // a program nested that deep is not one we run
		return nil, false
	}
	var out []psTok
	for {
		l.skipSpace()
		if l.pos >= len(l.b) {
			return out, false
		}
		switch c := l.b[l.pos]; c {
		case '}':
			l.pos++
			return out, true
		case '{':
			l.pos++
			proc, ok := parsePSBlock(l, depth+1)
			if !ok {
				return out, false
			}
			out = append(out, psTok{val: psVal{kind: psProc, proc: proc}})
			continue
		case '%': // a comment runs to the end of the line
			for l.pos < len(l.b) && l.b[l.pos] != '\n' {
				l.pos++
			}
			continue
		}
		t := l.token()
		switch t {
		case "":
			return out, false
		case "true":
			out = append(out, psTok{val: psVal{kind: psBool, b: true}})
		case "false":
			out = append(out, psTok{val: psVal{kind: psBool}})
		default:
			if f, err := strconv.ParseFloat(t, 64); err == nil {
				out = append(out, psTok{val: psVal{num: f}})
				continue
			}
			if f, err := parseLooseNumber(t); err == nil {
				out = append(out, psTok{val: psVal{num: f}})
				continue
			}
			out = append(out, psTok{op: t})
		}
	}
}

// psMachine runs a compiled program.
type psMachine struct {
	stack  []psVal
	budget int
}

// runPostScript evaluates a program with the given inputs and returns the
// top n values, bottom-first (the order a function's /Range expects).
func runPostScript(prog []psTok, in []float64, n int) []float64 {
	m := &psMachine{budget: 20000}
	for _, v := range in {
		m.push(psVal{num: v})
	}
	m.run(prog)
	if n <= 0 || len(m.stack) == 0 {
		return nil
	}
	if n > len(m.stack) {
		n = len(m.stack)
	}
	out := make([]float64, 0, n)
	for _, v := range m.stack[len(m.stack)-n:] {
		out = append(out, v.num)
	}
	return out
}

func (m *psMachine) push(v psVal) {
	if len(m.stack) < 1000 { // the spec's own limit is 100
		m.stack = append(m.stack, v)
	}
}

func (m *psMachine) pop() psVal {
	if len(m.stack) == 0 {
		return psVal{}
	}
	v := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return v
}

func (m *psMachine) popNum() float64 { return m.pop().num }

func (m *psMachine) popBool() bool {
	v := m.pop()
	if v.kind == psBool {
		return v.b
	}
	return v.num != 0
}

func (m *psMachine) pushNum(v float64) { m.push(psVal{num: v}) }

func (m *psMachine) pushBool(b bool) { m.push(psVal{kind: psBool, b: b}) }

// run executes a token list.
func (m *psMachine) run(prog []psTok) {
	for _, t := range prog {
		if m.budget <= 0 {
			return
		}
		m.budget--
		if t.op == "" {
			m.push(t.val)
			continue
		}
		m.exec(t.op)
	}
}

// exec runs one operator.
func (m *psMachine) exec(op string) {
	switch op {
	// -- arithmetic ----------------------------------------------------
	case "add":
		b, a := m.popNum(), m.popNum()
		m.pushNum(a + b)
	case "sub":
		b, a := m.popNum(), m.popNum()
		m.pushNum(a - b)
	case "mul":
		b, a := m.popNum(), m.popNum()
		m.pushNum(a * b)
	case "div":
		b, a := m.popNum(), m.popNum()
		if b == 0 {
			m.pushNum(0) // PostScript would raise; a colour cannot
			return
		}
		m.pushNum(a / b)
	case "idiv":
		b, a := m.popNum(), m.popNum()
		if int(b) == 0 {
			m.pushNum(0)
			return
		}
		m.pushNum(float64(int(a) / int(b)))
	case "mod":
		b, a := m.popNum(), m.popNum()
		if int(b) == 0 {
			m.pushNum(0)
			return
		}
		m.pushNum(float64(int(a) % int(b)))
	case "neg":
		m.pushNum(-m.popNum())
	case "abs":
		m.pushNum(math.Abs(m.popNum()))
	case "sqrt":
		m.pushNum(math.Sqrt(math.Max(m.popNum(), 0)))
	case "sin":
		m.pushNum(math.Sin(m.popNum() * math.Pi / 180)) // degrees, as PostScript
	case "cos":
		m.pushNum(math.Cos(m.popNum() * math.Pi / 180))
	case "atan":
		den, num := m.popNum(), m.popNum()
		deg := math.Atan2(num, den) * 180 / math.Pi
		if deg < 0 {
			deg += 360 // PostScript's atan returns 0..360
		}
		m.pushNum(deg)
	case "exp":
		b, a := m.popNum(), m.popNum()
		m.pushNum(math.Pow(a, b))
	case "ln":
		v := m.popNum()
		if v <= 0 {
			m.pushNum(0)
			return
		}
		m.pushNum(math.Log(v))
	case "log":
		v := m.popNum()
		if v <= 0 {
			m.pushNum(0)
			return
		}
		m.pushNum(math.Log10(v))
	case "cvi", "truncate":
		m.pushNum(math.Trunc(m.popNum()))
	case "cvr":
		m.pushNum(m.popNum())
	case "floor":
		m.pushNum(math.Floor(m.popNum()))
	case "ceiling":
		m.pushNum(math.Ceil(m.popNum()))
	case "round":
		m.pushNum(math.Round(m.popNum()))
	// -- comparison and logic ------------------------------------------
	case "eq":
		b, a := m.pop(), m.pop()
		m.pushBool(psEqual(a, b))
	case "ne":
		b, a := m.pop(), m.pop()
		m.pushBool(!psEqual(a, b))
	case "gt":
		b, a := m.popNum(), m.popNum()
		m.pushBool(a > b)
	case "ge":
		b, a := m.popNum(), m.popNum()
		m.pushBool(a >= b)
	case "lt":
		b, a := m.popNum(), m.popNum()
		m.pushBool(a < b)
	case "le":
		b, a := m.popNum(), m.popNum()
		m.pushBool(a <= b)
	case "and":
		b, a := m.pop(), m.pop()
		if a.kind == psBool || b.kind == psBool {
			m.pushBool(psTruth(a) && psTruth(b))
			return
		}
		m.pushNum(float64(int(a.num) & int(b.num)))
	case "or":
		b, a := m.pop(), m.pop()
		if a.kind == psBool || b.kind == psBool {
			m.pushBool(psTruth(a) || psTruth(b))
			return
		}
		m.pushNum(float64(int(a.num) | int(b.num)))
	case "xor":
		b, a := m.pop(), m.pop()
		if a.kind == psBool || b.kind == psBool {
			m.pushBool(psTruth(a) != psTruth(b))
			return
		}
		m.pushNum(float64(int(a.num) ^ int(b.num)))
	case "not":
		v := m.pop()
		if v.kind == psBool {
			m.pushBool(!v.b)
			return
		}
		m.pushNum(float64(^int(v.num)))
	case "bitshift":
		shift, a := int(m.popNum()), int(m.popNum())
		if shift >= 0 {
			m.pushNum(float64(a << uint(min(shift, 31))))
			return
		}
		m.pushNum(float64(a >> uint(min(-shift, 31))))
	// -- conditionals --------------------------------------------------
	case "if":
		proc := m.pop()
		if m.popBool() && proc.kind == psProc {
			m.run(proc.proc)
		}
	case "ifelse":
		alt, proc := m.pop(), m.pop()
		if m.popBool() {
			if proc.kind == psProc {
				m.run(proc.proc)
			}
			return
		}
		if alt.kind == psProc {
			m.run(alt.proc)
		}
	// -- stack ---------------------------------------------------------
	case "pop":
		m.pop()
	case "exch":
		b, a := m.pop(), m.pop()
		m.push(b)
		m.push(a)
	case "dup":
		v := m.pop()
		m.push(v)
		m.push(v)
	case "copy":
		n := int(m.popNum())
		if n <= 0 || n > len(m.stack) || n > 100 {
			return
		}
		m.stack = append(m.stack, m.stack[len(m.stack)-n:]...)
	case "index":
		n := int(m.popNum())
		if n < 0 || n >= len(m.stack) {
			m.pushNum(0)
			return
		}
		m.push(m.stack[len(m.stack)-1-n])
	case "roll":
		j, n := int(m.popNum()), int(m.popNum())
		if n <= 0 || n > len(m.stack) {
			return
		}
		part := m.stack[len(m.stack)-n:]
		j = ((j % n) + n) % n
		rolled := append(append([]psVal{}, part[n-j:]...), part[:n-j]...)
		copy(part, rolled)
	}
}

// psEqual compares two operands the way `eq` does.
func psEqual(a, b psVal) bool {
	if a.kind == psBool || b.kind == psBool {
		return psTruth(a) == psTruth(b)
	}
	return a.num == b.num
}

// psTruth reads an operand as a boolean.
func psTruth(v psVal) bool {
	if v.kind == psBool {
		return v.b
	}
	return v.num != 0
}
