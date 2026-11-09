package gml

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sushvibez/gfx/ui"
)

// patchVerify (GFX_PATCH_VERIFY=1) rebuilds the whole document from
// scratch after every frame that patched something and compares the two
// trees, reporting the first place they disagree. A debugging aid for the
// subtree patching: a patched tree must be what a full rebuild produces.
var patchVerify = os.Getenv("GFX_PATCH_VERIFY") != ""

// patchVerifyAll (GFX_PATCH_VERIFY=all) verifies EVERY frame, patched or
// not — the baseline: a cached tree that a from-scratch build does not
// reproduce even without patching is the verifier's (or the cache's) own
// disagreement, not patching's.
var patchVerifyAll = os.Getenv("GFX_PATCH_VERIFY") == "all"

func (doc *docState) verifyPatched(patched ui.Element, root *node, env *Env) {
	// The fresh build writes the registry (and its overlay and loose
	// lists, registry.go); all of it goes back as it was.
	savedElems := append([]ui.Element(nil), doc.elems.v...)
	savedOvEl := append([]ui.Element(nil), doc.reg.ovEl...)
	savedOvFrame := append([]uint32(nil), doc.reg.ovFrame...)
	savedAtBuild := append([]uint32(nil), doc.reg.atBuild...)
	savedAtFrame := append([]uint32(nil), doc.reg.atFrame...)
	savedLoose, savedLooseEnts := len(doc.reg.loose), len(doc.reg.looseEnts)
	savedSeen := append([]int(nil), doc.keySeen.v...)
	savedLast := doc.lastEntry
	noSubtreeCache = true
	doc.keySeen.clear()
	fresh, err := build(root, env, doc)
	noSubtreeCache = false
	clear(doc.elems.v[min(len(savedElems), len(doc.elems.v)):])
	copy(doc.elems.v, savedElems)
	clear(doc.reg.ovFrame[min(len(savedOvFrame), len(doc.reg.ovFrame)):])
	copy(doc.reg.ovEl, savedOvEl)
	copy(doc.reg.ovFrame, savedOvFrame)
	clear(doc.reg.atBuild[min(len(savedAtBuild), len(doc.reg.atBuild)):])
	clear(doc.reg.atFrame[min(len(savedAtFrame), len(doc.reg.atFrame)):])
	copy(doc.reg.atBuild, savedAtBuild)
	copy(doc.reg.atFrame, savedAtFrame)
	doc.reg.loose = doc.reg.loose[:savedLoose]
	doc.reg.looseEnts = doc.reg.looseEnts[:savedLooseEnts]
	copy(doc.keySeen.v, savedSeen)
	doc.lastEntry = savedLast
	if err != nil {
		fmt.Fprintf(os.Stderr, "gml: verify: fresh build failed: %v\n", err)
		return
	}
	if where := treeDiff(patched, fresh, "root"); where != "" {
		fmt.Fprintf(os.Stderr, "gml: verify: patched tree differs from a fresh build at %s%s\n", where, doc.entryFor(patched, where))
	}
}

// entryFor names the nearest cached subtree entry holding the element at
// a treeDiff path ("root/0/3/1 (...)"), so a mismatch can be read back to
// the markup that built it.
func (doc *docState) entryFor(root ui.Element, where string) string {
	path := strings.Fields(where)[0]
	segs := strings.Split(path, "/")[1:]
	chain := []ui.Element{root}
	cur := root
	for _, sg := range segs {
		i, err := strconv.Atoi(sg)
		kids := ui.Children(cur)
		if err != nil || i < 0 || i >= len(kids) || kids[i] == nil {
			break
		}
		cur = kids[i]
		chain = append(chain, cur)
	}
	for k := len(chain) - 1; k >= 0; k-- {
		for _, e := range doc.subtrees.v {
			if e != nil && e.el == chain[k] {
				return fmt.Sprintf(" [entry %s, %d levels up]", doc.keyStr(doc.keyID(e.node)), len(chain)-1-k)
			}
		}
	}
	return ""
}

// treeDiff compares two element trees structurally and reports the path
// of the first difference ("" when alike): types, fixed sizes, text,
// fill colour, child counts.
func treeDiff(a, b ui.Element, path string) string {
	if (a == nil) != (b == nil) {
		return path + " (one side nil)"
	}
	if a == nil {
		return ""
	}
	if fmt.Sprintf("%T", a) != fmt.Sprintf("%T", b) {
		return fmt.Sprintf("%s (%T vs %T)", path, a, b)
	}
	aw, ah := ui.FixedSize(a)
	bw, bh := ui.FixedSize(b)
	if aw != bw || ah != bh {
		return fmt.Sprintf("%s (%T fixed size %vx%v vs %vx%v)", path, a, aw, ah, bw, bh)
	}
	if at, ok := ui.TextOf(a); ok {
		if bt, _ := ui.TextOf(b); at != bt {
			return fmt.Sprintf("%s (text %q vs %q)", path, at, bt)
		}
	}
	ac, aok := ui.FillColor(a)
	bc, bok := ui.FillColor(b)
	if aok != bok || ac != bc {
		return fmt.Sprintf("%s (%T fill %v vs %v)", path, a, ac, bc)
	}
	ak, bk := ui.Children(a), ui.Children(b)
	if len(ak) != len(bk) {
		return fmt.Sprintf("%s (%T has %d children vs %d)", path, a, len(ak), len(bk))
	}
	for i := range ak {
		if d := treeDiff(ak[i], bk[i], fmt.Sprintf("%s/%d", path, i)); d != "" {
			return d
		}
	}
	return ""
}
