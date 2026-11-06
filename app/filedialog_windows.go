package app

import (
	"strings"
	"syscall"
	"unsafe"
)

// openFileNameW mirrors OPENFILENAMEW (commdlg.h) field for field; Go's
// natural alignment of these types matches the C layout on amd64 and
// arm64.
type openFileNameW struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

const (
	ofnPathMustExist = 0x00000800
	ofnFileMustExist = 0x00001000
	ofnNoChangeDir   = 0x00000008
	ofnExplorer      = 0x00080000
)

var (
	comdlg32         = syscall.NewLazyDLL("comdlg32.dll")
	getOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
)

// OpenFile shows the standard Windows open dialog, modal to the window,
// and returns the chosen path. ok is false when the user cancelled.
func (w *Window) OpenFile(o FileDialogOptions) (path string, ok bool, err error) {
	// Filter: "Label\0*.a;*.b\0Label\0...\0\0".
	var filter []uint16
	for _, f := range o.Filters {
		filter = append(filter, utf16z(f.Label)...)
		filter = append(filter, utf16z(strings.Join(f.Patterns, ";"))...)
	}
	if len(filter) == 0 {
		filter = append(filter, utf16z("All files")...)
		filter = append(filter, utf16z("*.*")...)
	}
	filter = append(filter, 0)
	buf := make([]uint16, 32768)
	ofn := openFileNameW{
		hwndOwner:   w.hwnd,
		lpstrFilter: &filter[0],
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		flags:       ofnPathMustExist | ofnFileMustExist | ofnNoChangeDir | ofnExplorer,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	var title, dir []uint16
	if o.Title != "" {
		title = utf16z(o.Title)
		ofn.lpstrTitle = &title[0]
	}
	if o.Dir != "" {
		dir = utf16z(o.Dir)
		ofn.lpstrInitialDir = &dir[0]
	}
	r, _, _ := getOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", false, nil
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(buf[:n]), true, nil
}

func utf16z(s string) []uint16 {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		u, _ = syscall.UTF16FromString(strings.Map(func(r rune) rune {
			if r == 0 {
				return -1
			}
			return r
		}, s))
	}
	return u
}
