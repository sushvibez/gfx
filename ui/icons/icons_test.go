package icons

import (
	"slices"
	"testing"
)

func TestNamesSortedAndComplete(t *testing.T) {
	names := Names()
	if !slices.IsSorted(names) {
		t.Error("Names is not sorted")
	}
	for _, n := range names {
		if !Has(n) {
			t.Errorf("Names lists %q but Has says no", n)
		}
	}
	if !slices.Contains(names, "back") {
		t.Error(`the built-in "back" glyph is missing`)
	}
}

// Parse caches per name and tint, and registering a glyph drops the cache
// so a replaced body is not served stale.
func TestParseCaches(t *testing.T) {
	a, err := Parse("back", "#ff0000")
	if err != nil || a == nil {
		t.Fatalf("Parse(back) = %v, %v", a, err)
	}
	if b, _ := Parse("back", "#ff0000"); b != a {
		t.Error("the same name and tint parsed twice")
	}
	if c, _ := Parse("back", "#00ff00"); c == a {
		t.Error("a different tint returned the cached glyph")
	}
	if _, err := Parse("no-such-glyph", ""); err == nil {
		t.Error("an unknown name parsed")
	}
	Register("test-glyph", `<path d="M0 0L24 24"/>`)
	first, _ := Parse("test-glyph", "")
	Register("test-glyph", `<path d="M24 0L0 24"/>`)
	if again, _ := Parse("test-glyph", ""); again == first {
		t.Error("a re-registered glyph was served from the cache")
	}
	if !slices.Contains(Names(), "test-glyph") {
		t.Error("a registered glyph is not in Names")
	}
}
