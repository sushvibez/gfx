package app

import "errors"

// FileFilter is one entry of a file dialog's type list: a label and the
// glob patterns it admits ("Models", []string{"*.obj", "*.stl"}).
type FileFilter struct {
	Label    string
	Patterns []string
}

// FileDialogOptions configure OpenFile.
type FileDialogOptions struct {
	Title   string
	Dir     string // initial directory ("" = the system's choice)
	Filters []FileFilter
}

// ErrNoFileDialog is returned where the platform has no native file
// dialog (X11, web, Android); apps fall back to ui.FileBrowser.
var ErrNoFileDialog = errors.New("app: no native file dialog on this platform")
