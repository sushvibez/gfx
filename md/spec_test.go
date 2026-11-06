package md

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// The CommonMark spec's own examples (0.31.2, testdata/spec.json).
type specExample struct {
	Markdown string `json:"markdown"`
	HTML     string `json:"html"`
	Example  int    `json:"example"`
	Section  string `json:"section"`
}

func loadSpec(t *testing.T) []specExample {
	t.Helper()
	b, err := os.ReadFile("testdata/spec.json")
	if err != nil {
		t.Skip("no testdata/spec.json")
	}
	var ex []specExample
	if err := json.Unmarshal(b, &ex); err != nil {
		t.Fatal(err)
	}
	return ex
}

// TestCommonMarkSpec runs every example of the CommonMark spec (0.31.2,
// all 652) through the strict parser and the HTML writer, and wants the
// reference HTML exactly.
func TestCommonMarkSpec(t *testing.T) {
	ex := loadSpec(t)
	fail := map[string][]int{}
	for _, e := range ex {
		if got := HTML(ParseCommonMark(e.Markdown)); got != e.HTML {
			fail[e.Section] = append(fail[e.Section], e.Example)
			if len(fail) <= 3 {
				t.Errorf("example %d (%s)\n in:   %q\n want: %q\n got:  %q", e.Example, e.Section, e.Markdown, e.HTML, got)
			}
		}
	}
	if len(fail) > 0 {
		var secs []string
		for s := range fail {
			secs = append(secs, s)
		}
		sort.Strings(secs)
		for _, s := range secs {
			t.Errorf("%s: examples %v", s, fail[s])
		}
	}
}

// TestSpecShow prints chosen examples (MDSPEC=12,34) for debugging.
func TestSpecShow(t *testing.T) {
	if os.Getenv("MDSPEC") == "" {
		t.Skip("MDSPEC=n,m prints those examples")
	}
	want := map[int]bool{}
	for _, f := range splitInts(os.Getenv("MDSPEC")) {
		want[f] = true
	}
	for _, e := range loadSpec(t) {
		if want[e.Example] {
			fmt.Printf("=== %d (%s)\nIN:   %q\nWANT: %q\nGOT:  %q\n", e.Example, e.Section, e.Markdown, e.HTML, HTML(ParseCommonMark(e.Markdown)))
		}
	}
}

func splitInts(s string) []int {
	var out []int
	n, in := 0, false
	for _, c := range s + "," {
		if c >= '0' && c <= '9' {
			n, in = n*10+int(c-'0'), true
		} else if in {
			out = append(out, n)
			n, in = 0, false
		}
	}
	return out
}
