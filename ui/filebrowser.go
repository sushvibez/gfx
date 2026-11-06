package ui

// FileBrowser is the directory-listing panel behind a file dialog: a title,
// the current path, a scrollable list of entries (parent first, then folders,
// then files), and Cancel/confirm buttons. It is a plain widget — any Go app
// can put one in a Modal — and the gml engine's FileDialog element renders
// it with the QML contract layered on top.
//
// State lives with the caller: the browser reads Dir/Selected and reports
// navigation and picks through callbacks, so the owner decides how to store
// them (a Go struct field, or a gml property that invalidates a build).

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileBrowser describes one panel. The zero value is not useful: set Dir.
type FileBrowser struct {
	Title string // heading ("Open", "Save File As")
	Dir   string // directory being listed
	// Filters are QML-style name filters ("PDF files (*.pdf)", "All files
	// (*)"); empty means everything.
	Filters []string
	// Selected holds the currently picked absolute paths.
	Selected []string
	// FolderOnly lists (and picks) directories only; Multi keeps previous
	// picks when another is added.
	FolderOnly bool
	Multi      bool
	// AcceptLabel overrides the confirm button's text ("Open" by default,
	// or "Save"/"Choose" as the caller prefers).
	AcceptLabel string
	// Err is shown under the path when the directory could not be read.
	Err string

	// OnNavigate is called with the directory to move to; OnPick with a
	// path the user chose; OnAccept and OnCancel with the buttons.
	OnNavigate func(dir string)
	OnPick     func(path string)
	OnAccept   func()
	OnCancel   func()
}

// Entries lists the directory the browser points at, applying the folder and
// name filters. Returned separately from Panel so a caller can pre-read or
// cache the listing; Panel does it itself when needed.
func (b FileBrowser) Entries() (names []string, dirs []bool, err error) {
	des, err := os.ReadDir(b.Dir)
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(des, func(i, j int) bool {
		if des[i].IsDir() != des[j].IsDir() {
			return des[i].IsDir() // directories first, like every file browser
		}
		return des[i].Name() < des[j].Name()
	})
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue // hidden entries, as the platform dialogs default to
		}
		if b.FolderOnly && !de.IsDir() {
			continue
		}
		if !de.IsDir() && !MatchesNameFilters(name, b.Filters) {
			continue
		}
		names = append(names, name)
		dirs = append(dirs, de.IsDir())
		if len(names) >= 500 { // a huge directory: keep the panel bounded
			break
		}
	}
	return names, dirs, nil
}

// Panel builds the dialog's content element. Put it in a Modal to float it.
func (b FileBrowser) Panel() Element {
	names, dirs, err := b.Entries()
	errText := b.Err
	if err != nil {
		errText = err.Error()
	}

	list := Div().Col().Gap(1)
	if parent := filepath.Dir(b.Dir); parent != b.Dir {
		list.Children(b.row("..", true, false, func() {
			if b.OnNavigate != nil {
				b.OnNavigate(parent)
			}
		}))
	}
	picked := map[string]bool{}
	for _, p := range b.Selected {
		picked[p] = true
	}
	for i, name := range names {
		path := filepath.Join(b.Dir, name)
		isDir := dirs[i]
		list.Children(b.row(name, isDir, picked[path], func() {
			if isDir && !b.FolderOnly {
				if b.OnNavigate != nil {
					b.OnNavigate(path)
				}
				return
			}
			if b.OnPick != nil {
				b.OnPick(path)
			}
		}))
	}
	if len(names) == 0 {
		list.Children(Div().P(10).Children(Text("Nothing to show here", RGB(0x868e96))))
	}

	accept := b.AcceptLabel
	if accept == "" {
		accept = "Open"
	}
	head := Div().Col().Gap(4).Children(
		Text(b.Title, RGB(0xe8e8f0)),
		Text(b.Dir, RGB(0x868e96)))
	if errText != "" {
		head.Children(Text(errText, RGB(0xffb4b4)))
	}
	return Div().Col().Gap(10).P(16).W(520).
		Bg(RGB(0x1f2128)).Rounded(10).Border(1, RGB(0x3a3d45)).
		Children(
			head,
			Scroll(&ScrollState{}).H(260).Children(list),
			Div().Row().Gap(8).Justify(AlignEnd).Children(
				dialogButton("Cancel", 0x495057, b.OnCancel),
				dialogButton(accept, 0x3b5bdb, b.OnAccept)))
}

// row is one entry line: folders are tinted and open on click, files pick.
func (b FileBrowser) row(name string, isDir, picked bool, onClick func()) Element {
	color := RGB(0xe8e8f0)
	if isDir {
		color = RGB(0x74c0fc)
	}
	row := Div().Row().ItemsCenter().P(6).Rounded(4).Children(Text(name, color))
	if picked {
		row.Bg(RGB(0x2b3a55))
	}
	row.OnClick(onClick)
	return row
}

func dialogButton(label string, bg uint32, onClick func()) Element {
	b := Div().PadXY(14, 8).Rounded(6).Bg(RGB(bg)).Children(Text(label, RGB(0xffffff)))
	if onClick != nil {
		b.OnClick(onClick)
	}
	return b
}

// MatchesNameFilters reports whether a file name passes any QML-style name
// filter — "PDF files (*.pdf)", "Archives (*.zip *.gz)", "All files (*)".
// No filters means everything matches.
func MatchesNameFilters(name string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		globs := f
		if open := strings.IndexByte(f, '('); open >= 0 {
			globs = strings.Trim(f[open+1:], ")")
		}
		for _, g := range strings.Fields(globs) {
			if g == "*" || g == "*.*" {
				return true
			}
			if ok, _ := filepath.Match(g, name); ok {
				return true
			}
		}
	}
	return false
}
