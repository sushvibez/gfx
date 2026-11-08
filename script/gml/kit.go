package gml

// Retained state for the controls kit's gml controls that own some:
// spinners and indeterminate bars (ui.Activity).

import "github.com/sushvibez/gfx/ui"

// activity is the element's own ui.Activity, the loop an indeterminate
// ProgressBar or a BusyIndicator draws from.
func (doc *docState) activity(n *node) *ui.Activity {
	if doc.activities == nil {
		doc.activities = map[string]*ui.Activity{}
	}
	key := doc.key(n)
	a := doc.activities[key]
	if a == nil {
		a = &ui.Activity{}
		doc.activities[key] = a
	}
	if doc.activitySeen == nil {
		doc.activitySeen = map[string]bool{}
	}
	doc.activitySeen[key] = true
	return a
}

// sweepActivities stops the loops of spinners and indeterminate bars that
// the last build did not reach (their page was navigated away, a Loader
// swapped them out): a loop left running keeps the frame loop busy for
// ever.
func (doc *docState) sweepActivities() {
	for k, a := range doc.activities {
		if !doc.activitySeen[k] {
			a.Run(false)
			delete(doc.activities, k)
		}
	}
	clear(doc.activitySeen)
}
