package gml

import (
	"testing"
	"time"

	"github.com/sushvibez/gfx/core"
)

// TestNativeType exercises the Go-registered type bridge end to end:
// live properties, method invocation from markup, signal emission (from
// another goroutine) driving an on<Signal> handler with bound args.
func TestNativeType(t *testing.T) {
	status := &core.Property[string]{}
	status.Set("idle")
	var gotPing string
	var emitFn EmitFunc
	RegisterType("TestBackend", func(emit EmitFunc) *NativeObject {
		emitFn = emit
		return &NativeObject{
			Props: map[string]any{"status": BindString(status)},
			Methods: map[string]any{
				"ping": func(s string) { gotPing = s },
			},
		}
	})

	src := `
Item {
    property string received: ""
    TestBackend {
        id: backend
        onTicked: function(n, label) { received = label + ":" + n }
    }
    Text { text: backend.status; color: "#fff" }
}`
	env := NewEnv()
	d, err := LoadString(src, env)
	if err != nil {
		t.Fatal(err)
	}
	view := d.View()
	view() // first build instantiates the backend

	// Live property: Go-side set is visible to markup on the next build.
	if v, _ := env.vars["backend"].(*elemObject).member("status"); v != "idle" {
		t.Fatalf("status = %v, want idle", v)
	}
	status.Set("connected")
	view()
	if v, _ := env.vars["backend"].(*elemObject).member("status"); v != "connected" {
		t.Fatalf("status = %v, want connected", v)
	}

	// Method invocation through the element id.
	if _, err := evalSnippet(`backend.ping("hello")`, env); err != nil {
		t.Fatalf("method call: %v", err)
	}
	if gotPing != "hello" {
		t.Fatalf("ping arg = %q, want hello", gotPing)
	}

	// Signal from another goroutine: queued, drained on the next view().
	done := make(chan struct{})
	go func() {
		emitFn("ticked", 7.0, "tick")
		close(done)
	}()
	<-done
	time.Sleep(10 * time.Millisecond)
	view()
	if v, _ := d.doc.props["received"].get().(string); v != "tick:7" {
		t.Fatalf("received = %q, want tick:7", v)
	}
}

// evalSnippet parses and evaluates one expression against env.
func evalSnippet(src string, env *Env) (any, error) {
	root, err := Parse("Item { property var __x: " + src + " }")
	if err != nil {
		return nil, err
	}
	return root.props[0].def.eval(env)
}
