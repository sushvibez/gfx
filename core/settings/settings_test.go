package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sushvibez/gfx/core"
)

func TestFile(t *testing.T) {
	store := &Memory{}
	store.Save("app", map[string]any{"view/theme": "dark"})
	f := OpenIn(store, "app")
	if got := f.Value("view/theme", "light"); got != "dark" {
		t.Errorf("theme = %v, want the saved dark", got)
	}
	if got := f.Value("view/zoom", 1.5); got != 1.5 {
		t.Errorf("zoom = %v, want the default", got)
	}
	f.Set("view/theme", "dark")
	if f.Dirty() {
		t.Error("setting the same value made the file dirty")
	}
	f.Set("view/size", []int{3, 4}) // normalised as JSON holds it
	if v, _ := f.Get("view/size"); len(v.([]any)) != 2 || v.([]any)[1] != 4.0 {
		t.Errorf("size = %#v", v)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	if f.Dirty() || store.Load("app")["view/size"] == nil {
		t.Error("Save did not write")
	}
}

func TestBind(t *testing.T) {
	store := &Memory{}
	store.Save("", map[string]any{"grid": true, "zoom": 2.5})
	f := OpenIn(store, "")
	var grid core.Property[bool]
	var zoom core.Property[float64]
	var name core.Property[string]
	Bind(f, "grid", &grid)
	Bind(f, "zoom", &zoom)
	Bind(f, "name", &name)
	if !grid.Get() || zoom.Get() != 2.5 || name.Get() != "" {
		t.Errorf("loaded grid %v zoom %v name %q", grid.Get(), zoom.Get(), name.Get())
	}
	zoom.Set(3)
	name.Set("x")
	_ = f.Save()
	got := store.Load("")
	if got["zoom"] != 3.0 || got["name"] != "x" || got["grid"] != true {
		t.Errorf("saved %v", got)
	}
}

func TestPlatformStoreFile(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("localStorage store")
	}
	path := filepath.Join(t.TempDir(), "prefs.json")
	var s platformStore
	if got := s.Load(path); got != nil {
		t.Fatalf("Load of a missing file = %v, want nil", got)
	}
	if err := s.Save(path, map[string]any{"view/zoom": 2.0, "name": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	got := s.Load(path)
	if got["view/zoom"] != 2.0 || got["name"] != "x" {
		t.Errorf("Load = %v", got)
	}
}
