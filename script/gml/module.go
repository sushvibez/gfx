package gml

// Minimal QML module resolution: `import <module>` finds a directory with
// a qmldir manifest (searched relative to the document and its ancestors,
// including the conventional imports/ and styles/ roots), then
//   - `singleton Type 1.0 Type.qml` files load once per document: their
//     property declarations and functions become a shared object exposed
//     under the type name (Style.mainColor, Style.resize(x)).
//   - `Type 1.0 Type.qml` entries register as file components, so module
//     types instantiate exactly like sibling-directory components.
// Qt's own modules (QtQuick, QtQuick.Controls, ...) have no directory: the
// element vocabulary for those is built in. Any other module that is not
// found is recorded (Document.UnresolvedImports), and a strict document
// does not build with one missing.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// resolveImports loads a parsed file's import list into the document:
// module component types and singleton objects. Safe to call repeatedly
// (component files import too); modules load once.
func (doc *docState) resolveImports(root *node, env *Env) {
	doc.loadControlsStyle(env)
	for _, name := range root.imports {
		if doc.modulesSeen == nil {
			doc.modulesSeen = map[string]bool{}
		}
		if doc.modulesSeen[name] {
			continue
		}
		doc.modulesSeen[name] = true
		dir := findModuleDir(doc.baseDir, name)
		if dir == "" {
			if !qtModule(name) {
				doc.unresolved = append(doc.unresolved, name)
			}
			continue
		}
		doc.loadModule(name, dir, env)
	}
}

// qtModule reports a module that is Qt's own — QtQuick, QtQuick.Controls,
// QtQml.Models, Qt5Compat.GraphicalEffects, Qt.labs.settings — which no
// directory has to provide: what the engine models of them is built in.
func qtModule(name string) bool {
	if !strings.HasPrefix(name, "Qt") {
		return false
	}
	if len(name) == 2 {
		return true
	}
	c := name[2]
	return c == '.' || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// UnresolvedImports lists the modules this document (and the component
// files it has loaded so far) imports that are neither Qt's own nor found
// as a directory with a qmldir near the document — sorted, each once. A
// page written for a C++ application names the modules its C++ registers;
// here those have nowhere to come from, and their types do not exist.
func (d *Document) UnresolvedImports() []string {
	out := append([]string(nil), d.doc.unresolved...)
	sort.Strings(out)
	return out
}

// unresolvedImportsError is the build error of a strict document that
// imports modules nothing provides, nil when there are none.
func (doc *docState) unresolvedImportsError() error {
	if len(doc.unresolved) == 0 {
		return nil
	}
	mods := append([]string(nil), doc.unresolved...)
	sort.Strings(mods)
	return fmt.Errorf("import %s: no such module (looked for <module>/qmldir beside the document and in its parent directories, and under their imports/ and styles/)",
		strings.Join(mods, ", "))
}

// findModuleDir searches for <module>/qmldir near the document: each
// ancestor directory A tries A/<module>, A/imports/<module> and
// A/styles/<module> (the conventional Qt project layout).
func findModuleDir(baseDir, module string) string {
	rel := filepath.FromSlash(strings.ReplaceAll(module, ".", "/"))
	dir := baseDir
	for i := 0; i < 6 && dir != ""; i++ {
		for _, cand := range []string{
			filepath.Join(dir, rel),
			filepath.Join(dir, "imports", rel),
			filepath.Join(dir, "styles", rel),
		} {
			if _, err := os.Stat(filepath.Join(cand, "qmldir")); err == nil {
				return cand
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// loadModule reads a module directory's qmldir and registers its types.
func (doc *docState) loadModule(module, dir string, env *Env) {
	data, err := os.ReadFile(filepath.Join(dir, "qmldir"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch {
		case f[0] == "singleton" && len(f) >= 4:
			doc.loadSingleton(f[1], filepath.Join(dir, f[len(f)-1]), env)
		case f[0] == "module" || f[0] == "internal" || f[0] == "typeinfo" ||
			f[0] == "plugin" || f[0] == "depends" || f[0] == "prefer" ||
			f[0] == "optional" || f[0] == "designersupported":
			// manifest metadata / native plugins: nothing to load
		case len(f) >= 3 && (strings.HasSuffix(f[len(f)-1], ".qml") || strings.HasSuffix(f[len(f)-1], ".gml")):
			// `Type 1.0 Type.qml` (or native .gml) — a module component type.
			if doc.moduleTypes == nil {
				doc.moduleTypes = map[string]string{}
			}
			path := filepath.Join(dir, f[len(f)-1])
			doc.moduleTypes[f[0]] = path
			// Also index by module, because QML imports are PER FILE: a
			// style module shadows Button only in the files that import it.
			if doc.moduleTypesBy == nil {
				doc.moduleTypesBy = map[string]map[string]string{}
			}
			if doc.moduleTypesBy[module] == nil {
				doc.moduleTypesBy[module] = map[string]string{}
			}
			doc.moduleTypesBy[module][f[0]] = path
		}
	}
}

// loadSingleton parses a `pragma Singleton` file, declares its properties
// and functions in a private scope, and exposes them as one shared object
// under the type name in the importing scope.
func (doc *docState) loadSingleton(name, path string, env *Env) {
	if _, exists := env.vars[name]; exists {
		return
	}
	src, err := os.ReadFile(path)
	if err != nil {
		logf("gml: singleton %s: %v", name, err)
		return
	}
	root, err := Parse(string(src))
	if err != nil {
		logf("gml: singleton %s: %v", name, err)
		return
	}
	// A private scope + scratch registry: the singleton's bindings see its
	// own properties, not the importing document's. A fully initialized
	// docState (not a bare literal) so declareProps' registries all exist.
	senv := NewEnv()
	scratch := newDocState()
	scratch.baseDir = filepath.Dir(path)
	scratch.resolveImports(root, senv) // singletons may import modules too
	declareFuncs(root, senv)           // first: property defaults may call them
	if err := declareProps(root, senv, scratch); err != nil {
		logf("gml: singleton %s: %v", name, err)
		return
	}

	obj := &elemObject{props: map[string]any{}, methods: map[string]any{}}
	for _, d := range root.props {
		if ref, ok := scratch.props[d.name]; ok {
			obj.setProp(d.name, ref)
		}
	}
	for _, f := range root.funcs {
		if fn, ok := senv.vars[f.name]; ok {
			obj.methods[f.name] = fn
		}
	}
	// Qualified self-references (id.resize(...)) inside the singleton reach
	// its functions through the id object too.
	if id := nodeID(root); id != "" {
		if io, ok := scratch.elemObjs.get2(scratch.intern(id)); ok {
			io.methods = obj.methods
		}
	}
	// Retry forward-referencing defaults now that functions and the id
	// object are fully wired.
	scratch.retryPendingProps()
	env.vars[name] = obj
}

// loadControlsStyle honors qtquickcontrols2.conf's `Style=<module>` — how a
// Qt app selects a Controls style for the WHOLE application (QQuickStyle /
// QT_QUICK_CONTROLS_STYLE do the same). Unlike a per-file import, it
// restyles every Controls type in every file, so it is tracked separately
// from the import lists. Looked up once, in the document's directory and
// its ancestors.
func (doc *docState) loadControlsStyle(env *Env) {
	if doc.styleChecked || doc.baseDir == "" {
		return
	}
	doc.styleChecked = true
	dir := doc.baseDir
	for i := 0; i < 6 && dir != ""; i++ {
		data, err := os.ReadFile(filepath.Join(dir, "qtquickcontrols2.conf"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "Style") {
					continue
				}
				if _, v, ok := strings.Cut(line, "="); ok {
					name := strings.TrimSpace(v)
					if sdir := findModuleDir(doc.baseDir, name); sdir != "" {
						doc.styleModule = name
						doc.loadModule(name, sdir, env)
					}
				}
			}
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}
