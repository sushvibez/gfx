package gml

// Qt Quick Controls' PageIndicator: one dot per page, the current one
// highlighted. The dots are its delegate (or a built-in dot when none is
// declared), each built with `index` in scope, and `interactive` makes them
// clickable so the view can be paged from the indicator.

import (
	"fmt"

	"github.com/sushvibez/gfx/ui"
)

// buildPageIndicator renders a PageIndicator element.
func buildPageIndicator(n *node, env *Env, doc *docState) (ui.Element, error) {
	count := 0
	var idxRef Ref
	interactive := false
	spacing := float32(6)
	for _, a := range n.attrs {
		switch a.name {
		case "count":
			if f, err := attrFloat(a, env); err == nil {
				count = int(f)
			}
		case "currentIndex":
			idxRef, _ = intBinding(doc, n, a, env)
		case "interactive":
			if b, err := attrBool(a, env); err == nil {
				interactive = b
			}
		case "spacing":
			if f, err := attrFloat(a, env); err == nil {
				spacing = f
			}
		case "delegate", "id", "width", "height", "z", "visible", "opacity",
			"enabled", "padding", "topPadding", "bottomPadding", "leftPadding",
			"rightPadding", "background", "contentItem", "anchors.centerIn",
			"anchors.horizontalCenter", "anchors.bottom", "anchors.bottomMargin",
			"anchors.top", "anchors.topMargin", "objectName":
			// layout/chrome — accepted, applied below or ignored
		case "onCurrentIndexChanged":
			// The index is the view's own property here (bound above), so the
			// handler that mirrors it back has nothing to do.
		default:
			if _, attached := attachedSizeAttr(a.name); attached {
				continue
			}
			return nil, fmt.Errorf("gml:%d: PageIndicator has no attribute %q", a.line, a.name)
		}
	}
	if idxRef == nil {
		idxRef = doc.floatProp(doc.key(n)+".currentIndex", 0)
	}
	doc.exposeIDProp(n, "currentIndex", idxRef)
	doc.exposeIDProp(n, "count", float64(count))
	// The row and its default dots are ui.PageIndicator's; a delegate draws
	// each dot in its row's scope instead.
	o := ui.PageIndicatorOpts{Spacing: spacing}
	var buildErr error
	if delegate, _ := delegateChild(n); delegate != nil {
		viewKey := doc.key(n)
		o.Dot = func(i int, _ bool) ui.Element {
			restore := enterRow(viewKey, delegate, env, doc, nil, i)
			el, err := build(delegate, env, doc)
			restore()
			if err != nil {
				buildErr = err
				return ui.Div()
			}
			return el
		}
	}
	var pick func(int)
	if interactive {
		ref := idxRef
		pick = func(i int) { _ = ref.set(i) }
	}
	row := ui.PageIndicator(count, refInt(idxRef), pick, o)
	if buildErr != nil {
		return nil, buildErr
	}
	return row, nil
}
