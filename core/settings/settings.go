// Package settings keeps a program's settings between runs: named files of
// key/value pairs (QSettings' "group/key" keys), JSON-valued, in a
// pluggable Store — a JSON file per name under the user's config
// directory natively, localStorage on the web.
//
//	prefs := settings.Open("myapp")
//	zoom := prefs.Value("view/zoom", 1.0)
//	settings.Bind(prefs, "view/grid", &gridOn) // load now, save on change
//	...
//	prefs.Save() // writes only when something changed
//
// A File belongs to one goroutine (the UI's, like core.Property).
package settings

import (
	"encoding/json"
	"log"
	"reflect"

	"github.com/sushvibez/gfx/core"
)

// Store keeps settings files. name is the file's name ("" for the
// program's default); values map keys to JSON-compatible values
// (float64, string, bool, []any, map[string]any).
type Store interface {
	Load(name string) map[string]any
	Save(name string, values map[string]any) error
}

var defaultStore Store = platformStore{}

// DefaultStore is the store Open uses: the platform's, unless replaced.
func DefaultStore() Store { return defaultStore }

// SetDefault replaces the store Open uses from now on (tests, or a host
// that keeps settings elsewhere). nil keeps them in memory only.
func SetDefault(s Store) {
	if s == nil {
		s = &Memory{}
	}
	defaultStore = s
}

// Memory is a Store in memory: settings last for the run.
type Memory struct {
	files map[string]map[string]any
}

func (m *Memory) Load(name string) map[string]any {
	out := map[string]any{}
	for k, v := range m.files[name] {
		out[k] = v
	}
	return out
}

func (m *Memory) Save(name string, values map[string]any) error {
	if m.files == nil {
		m.files = map[string]map[string]any{}
	}
	cp := make(map[string]any, len(values))
	for k, v := range values {
		cp[k] = v
	}
	m.files[name] = cp
	return nil
}

// File is one settings file, loaded on first use.
type File struct {
	store  Store
	name   string
	values map[string]any
	dirty  bool
}

// Open returns the settings file name in the default store.
func Open(name string) *File { return OpenIn(DefaultStore(), name) }

// OpenIn returns the settings file name in store.
func OpenIn(store Store, name string) *File { return &File{store: store, name: name} }

// Name is the file's name.
func (f *File) Name() string { return f.name }

func (f *File) load() {
	if f.values != nil {
		return
	}
	f.values = f.store.Load(f.name)
	if f.values == nil {
		f.values = map[string]any{}
	}
}

// Get is key's saved value.
func (f *File) Get(key string) (any, bool) {
	f.load()
	v, ok := f.values[key]
	return v, ok
}

// Value is key's saved value, or def.
func (f *File) Value(key string, def any) any {
	if v, ok := f.Get(key); ok {
		return v
	}
	return def
}

// Set stores v under key (as JSON would hold it: numbers become float64,
// structs objects) and marks the file changed when the value differs.
func (f *File) Set(key string, v any) {
	f.load()
	v = normalize(v)
	if old, ok := f.values[key]; ok && reflect.DeepEqual(old, v) {
		return
	}
	f.values[key] = v
	f.dirty = true
}

// Dirty reports whether the file has changes not yet saved.
func (f *File) Dirty() bool { return f.dirty }

// Save writes the file if it changed since it was loaded or last saved.
func (f *File) Save() error {
	if !f.dirty {
		return nil
	}
	if err := f.store.Save(f.name, f.values); err != nil {
		return err
	}
	f.dirty = false
	return nil
}

// normalize turns v into the JSON-compatible value a store keeps.
func normalize(v any) any {
	switch v.(type) {
	case nil, float64, string, bool:
		return v
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if json.Unmarshal(b, &out) != nil {
		return v
	}
	return out
}

// Bind ties p to key: a saved value is loaded into p now, and every change
// of p is set in f (call f.Save to write it). The returned connection
// undoes the second half.
func Bind[T comparable](f *File, key string, p *core.Property[T]) core.Connection {
	if v, ok := f.Get(key); ok {
		var t T
		if b, err := json.Marshal(v); err == nil && json.Unmarshal(b, &t) == nil {
			p.Set(t)
		}
	}
	return p.Changed().Connect(func(v T) { f.Set(key, v) })
}

func logf(format string, args ...any) { log.Printf(format, args...) }
