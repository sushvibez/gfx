package app

import "syscall/js"

// OpenURL opens url in a new browser tab.
func OpenURL(url string) error {
	js.Global().Call("open", url, "_blank")
	return nil
}
