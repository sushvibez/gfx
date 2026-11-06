package gml

import (
	"fmt"
	"math"
	"time"

	"github.com/sushvibez/gfx/core"
	"github.com/sushvibez/gfx/ui"
)

// Attribute conversion helpers shared by the builders.

// attrExprGuard rejects element-valued attributes where an expression is
// required (a.expr nil would otherwise segfault deep in eval).
func attrExprGuard(a attr) error {
	if a.expr == nil {
		return fmt.Errorf("gml:%d: %s wants an expression, not an element", a.line, a.name)
	}
	return nil
}

func attrColor(a attr, env *Env) (ui.RGBA, error) {
	if err := attrExprGuard(a); err != nil {
		return ui.RGBA{}, err
	}
	v, err := a.expr.eval(env)
	if err != nil {
		return ui.RGBA{}, err
	}
	s, ok := plainColor(v).(string)
	if !ok {
		return ui.RGBA{}, fmt.Errorf("gml:%d: %s wants a color string", a.line, a.name)
	}
	c, err := parseColor(s)
	if err != nil {
		return ui.RGBA{}, fmt.Errorf("gml:%d: %v", a.line, err)
	}
	return c, nil
}

func attrString(a attr, env *Env) (string, error) {
	if err := attrExprGuard(a); err != nil {
		return "", err
	}
	v, err := a.expr.eval(env)
	if err != nil {
		return "", err
	}
	s, ok := plainColor(v).(string)
	if !ok {
		return "", fmt.Errorf("gml:%d: %s wants a string", a.line, a.name)
	}
	return s, nil
}

func attrFloat(a attr, env *Env) (float32, error) {
	if err := attrExprGuard(a); err != nil {
		return 0, err
	}
	v, err := a.expr.eval(env)
	if err != nil {
		return 0, err
	}
	f, err := toFloat32(v)
	if err != nil {
		return 0, fmt.Errorf("gml:%d: %s: %v", a.line, a.name, err)
	}
	// A binding can divide by a zero it read before the thing it divides
	// by exists (`width / img.implicitWidth` on the frame the image has
	// not decoded), and JS answers Infinity or NaN. Neither is a length:
	// they poison every rect derived from them, and a layout handed one
	// walks an infinite box (the corpus' blueprint viewer took 40s a frame
	// once its implicit size was real). Qt treats such a value as nothing,
	// and so does this.
	if math.IsInf(float64(f), 0) || math.IsNaN(float64(f)) {
		return 0, nil
	}
	return f, nil
}

func attrBool(a attr, env *Env) (bool, error) {
	if err := attrExprGuard(a); err != nil {
		return false, err
	}
	v, err := a.expr.eval(env)
	if err != nil {
		return false, err
	}
	b, err := toBool(v)
	if err != nil {
		return false, fmt.Errorf("gml:%d: %s: %v", a.line, a.name, err)
	}
	return b, nil
}

func attrAlign(a attr, env *Env) (ui.Align, error) {
	v, err := a.expr.eval(env)
	if err != nil {
		return ui.AlignStart, err
	}
	switch str(v) {
	case "start":
		return ui.AlignStart, nil
	case "center":
		return ui.AlignCenter, nil
	case "end":
		return ui.AlignEnd, nil
	}
	return ui.AlignStart, fmt.Errorf("gml:%d: %s wants start|center|end", a.line, a.name)
}

// newDeclaredProp backs a markup `property` declaration with a real
// core.Property of the declared type.
func newDeclaredProp(typ string, def any) (Ref, error) {
	switch typ {
	case "int":
		p := &core.Property[int]{}
		v, err := toInt(def)
		if err != nil {
			return nil, err
		}
		p.Set(v)
		return propRef[int]{p, toInt, func(to int, d time.Duration) { core.Tween(p, to, d, nil) }}, nil
	case "real", "double":
		p := &core.Property[float32]{}
		v, err := toFloat32(def)
		if err != nil {
			return nil, err
		}
		p.Set(v)
		return propRef[float32]{p, toFloat32, func(to float32, d time.Duration) { core.Tween(p, to, d, nil) }}, nil
	case "bool":
		p := &core.Property[bool]{}
		v, err := toBool(def)
		if err != nil {
			return nil, err
		}
		p.Set(v)
		return propRef[bool]{p, toBool, nil}, nil
	case "string":
		p := &core.Property[string]{}
		p.Set(str(def))
		return propRef[string]{p, func(v any) (string, error) { return str(v), nil }, nil}, nil
	case "color":
		// A colour is string-backed (attributes parse the hex at the use
		// site), but it READS as a colour: `property color c` has .r/.g/.b,
		// where a `property string` holding the same text does not. See
		// colorVal.
		p := &core.Property[string]{}
		p.Set(str(def))
		return colorRef{propRef[string]{p, func(v any) (string, error) { return str(v), nil }, nil}}, nil
	case "var":
		// var holds any JS value. Boxing gives pointer identity so the
		// property's equality check never compares (possibly non-comparable)
		// contents — Set always invalidates dependents.
		p := &core.Property[*varBox]{}
		p.Set(&varBox{def})
		return varRef{p}, nil
	}
	return nil, fmt.Errorf("unsupported property type %q", typ)
}
