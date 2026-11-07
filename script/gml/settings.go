package gml

import (
	"strings"
)

// QtCore's Settings: the properties declared on a Settings element keep
// their values across runs. When a property is created its saved value,
// if any, replaces the default (as an assignment would, so the default's
// binding is broken); every change is written back, once per frame, to
// the document's SettingsStore. Keys are QSettings', "category/name" or
// the bare name. value(key, default), setValue(key, value) and sync() are
// the element's methods.

// SettingsStore keeps Settings values between runs. file is the
// Settings element's location or fileName ("" for the default file); the
// values are a flat map from "category/name" keys to JSON-compatible
// values (numbers, strings, bools, arrays, objects).
type SettingsStore interface {
	Load(file string) map[string]any
	Save(file string, values map[string]any) error
}

// defaultSettingsStore is the store a Document starts with: a JSON file
// per settings file natively, localStorage on the web (settings_*.go).
var defaultSettingsStore SettingsStore = platformSettingsStore{}

// SetDefaultSettingsStore replaces the store Documents loaded afterwards
// use (tests, or a host that keeps its settings elsewhere). nil keeps
// Settings values in memory only.
func SetDefaultSettingsStore(s SettingsStore) {
	if s == nil {
		s = &MemorySettingsStore{}
	}
	defaultSettingsStore = s
}

// SetSettingsStore replaces this document's store. Call it before the
// first frame: Settings properties load their values on it.
func (d *Document) SetSettingsStore(s SettingsStore) {
	if s == nil {
		s = &MemorySettingsStore{}
	}
	d.doc.settingsStore = s
}

// SaveSettings writes changed Settings values now rather than at the
// start of the next frame (a host about to exit calls it).
func (d *Document) SaveSettings() { d.doc.flushSettings() }

// MemorySettingsStore keeps values in memory, per file: Settings work
// within a run and are forgotten after it.
type MemorySettingsStore struct {
	files map[string]map[string]any
}

func (m *MemorySettingsStore) Load(file string) map[string]any {
	out := map[string]any{}
	for k, v := range m.files[file] {
		out[k] = v
	}
	return out
}

func (m *MemorySettingsStore) Save(file string, values map[string]any) error {
	if m.files == nil {
		m.files = map[string]map[string]any{}
	}
	cp := make(map[string]any, len(values))
	for k, v := range values {
		cp[k] = v
	}
	m.files[file] = cp
	return nil
}

// settingsState is a document's loaded settings files.
type settingsState struct {
	files map[string]map[string]any
	dirty map[string]bool
	// pending are the properties still to bind: all of them until the
	// first frame (the host may set the store after loading), then an
	// alias whose target needs the first build (`property alias winX:
	// window.x`).
	pending []func() bool
	// watch compares each bound property with its last value, marking
	// its file dirty when it changed.
	watch []func()
}

func (doc *docState) settingsFiles() *settingsState {
	if doc.settings == nil {
		doc.settings = &settingsState{files: map[string]map[string]any{}, dirty: map[string]bool{}}
	}
	return doc.settings
}

func (doc *docState) store() SettingsStore {
	if doc.settingsStore == nil {
		doc.settingsStore = defaultSettingsStore
	}
	return doc.settingsStore
}

// settingsValues returns the loaded values of file, loading it once.
func (doc *docState) settingsValues(file string) map[string]any {
	s := doc.settingsFiles()
	vals, ok := s.files[file]
	if !ok {
		vals = doc.store().Load(file)
		if vals == nil {
			vals = map[string]any{}
		}
		s.files[file] = vals
	}
	return vals
}

// flushSettings saves every file changed since the last flush.
func (doc *docState) flushSettings() {
	s := doc.settings
	if s == nil {
		return
	}
	for _, w := range s.watch {
		w()
	}
	if len(s.dirty) == 0 {
		return
	}
	for file := range s.dirty {
		if err := doc.store().Save(file, s.files[file]); err != nil {
			logf("gml: Settings: %v", err)
		}
	}
	clear(s.dirty)
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

// bindSetting gives a Settings property its saved value and saves its
// changes. ref is the property as declared (an alias resolves to its
// target; one that cannot resolve yet waits for the first build).
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
		vals := doc.settingsValues(file)
		if v, ok := vals[key]; ok {
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
				vals[key] = v
				s.dirty[file] = true
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
// own kinds pass, anything else (a color, a url) is kept as its string.
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
		if v, ok := doc.settingsValues(file)[settingsKey(category, str(args[0]))]; ok {
			return v, nil
		}
		if len(args) > 1 {
			return args[1], nil
		}
		return nil, nil
	})
	obj.methods["setValue"] = jsFunc(func(args []any) (any, error) {
		if len(args) < 2 {
			return nil, nil
		}
		doc.settingsValues(file)[settingsKey(category, str(args[0]))] = settingsJSON(args[1])
		doc.settingsFiles().dirty[file] = true
		return nil, nil
	})
	obj.methods["sync"] = jsFunc(func([]any) (any, error) {
		doc.flushSettings()
		return nil, nil
	})
}
