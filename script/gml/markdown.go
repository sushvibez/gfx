package gml

import (
	"github.com/sushvibez/gfx/md/mdrender"
	"github.com/sushvibez/gfx/md/mdui"
	"github.com/sushvibez/gfx/ui"
)

// buildMarkdown is a Text with textFormat: Text.MarkdownText. The page
// takes the text's colour, and the rest of its palette from whichever of
// the light or dark styles that colour reads on; it is w wide (400 when the
// item states no width, as a QML Text with no width wraps nowhere and a
// page has to wrap somewhere).
func buildMarkdown(s string, color ui.RGBA, sizePx float64, w float32, imgs *mdrender.Images) ui.Element {
	size := float32(sizePx)
	if size <= 0 {
		size = 14
	}
	if w <= 0 {
		w = 400
	}
	st := mdrender.DarkStyle(size)
	if 0.2126*color.R+0.7152*color.G+0.0722*color.B < 0.5 {
		st = mdrender.LightStyle(size)
	}
	st.Text = color
	return mdui.Markdown(s, w, mdui.Options{Style: st, Images: imgs})
}

// markdownImages is the document's picture source for Markdown text: files
// relative to the document, URLs, data: URIs. One a document, so a picture
// is read once however often the text is rebuilt.
func (doc *docState) markdownImages() *mdrender.Images {
	if doc == nil {
		return nil
	}
	if doc.mdImages == nil {
		doc.mdImages = mdrender.NewImages(doc.baseDir)
	}
	return doc.mdImages
}

// markdownWithLinks makes a Markdown text's links clickable when it has an
// onLinkActivated handler: a click on a link runs the handler with `link`
// bound to where it points, as QML's Text does.
func markdownWithLinks(el ui.Element, onLink []stmt, env *Env, doc *docState) ui.Element {
	md, ok := el.(*mdui.Element)
	if !ok || onLink == nil || doc == nil {
		return el
	}
	run := doc.handlerRunner(env)
	var px, py float32
	return ui.Div().Children(md).OnPointer(func(x, y float32, phase ui.DragPhase) {
		switch phase {
		case ui.DragStart:
			px, py = x, y
		case ui.DragEnd:
			if (x-px)*(x-px)+(y-py)*(y-py) > 16 {
				return // a drag, not a click
			}
			dest, ok := md.Click(x, y)
			if !ok {
				return
			}
			prev, had := env.vars["link"]
			env.vars["link"] = dest
			run(onLink, "onLinkActivated")
			if had {
				env.vars["link"] = prev
			} else {
				delete(env.vars, "link")
			}
		}
	})
}
