//go:build !js

package gml

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// platformSettingsStore keeps each settings file as JSON. A Settings
// element without a location or fileName uses <user config dir>/gfx/
// <program name>.json, the program's own settings as QSettings keeps
// them per application; a relative location sits in that directory too,
// and an absolute one is used as given.
type platformSettingsStore struct{}

func settingsPath(file string) (string, error) {
	if filepath.IsAbs(file) {
		return file, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if file == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		file = strings.TrimSuffix(filepath.Base(exe), ".exe")
	}
	if filepath.Ext(file) == "" {
		file += ".json"
	}
	return filepath.Join(dir, "gfx", file), nil
}

func (platformSettingsStore) Load(file string) map[string]any {
	path, err := settingsPath(file)
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logf("gml: Settings: %v", err)
		}
		return nil
	}
	var vals map[string]any
	if err := json.Unmarshal(b, &vals); err != nil {
		logf("gml: Settings: %s: %v", path, err)
		return nil
	}
	return vals
}

func (platformSettingsStore) Save(file string, values map[string]any) error {
	path, err := settingsPath(file)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Write beside and rename, so a crash mid-write leaves the old file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
