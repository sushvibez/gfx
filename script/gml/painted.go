package gml

// Go-implemented VISUAL element types — the QQuickPaintedItem equivalent of
// RegisterType. A painted type is an app's own element that draws itself:
// markup sizes and binds it like any item, Go paints it.
//
//	gml.RegisterPaintedType("GaugeItem", func(emit gml.EmitFunc) *gml.PaintedObject {
//	    value := &core.Property[float32]{}
//	    return &gml.PaintedObject{
//	        Props: map[string]any{"value": gml.BindFloat(value)},
//	        Draw: func(r *render.Renderer2D, f *render.Font, rect ui.Rect) {
//	            // ... draw the gauge at value.Get() inside rect
//	        },
//	    }
//	})
//
//	GaugeItem { width: 200; height: 200; value: rpmSlider.value }
//
// Instances are retained per element id across the per-frame rebuild, so
// their state (and any goroutine driving it) survives; attribute writes go
// through the bound properties, and Draw runs in the paint phase with the
// item's laid-out rect.

import (
	"fmt"
	"sync"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
)

// PaintedObject is what a painted type's factory returns.
type PaintedObject struct {
	// Props exposes bindable state (BindFloat / BindString / ...), read and
	// written from markup like any element property.
	Props map[string]any
	// Methods exposes invokables, callable through the element's id.
	Methods map[string]any
	// Draw paints the item. It runs in the paint phase, with the rect the
	// layout gave the element; the font is the UI default face.
	Draw func(r *render.Renderer2D, f *render.Font, rect ui.Rect)
}

var (
	paintedMu    sync.RWMutex
	paintedTypes = map[string]func(EmitFunc) *PaintedObject{}
)

// RegisterPaintedType registers a Go-implemented visual element type. Call
// before loading documents that use it.
func RegisterPaintedType(name string, factory func(EmitFunc) *PaintedObject) {
	paintedMu.Lock()
	paintedTypes[name] = factory
	paintedMu.Unlock()
}

// isPaintedType reports whether a type name has a registered painter.
func isPaintedType(name string) bool {
	paintedMu.RLock()
	_, ok := paintedTypes[name]
	paintedMu.RUnlock()
	return ok
}

// paintedInst is one painted element's retained instance.
type paintedInst struct {
	obj *PaintedObject
}

// buildPainted renders a registered painted type: it applies the markup's
// attribute writes to the instance's properties and returns a canvas that
// calls its Draw.
func buildPainted(n *node, env *Env, doc *docState) (ui.Element, error) {
	key := doc.key(n)
	if doc.painted == nil {
		doc.painted = map[string]*paintedInst{}
	}
	inst := doc.painted[key]
	if inst == nil {
		paintedMu.RLock()
		factory := paintedTypes[n.typeName]
		paintedMu.RUnlock()
		if factory == nil {
			return nil, fmt.Errorf("gml:%d: unknown element %q", n.line, n.typeName)
		}
		// Signals from a painted type reach the same per-element handler
		// queue the data backends use.
		id := nodeID(n)
		inst = &paintedInst{obj: factory(func(signal string, args ...any) {
			doc.queueNativeSignal(id, signal, args)
		})}
		doc.painted[key] = inst
	}
	// Expose properties and methods through the element's id, so other
	// bindings can read them (`gauge.value`) and handlers can call them.
	if id := nodeID(n); id != "" {
		obj := doc.elemObjs.get(doc.here(id))
		if obj == nil {
			obj = &elemObject{}
			doc.elemObjs.set(doc.here(id), obj)
		}
		obj.props = inst.obj.Props
		obj.methods = inst.obj.Methods
		env.vars[id] = obj
	}

	var w, h float32
	for _, a := range n.attrs {
		switch a.name {
		case "id":
		case "width":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			w = f
		case "height":
			f, err := attrFloat(a, env)
			if err != nil {
				return nil, err
			}
			h = f
		case "visible":
			if b, err := attrBool(a, env); err == nil && !b {
				return ui.Div(), nil
			}
		default:
			if a.expr == nil {
				continue // handlers are wired by the signal machinery
			}
			// Everything else is a property write, re-evaluated each build
			// so `value: slider.value` behaves like a binding.
			ref, ok := inst.obj.Props[a.name].(Ref)
			if !ok {
				if _, known := inst.obj.Props[a.name]; !known {
					return nil, fmt.Errorf("gml:%d: %s has no property %q",
						a.line, n.typeName, a.name)
				}
				continue
			}
			v, err := a.expr.eval(env)
			if err != nil {
				return nil, err
			}
			if err := ref.set(v); err != nil {
				return nil, fmt.Errorf("gml:%d: %s.%s: %v", a.line, n.typeName, a.name, err)
			}
		}
	}
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 100
	}
	draw := inst.obj.Draw
	if draw == nil {
		return ui.Div().Size(w, h), nil
	}
	return ui.NewCanvasFont(w, h, draw), nil
}
