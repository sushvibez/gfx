package md

import (
	"math/rand"
	"strings"
	"testing"
)

// No input panics: a hundred thousand strings built from the characters
// the parser treats specially.
func TestRandomInputNeverPanics(t *testing.T) {
	alphabet := []string{"*", "_", "~", "`", "[", "]", "(", ")", "!", "<", ">", "&", "#", "-", "+", "1.", "|", ":",
		"\\", " ", "  ", "\n", "\n\n", "\t", "a", "word", "http://x.y", "www.", "```", "    ", "> ", "- [ ] ", "&amp;", "<div>", "\""}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 100000; i++ {
		var b strings.Builder
		for n := r.Intn(30); n >= 0; n-- {
			b.WriteString(alphabet[r.Intn(len(alphabet))])
		}
		src := b.String()
		func() {
			defer func() {
				if e := recover(); e != nil {
					t.Fatalf("panic on %q: %v", src, e)
				}
			}()
			HTML(Parse(src))
		}()
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"# a", "- [x] b", "| a |\n| - |\n| b |", "[a](b \"c\")", "***x***", "> q\nlazy",
		"\tfoo\tbar", "-\t\tx", "<a b='c' d=e/>", "<!--->", "[x]: <>\n===", "&HilbertSpace;",
		"a[^1]\n\n[^1]: n\n    m", "T\n: d\n: e\n\n  f"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		HTML(Parse(s))
		HTML(ParseCommonMark(s))
	})
}
