package md

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// GitHub's own examples of its extensions — tables, strikethrough, the
// extended autolinks and the tag filter — from cmark-gfm's spec.txt
// (testdata/gfm-spec.txt), through Parse.
func TestGFMExtensions(t *testing.T) {
	f, err := os.Open("testdata/gfm-spec.txt")
	if err != nil {
		t.Skip("no testdata/gfm-spec.txt")
	}
	defer f.Close()
	const fence = "````````````````````````````````"
	type ex struct{ kind, in, want string }
	var all []ex
	sc := bufio.NewScanner(f)
	var cur *ex
	var in, out []string
	inHTML := false
	for sc.Scan() {
		ln := sc.Text()
		switch {
		case cur == nil && strings.HasPrefix(ln, fence+" example "):
			kind := strings.TrimSpace(strings.TrimPrefix(ln, fence+" example "))
			if kind == "disabled" {
				continue
			}
			cur, in, out, inHTML = &ex{kind: kind}, nil, nil, false
		case cur != nil && ln == fence:
			cur.in, cur.want = strings.Join(in, ""), strings.Join(out, "")
			all = append(all, *cur)
			cur = nil
		case cur != nil && ln == "." && !inHTML:
			inHTML = true
		case cur != nil:
			ln = strings.ReplaceAll(ln, "→", "\t") + "\n"
			if inHTML {
				out = append(out, ln)
			} else {
				in = append(in, ln)
			}
		}
	}
	if len(all) < 20 {
		t.Fatalf("read only %d extension examples", len(all))
	}
	for _, e := range all {
		if got := HTML(Parse(e.in)); got != e.want {
			t.Errorf("%s example\n in:   %q\n want: %q\n got:  %q", e.kind, e.in, e.want, got)
		}
	}
}
