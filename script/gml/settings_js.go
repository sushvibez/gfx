//go:build js

package gml

import (
	"encoding/json"
	"syscall/js"
)

// platformSettingsStore keeps each settings file as one JSON entry in the
// page's localStorage, under "gfx.settings:" + the file name. Storage
// that is missing or refuses (a private window) leaves Settings working
// for the session only.
type platformSettingsStore struct{}

func localStorage() (v js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	ls := js.Global().Get("localStorage")
	return ls, ls.Truthy()
}

func (platformSettingsStore) Load(file string) map[string]any {
	ls, ok := localStorage()
	if !ok {
		return nil
	}
	var vals map[string]any
	func() {
		defer func() { _ = recover() }()
		item := ls.Call("getItem", "gfx.settings:"+file)
		if item.Type() == js.TypeString {
			if err := json.Unmarshal([]byte(item.String()), &vals); err != nil {
				vals = nil
			}
		}
	}()
	return vals
}

func (platformSettingsStore) Save(file string, values map[string]any) error {
	ls, ok := localStorage()
	if !ok {
		return nil
	}
	b, err := json.Marshal(values)
	if err != nil {
		return err
	}
	func() {
		defer func() { _ = recover() }()
		ls.Call("setItem", "gfx.settings:"+file, string(b))
	}()
	return nil
}
