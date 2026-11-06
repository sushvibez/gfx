//go:build !windows

package app

// OpenFile has no native dialog on this platform; it returns
// ErrNoFileDialog so the app can show ui.FileBrowser instead.
func (w *Window) OpenFile(o FileDialogOptions) (path string, ok bool, err error) {
	return "", false, ErrNoFileDialog
}
