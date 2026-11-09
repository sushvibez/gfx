package app

import (
	"syscall"
	"unsafe"
)

var procShellExecuteW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

// OpenURL opens url in the system's browser.
func OpenURL(url string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	u, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	// ShellExecuteW returns a value above 32 on success.
	if r, _, e := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(u)), 0, 0, 1 /*SW_SHOWNORMAL*/); r <= 32 {
		return e
	}
	return nil
}
