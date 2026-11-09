//go:build js || android

package app

import "image"

// SetIcon has no window icon to set on this platform (the web page's
// favicon is the page's own).
func (w *Window) SetIcon(image.Image) error { return nil }
