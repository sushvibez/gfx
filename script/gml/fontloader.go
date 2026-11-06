package gml

// QML FontLoader: loads a font file and exposes its family name so markup
// can use `font.family: loader.name`. The face registers process-globally
// in render's font registry (Qt's engine is global the same way); status
// mirrors FontLoader.Ready(1)/Error(3).

import (
	"os"

	"github.com/sushvibez/gfx/render"
)

// loadedFonts caches instantiations by resolved path — singletons and file
// components re-run declareProps per instantiation, and the parse+register
// work is identical each time.
var loadedFonts = map[string]map[string]any{}

// instantiateFontLoader evaluates a FontLoader element into its property
// object ({name, source, status}) and registers the face.
func instantiateFontLoader(n *node, env *Env, doc *docState) map[string]any {
	src := ""
	for _, a := range n.attrs {
		if a.name == "source" && a.expr != nil {
			if v, err := a.expr.eval(env); err == nil {
				src = str(v)
			}
		}
	}
	obj := map[string]any{"name": "", "source": src, "status": float64(3) /*Error*/}
	path := resolveFontPath(doc.baseDir, src)
	if path == "" {
		logf("gml:%d: FontLoader: cannot resolve %q", n.line, src)
		return obj
	}
	if cached, ok := loadedFonts[path]; ok {
		return cached
	}
	b, err := os.ReadFile(path)
	if err != nil {
		logf("gml:%d: FontLoader: %v", n.line, err)
		return obj
	}
	fam, err := render.FontFamilyName(b)
	if err != nil || fam == "" {
		logf("gml:%d: FontLoader: no family name in %s: %v", n.line, path, err)
		return obj
	}
	render.RegisterFontBytes(fam, b)
	obj["name"] = fam
	obj["status"] = float64(1) // Ready
	loadedFonts[path] = obj
	return obj
}

// resolveFontPath maps a QML font source to an existing filesystem path
// (resolveImagePath's rules — same qrc-tree searching — plus an existence
// check, since a missing font must fail the loader, not the renderer).
func resolveFontPath(baseDir, src string) string {
	p := resolveImagePath(baseDir, src)
	if p == "" || !statFile(p) {
		return ""
	}
	return p
}
