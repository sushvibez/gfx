package gml

import "sort"

// The order Component.onCompleted handlers run in.
//
// Qt documents the order as undefined. It is nonetheless fixed by how the
// engine keeps the handlers — each new one goes on the FRONT of a list
// that is then run from the front — and pages come to depend on it: a
// later sibling's handler fills what an earlier one reads. Measured on
// Qt 6.10.1 (TestCompletionOrder has the files and the output):
//
//   - The delegates of a view (Repeater, ListView, …) complete when the
//     view creates them, which is before any handler of the component
//     that holds the view: the views last to first, the rows of each in
//     order.
//   - Otherwise an object's handler runs before those of the objects it
//     holds, and siblings run LAST to first, as they are written.
//   - Where a component's root and the place it is used both have one,
//     the component's own runs first.
//
// Handlers are collected in build order, which is none of that: parents
// first, siblings first to last, and the children that draw nothing (a
// Timer, a model) apart from the ones that do. So while a document has
// not completed, every element built is numbered and its parent's number
// and its place in the source are kept (builtElem); the handlers carry
// their element's number, and that is enough to walk the tree Qt's way.

// builtElem is one element built before the document completed: the
// element it was built inside, and where it is written.
type builtElem struct {
	parent    int
	line, ord int
}

// noteBuilt numbers an element being built inside element parent and
// returns the number. Nothing is kept once the document has completed.
func (doc *docState) noteBuilt(parent int, n *node) int {
	if doc.completed {
		return 0
	}
	if len(doc.built) == 0 {
		doc.built = append(doc.built, builtElem{}) // 0 is "no element"
	}
	doc.built = append(doc.built, builtElem{parent: parent, line: n.line, ord: n.ord})
	return len(doc.built) - 1
}

// orderCompleted returns the collected handlers in the order Qt runs them.
func (doc *docState) orderCompleted(tasks []completedTask) []completedTask {
	if len(tasks) < 2 {
		return tasks
	}
	out := make([]completedTask, 0, len(tasks))
	var static []completedTask
	var views []string // in the order first seen
	rows := map[string][]string{}
	byRow := map[string][]completedTask{}
	for _, t := range tasks {
		if t.view == "" {
			static = append(static, t)
			continue
		}
		if _, seen := rows[t.view]; !seen {
			views = append(views, t.view)
		}
		if _, seen := byRow[t.row]; !seen {
			rows[t.view] = append(rows[t.view], t.row)
		}
		byRow[t.row] = append(byRow[t.row], t)
	}
	for i := len(views) - 1; i >= 0; i-- {
		for _, row := range rows[views[i]] {
			out = doc.appendCompletedTree(out, byRow[row])
		}
	}
	return doc.appendCompletedTree(out, static)
}

// appendCompletedTree appends tasks as Qt runs them: an element's own
// handlers, then those of what it holds, siblings last to first.
func (doc *docState) appendCompletedTree(out, tasks []completedTask) []completedTask {
	// The part of the element tree these handlers hang on: each handler's
	// element and its ancestors.
	own := map[int][]int{}  // element -> its handlers, as indices into tasks
	kids := map[int][]int{} // element -> the elements under it that matter
	seen := map[int]bool{0: true}
	for i, t := range tasks {
		el := t.elem
		if el < 0 || el >= len(doc.built) {
			el = 0
		}
		own[el] = append(own[el], i)
		for e := el; !seen[e]; {
			seen[e] = true
			p := doc.built[e].parent
			kids[p] = append(kids[p], e)
			e = p
		}
	}
	var walk func(el int)
	walk = func(el int) {
		// A component's root and its use site are one element, and the
		// use site's handler was collected first: the component's own
		// runs before it.
		for i := len(own[el]) - 1; i >= 0; i-- {
			out = append(out, tasks[own[el][i]])
		}
		ks := kids[el]
		sort.Slice(ks, func(a, b int) bool {
			ea, eb := doc.built[ks[a]], doc.built[ks[b]]
			if ea.line != eb.line {
				return ea.line < eb.line
			}
			if ea.ord != eb.ord {
				return ea.ord < eb.ord
			}
			return ks[a] < ks[b]
		})
		for k := len(ks) - 1; k >= 0; k-- {
			walk(ks[k])
		}
	}
	walk(0)
	return out
}
