//go:build !js

package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// platformStore keeps each settings file as JSON. The file with no name
// is <user config dir>/gfx/<program name>.json, the program's own
// settings as QSettings keeps them per application; another relative name
// sits in that directory too, and an absolute one is used as given.
type platformStore struct{}

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

func (platformStore) Load(file string) map[string]any {
	path, err := settingsPath(file)
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logf("settings: %v", err)
		}
		return nil
	}
	var vals map[string]any
	if err := json.Unmarshal(b, &vals); err != nil {
		logf("settings: %s: %v", path, err)
		return nil
	}
	return vals
}

func (platformStore) Save(file string, values map[string]any) error {
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
