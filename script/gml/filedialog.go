package gml

// QML file dialogs (Qt.labs.platform / QtQuick.Dialogs' FileDialog and
// FolderDialog). Qt hands these to the desktop's native dialog; we have no
// such thing, so this is a real in-app browser: a modal panel listing a
// directory, with parent navigation, single or multi selection, a name
// filter, and Open/Save/Cancel. It renders through the same overlay
// machinery as Popup, so it floats above the page and dismisses on an
// outside click.
//
// The QML contract: open()/close() methods on the id, `selectedFile` /
// `selectedFiles` / `currentFolder` properties, and onAccepted / onRejected
// handlers. fileMode picks Open/Save/OpenMultiple.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sushvibez/gfx/ui"
)

// fileDialogState is one dialog's retained state, keyed by element id so it
// survives the per-frame rebuild.
type fileDialogState struct {
	// open and sel are properties, not plain Go values: showing the dialog
	// or picking a file has to invalidate the document's build cache, and
	// only property writes do that.
	open Ref
	sel  Ref // []any of absolute paths
	dir  string
	err  string
}

// selection returns the picked absolute paths.
func (st *fileDialogState) selection() []string {
	arr, _ := st.sel.get().([]any)
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		out = append(out, str(v))
	}
	return out
}

// pick selects a path — replacing the selection unless multi is set, which
// is FileDialog.OpenMultipleFiles.
func (st *fileDialogState) pick(path string, multi bool) {
	if !multi {
		_ = st.sel.set([]any{path})
		return
	}
	for _, p := range st.selection() {
		if p == path {
			return
		}
	}
	_ = st.sel.set(append(toAnySlice(st.selection()), path))
}

// clearSelection drops the picked files (navigating to another directory).
func (st *fileDialogState) clearSelection() { _ = st.sel.set([]any{}) }

// isPicked reports whether path is in the selection.
func (st *fileDialogState) isPicked(path string) bool {
	for _, p := range st.selection() {
		if p == path {
			return true
		}
	}
	return false
}

// isOpen reports whether the dialog is showing.
func (st *fileDialogState) isOpen() bool {
	b, _ := toBool(st.open.get())
	return b
}

// setOpen shows or hides the dialog (and re-renders, since the write bumps
// the property revision).
func (st *fileDialogState) setOpen(v bool) { _ = st.open.set(v) }

// buildFileDialog renders a FileDialog / FolderDialog element: nothing in
// the flow, and a floating panel while it is open.
func buildFileDialog(n *node, env *Env, doc *docState) (ui.Element, error) {
	folderOnly := n.typeName == "FolderDialog"
	title := "Open"
	if folderOnly {
		title = "Choose Folder"
	}
	var filters []string
	mode := "OpenFile"
	var onAccepted, onRejected []stmt
	startDir := ""
	for _, a := range n.attrs {
		switch a.name {
		case "id", "visible", "modality", "flags", "options", "defaultSuffix",
			"acceptLabel", "rejectLabel", "selectedNameFilter", "currentFile",
			"selectedFile", "selectedFiles", "folder", "selectedFolder":
			// accepted; state lives in the dialog itself
		case "title":
			if v, err := a.expr.eval(env); err == nil {
				title = str(v)
			}
		case "nameFilters":
			if v, err := a.expr.eval(env); err == nil {
				if arr, ok := v.([]any); ok {
					for _, f := range arr {
						filters = append(filters, str(f))
					}
				}
			}
		case "fileMode":
			if v, err := a.expr.eval(env); err == nil {
				mode = strings.TrimPrefix(str(v), "FileDialog.")
			}
		case "currentFolder", "initialFolder":
			if v, err := a.expr.eval(env); err == nil {
				startDir = fileURLPath(str(v))
			}
		case "onAccepted":
			onAccepted = a.stmt
		case "onRejected":
			onRejected = a.stmt
		default:
			return nil, fmt.Errorf("gml:%d: %s has no attribute %q", a.line, n.typeName, a.name)
		}
	}

	st := doc.fileDialog(doc.key(n), startDir)
	// The id exposes the QML contract: open()/close() and the selection.
	sel := st.sortedSelection()
	doc.exposeIDProp(n, "selectedFile", firstOr(sel, ""))
	doc.exposeIDProp(n, "currentFile", firstOr(sel, ""))
	doc.exposeIDProp(n, "selectedFiles", toAnySlice(sel))
	doc.exposeIDProp(n, "selectedFolder", pathURL(st.dir))
	doc.exposeIDProp(n, "currentFolder", pathURL(st.dir))
	doc.exposeIDProp(n, "folder", pathURL(st.dir))
	doc.exposeIDProp(n, "visible", st.isOpen())
	if id := nodeID(n); id != "" {
		if obj := doc.elemObjs.get(doc.here(id)); obj != nil {
			if obj.methods == nil {
				obj.methods = map[string]any{}
			}
			obj.methods["open"] = jsFunc(func([]any) (any, error) {
				st.setOpen(true)
				return nil, nil
			})
			obj.methods["close"] = jsFunc(func([]any) (any, error) {
				st.setOpen(false)
				return nil, nil
			})
		}
	}
	if !st.isOpen() {
		return ui.Div(), nil
	}

	run := doc.handlerRunner(env)
	accept := func() {
		st.setOpen(false)
		if onAccepted != nil {
			run(onAccepted, "onAccepted")
		}
	}
	reject := func() {
		st.setOpen(false)
		if onRejected != nil {
			run(onRejected, "onRejected")
		}
	}
	panel := fileDialogPanel(st, title, filters, folderOnly, mode, accept, reject)
	return ui.Modal(panel, ui.RGBA{A: 0.45}, reject), nil
}

// fileDialogPanel wraps ui.FileBrowser — the reusable widget — with this
// dialog's retained state and QML labels.
func fileDialogPanel(st *fileDialogState, title string, filters []string,
	folderOnly bool, mode string, accept, reject func()) ui.Element {

	label := "Open"
	switch {
	case folderOnly:
		label = "Choose"
	case mode == "SaveFile":
		label = "Save"
	}
	multi := mode == "OpenMultipleFiles"
	return ui.FileBrowser{
		Title:       title,
		Dir:         st.dir,
		Filters:     filters,
		Selected:    st.selection(),
		FolderOnly:  folderOnly,
		Multi:       multi,
		AcceptLabel: label,
		OnNavigate: func(dir string) {
			st.dir = dir
			st.clearSelection()
		},
		OnPick:   func(path string) { st.pick(path, multi) },
		OnAccept: accept,
		OnCancel: reject,
	}.Panel()
}

// fileDialog returns the retained state for a dialog, starting in dir (or
// the working directory).
func (doc *docState) fileDialog(key, dir string) *fileDialogState {
	if doc.fileDialogs == nil {
		doc.fileDialogs = map[string]*fileDialogState{}
	}
	if st, ok := doc.fileDialogs[key]; ok {
		return st
	}
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home
		} else {
			dir, _ = os.Getwd()
		}
	}
	st := &fileDialogState{
		dir:  dir,
		open: doc.boolProp(key+"|open", false),
		sel:  doc.varProp(key+"|selected", []any{}),
	}
	doc.fileDialogs[key] = st
	return st
}

// sortedSelection returns the picked paths as file: URLs, in a stable order.
func (st *fileDialogState) sortedSelection() []string {
	out := make([]string, 0, 4)
	for _, p := range st.selection() {
		out = append(out, pathURL(p))
	}
	sort.Strings(out)
	return out
}

// pathURL renders a filesystem path as the file: URL QML properties carry.
func pathURL(p string) string {
	if p == "" {
		return ""
	}
	return "file://" + filepath.ToSlash(p)
}

// fileURLPath is pathURL's inverse: QML hands folder properties as URLs.
func fileURLPath(u string) string {
	return filepath.FromSlash(strings.TrimPrefix(u, "file://"))
}

func firstOr(vs []string, def string) string {
	if len(vs) > 0 {
		return vs[0]
	}
	return def
}

func toAnySlice(vs []string) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}
