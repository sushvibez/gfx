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
	cur := refInt(idxRef)
	row := ui.Div().Row().ItemsCenter().Gap(spacing)
	delegate, _ := delegateChild(n)
	viewKey := doc.key(n)
	for i := 0; i < count; i++ {
		var dot ui.Element
		if delegate != nil {
			restore := enterRow(viewKey, delegate, env, doc, nil, i)
			el, err := build(delegate, env, doc)
			restore()
			if err != nil {
				return nil, err
			}
			dot = el
		} else {
			// The built-in dot: 8px, dim unless current.
			c := ui.RGBA{R: 0.6, G: 0.6, B: 0.6, A: 0.45}
			if i == cur {
				c = ui.RGBA{R: 0.6, G: 0.6, B: 0.6, A: 1}
			}
			dot = ui.Div().W(8).H(8).Rounded(4).Bg(c)
		}
		if interactive {
			pick, ref := i, idxRef
			dot = ui.Div().ItemsCenter().JustifyCenter().
				OnClick(func() { _ = ref.set(pick) }).Children(dot)
		}
		row.Children(dot)
	}
	return row, nil
}
