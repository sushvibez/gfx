package gml

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// TestCorpusSweep writes what every page of a corpus BUILDS, without a GPU,
// so a change to the evaluator can be compared before and after:
//
//	GFX_CORPUS_SWEEP=before.txt GFX_QML_CORPUS=<qml-examples> go test -run TestCorpusSweep ./script/gml
//	... the change ...
//	GFX_CORPUS_SWEEP=after.txt  GFX_QML_CORPUS=<qml-examples> go test -run TestCorpusSweep ./script/gml
//	diff before.txt after.txt
//
// Per page: the load error if any, everything the engine logged while the
// page settled (its warnings), and the built tree as element type and text.
// Geometry is left out on purpose: half the corpus animates on the wall
// clock, and a rectangle that differs between two runs of the same binary
// says nothing. GFX_SWEEP_EXTRA adds files by glob (';'-separated) — the
// .gml examples, an application's own markup. GFX_SWEEP_STEPS runs the
// pages under that execution budget (budget.go): a sweep that comes out
// the same under a small one says how far real pages are from the default.
func TestCorpusSweep(t *testing.T) {
	out := os.Getenv("GFX_CORPUS_SWEEP")
	if out == "" {
		t.Skip("set GFX_CORPUS_SWEEP=<file>: writes the built tree of every corpus page")
	}
	var files []string
	if root := os.Getenv("GFX_QML_CORPUS"); root != "" {
		m, _ := filepath.Glob(filepath.Join(root, "examples", "*", "Main.qml"))
		files = append(files, m...)
	}
	for _, g := range strings.Split(os.Getenv("GFX_SWEEP_EXTRA"), ";") {
		if g = strings.TrimSpace(g); g != "" {
			m, _ := filepath.Glob(g)
			files = append(files, m...)
		}
	}
	sort.Strings(files)
	if n, _ := strconv.ParseInt(os.Getenv("GFX_SWEEP_STEPS"), 10, 64); n > 0 {
		defer SetBudget(SetBudget(Budget{Steps: n}))
	}
	if len(files) == 0 {
		t.Skip("no pages: set GFX_QML_CORPUS and/or GFX_SWEEP_EXTRA")
	}
	var sb strings.Builder
	for _, path := range files {
		fmt.Fprintf(&sb, "=== %s\n", filepath.ToSlash(path))
		logged := captureStderr(t, func() {
			// A page that panics is a finding, not the end of the sweep.
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(&sb, "panic: %v\n", r)
				}
			}()
			d, err := Load(path, NewEnv())
			if err != nil {
				fmt.Fprintf(&sb, "load: %v\n", err)
				return
			}
			view := d.View()
			var root ui.Element
			u := ui.New(nil, nil, func() ui.Element { root = view(); return root })
			for i := 0; i < 20; i++ {
				u.Layout(900, 700)
			}
			sweepDump(&sb, root, 0)
		})
		if logged != "" {
			fmt.Fprintf(&sb, "--- logged\n%s\n", logged)
		}
	}
	if err := os.WriteFile(out, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d pages written to %s", len(files), out)
}

// sweepDump writes one line per element: its Go type, and its text if it
// is a text element.
func sweepDump(sb *strings.Builder, el ui.Element, depth int) {
	if el == nil || depth > 200 {
		return
	}
	fmt.Fprintf(sb, "%*s%T", depth, "", el)
	if s, ok := ui.TextOf(el); ok {
		fmt.Fprintf(sb, " %q", s)
	}
	sb.WriteByte('\n')
	for _, c := range ui.Children(el) {
		sweepDump(sb, c, depth+1)
	}
}

// captureStderr runs fn with os.Stderr pointed at a file and returns what
// was written — the engine reports its warnings there (logf).
func captureStderr(t *testing.T, fn func()) string {
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = f
	defer func() { os.Stderr = old; f.Close() }()
	fn()
	os.Stderr = old
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	return strings.TrimSpace(string(b))
}
