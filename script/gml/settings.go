package gml

import (
	"strings"

	"github.com/sushvibez/gfx/core/settings"
)

// QtCore's Settings over core/settings (the Go API for the same): the
// properties declared on a Settings element keep their values across
// runs. When a property binds its saved value, if any, replaces the
// default (as an assignment would, so the default's binding is broken);
// every change is set in its settings.File and written once per frame.
// Keys are QSettings', "category/name" or the bare name; the file is the
// element's location or fileName ("" for the program's default).
// value(key, default), setValue(key, value) and sync() are the element's
// methods.

// SetSettingsStore replaces this document's settings store (the default
// is settings.DefaultStore()). Call it before the first frame: Settings
// properties load their values on it. nil keeps them in memory only.
func (d *Document) SetSettingsStore(s settings.Store) {
	if s == nil {
		s = &settings.Memory{}
	}
	d.doc.settingsStore = s
}

// SaveSettings writes changed Settings values now rather than at the
// start of the next frame (a host about to exit calls it).
func (d *Document) SaveSettings() { d.doc.flushSettings() }

// settingsState is a document's open settings files and bound properties.
type settingsState struct {
	files map[string]*settings.File
	// pending are the properties still to bind: all of them until the
	// first frame (the host may set the store after loading), then an
	// alias whose target needs the first build (`property alias winX:
	// window.x`).
	pending []func() bool
	// watch compares each bound property with its last value and sets it
	// in its file when it changed.
	watch []func()
}

func (doc *docState) settingsFiles() *settingsState {
	if doc.settings == nil {
		doc.settings = &settingsState{files: map[string]*settings.File{}}
	}
	return doc.settings
}

// settingsFile returns the document's settings file name, opening it once.
func (doc *docState) settingsFile(name string) *settings.File {
	s := doc.settingsFiles()
	f := s.files[name]
	if f == nil {
		store := doc.settingsStore
		if store == nil {
			store = settings.DefaultStore()
		}
		f = settings.OpenIn(store, name)
		s.files[name] = f
	}
	return f
}

// flushSettings sets every bound property that changed and saves the
// files that did.
func (doc *docState) flushSettings() {
	s := doc.settings
	if s == nil {
		return
	}
	for _, w := range s.watch {
		w()
	}
	for _, f := range s.files {
		if err := f.Save(); err != nil {
			logf("gml: Settings: %v", err)
		}
	}
}

// settingsWhere reads a Settings element's file and category.
func settingsWhere(n *node, env *Env) (file, category string) {
	for _, a := range n.attrs {
		if a.expr == nil {
			continue
		}
		switch a.name {
		case "location", "fileName", "category":
			v, err := a.expr.eval(env)
			if err != nil {
				continue
			}
			s := str(v)
			if a.name == "category" {
				category = s
			} else {
				file = strings.TrimPrefix(s, "file://")
				file = strings.TrimPrefix(file, "file:")
			}
		}
	}
	return file, category
}

func settingsKey(category, name string) string {
	if category == "" {
		return name
	}
	return category + "/" + name
}

// bindSetting queues a Settings property to bind on the first frame: its
// saved value loads, and its changes are saved. ref is the property as
// declared (an alias resolves to its target; one that cannot resolve yet
// waits for the first build).
func (doc *docState) bindSetting(n *node, name string, ref Ref, env *Env) {
	file, category := settingsWhere(n, env)
	key := settingsKey(category, name)
	bind := func() bool {
		target := ref
		if a, ok := ref.(aliasRef); ok {
			r, err := resolveRef(a.target, a.env)
			if err != nil {
				return false
			}
			target = r
		}
		f := doc.settingsFile(file)
		if v, ok := f.Get(key); ok {
			if err := target.set(v); err != nil {
				logf("gml: Settings %s: %v", key, err)
			}
		}
		// Watched by value, once per frame, rather than through onChange:
		// an alias to built-in geometry (window.width) does not notify.
		last := str(settingsJSON(target.get()))
		s := doc.settingsFiles()
		s.watch = append(s.watch, func() {
			v := settingsJSON(target.get())
			if sv := str(v); sv != last {
				last = sv
				f.Set(key, v)
			}
		})
		return true
	}
	s := doc.settingsFiles()
	s.pending = append(s.pending, bind)
}

// bindPendingSettings binds the properties waiting to. One that cannot
// yet (an alias before its target exists) waits for the next call, or,
// when final, is dropped with a log line.
func (doc *docState) bindPendingSettings(final bool) {
	s := doc.settings
	if s == nil || len(s.pending) == 0 {
		return
	}
	keep := s.pending[:0]
	for _, bind := range s.pending {
		if bind() {
			continue
		}
		if final {
			logf("gml: Settings: an alias property's target does not resolve; it is not saved")
			continue
		}
		keep = append(keep, bind)
	}
	s.pending = keep
}

// settingsJSON turns a property value into one a store can keep: JSON's
// own kinds pass, anything else (a color, a url) is kept as its string —
// the form the property reads back.
func settingsJSON(v any) any {
	switch v := v.(type) {
	case nil, float64, string, bool:
		return v
	case int:
		return float64(v)
	case float32:
		return float64(v)
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = settingsJSON(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = settingsJSON(e)
		}
		return out
	}
	return str(v)
}

// seedSettingsMethods gives a Settings element value, setValue and sync.
func (doc *docState) seedSettingsMethods(n *node, env *Env, obj *elemObject) {
	if n.typeName != "Settings" || obj == nil {
		return
	}
	if obj.methods == nil {
		obj.methods = map[string]any{}
	}
	file, category := settingsWhere(n, env)
	obj.methods["value"] = jsFunc(func(args []any) (any, error) {
		if len(args) == 0 {
			return nil, nil
		}
		var def any
		if len(args) > 1 {
			def = args[1]
		}
		return doc.settingsFile(file).Value(settingsKey(category, str(args[0])), def), nil
	})
	obj.methods["setValue"] = jsFunc(func(args []any) (any, error) {
		if len(args) < 2 {
			return nil, nil
		}
		doc.settingsFile(file).Set(settingsKey(category, str(args[0])), settingsJSON(args[1]))
		return nil, nil
	})
	obj.methods["sync"] = jsFunc(func([]any) (any, error) {
		doc.flushSettings()
		return nil, nil
	})
}
