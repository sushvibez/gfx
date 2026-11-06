package shell

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The symbol fallback is a CHAIN: no one face carries every dingbat block a
// UI font skips, so each group contributes at most one face and they are
// tried in order. GFX_SYMBOL_FONT goes in front of all of them.
func TestSymbolFontChain(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.ttf")
	if err := os.WriteFile(first, []byte("FIRST"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Two files in the SAME group: only the first found is taken, since the
	// rest of a group is the same face installed elsewhere.
	a := filepath.Join(dir, "a.ttf")
	b := filepath.Join(dir, "b.ttf")
	for p, data := range map[string]string{a: "A", b: "B"} {
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saved := symbolFallbackFonts
	symbolFallbackFonts = [][]string{{a, b}, {b}}
	defer func() { symbolFallbackFonts = saved }()

	t.Setenv("GFX_SYMBOL_FONT", first)
	got := symbolFontChain()
	want := []string{"FIRST", "A", "B"}
	if len(got) != len(want) {
		t.Fatalf("chain of %d faces, want %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Errorf("face %d = %q, want %q", i, got[i], want[i])
		}
	}

	// Nothing installed and no override: no chain, and no crash.
	t.Setenv("GFX_SYMBOL_FONT", filepath.Join(dir, "missing.ttf"))
	symbolFallbackFonts = [][]string{{filepath.Join(dir, "nope.ttf")}}
	if got := symbolFontChain(); len(got) != 0 {
		t.Errorf("chain of %d faces with nothing installed, want none", len(got))
	}
}

// The default sans is what the SYSTEM resolves for "sans-serif" — on a
// current Ubuntu that is Ubuntu Sans, which is what Qt and a browser pick.
// Ours tried the older Ubuntu face first, whose typographic metrics are a
// pixel per line tighter, so every multi-line paragraph in the QML corpus
// wrapped to Qt's line breaks and then sat closer together than Qt's.
func TestDefaultSansPrefersTheSystemFace(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the fallback list is per-platform")
	}
	list := gpuiFallbackFonts()
	var sans, ubuntu int = -1, -1
	for i, p := range list {
		switch {
		case strings.Contains(p, "UbuntuSans["):
			if sans < 0 {
				sans = i
			}
		case strings.Contains(p, "Ubuntu[") || strings.Contains(p, "Ubuntu-Regular"):
			if ubuntu < 0 {
				ubuntu = i
			}
		}
	}
	if sans < 0 {
		t.Fatalf("Ubuntu Sans is not in the fallback list: %v", list)
	}
	if ubuntu >= 0 && sans > ubuntu {
		t.Errorf("Ubuntu Sans is tried at %d, after Ubuntu at %d", sans, ubuntu)
	}
}
