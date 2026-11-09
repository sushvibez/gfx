package app

import (
	"errors"
	"image"
	"syscall"
	"unsafe"
)

var (
	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procCreateIconIndirect = syscall.NewLazyDLL("user32.dll").NewProc("CreateIconIndirect")
)

// bitmapInfoHeader mirrors BITMAPINFOHEADER.
type bitmapInfoHeader struct {
	size                         uint32
	width, height                int32
	planes, bitCount             uint16
	compression, sizeImage       uint32
	xPelsPerMeter, yPelsPerMeter int32
	clrUsed, clrImportant        uint32
}

// iconInfo mirrors ICONINFO.
type iconInfo struct {
	fIcon              int32
	xHotspot, yHotspot uint32
	hbmMask, hbmColor  uintptr
}

// SetIcon sets the window's icon — the title bar, the taskbar and Alt+Tab
// — from img (32-bit with alpha; 32×32 to 256×256 reads well).
func (w *Window) SetIcon(img image.Image) error {
	b := img.Bounds()
	wd, ht := b.Dx(), b.Dy()
	if wd == 0 || ht == 0 {
		return errors.New("app: empty icon")
	}
	hdr := bitmapInfoHeader{size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), width: int32(wd), height: -int32(ht), planes: 1, bitCount: 32}
	var bits unsafe.Pointer
	color, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&hdr)), 0 /*DIB_RGB_COLORS*/, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 || bits == nil {
		return errors.New("app: CreateDIBSection failed")
	}
	defer procDeleteObject.Call(color)
	px := unsafe.Slice((*byte)(bits), wd*ht*4)
	for y := 0; y < ht; y++ {
		for x := 0; x < wd; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*wd + x) * 4
			// BGRA, premultiplied (what the shell composites).
			px[i], px[i+1], px[i+2], px[i+3] = byte(bl>>8), byte(g>>8), byte(r>>8), byte(a>>8)
		}
	}
	mask, _, _ := procCreateBitmap.Call(uintptr(wd), uintptr(ht), 1, 1, 0)
	if mask == 0 {
		return errors.New("app: CreateBitmap failed")
	}
	defer procDeleteObject.Call(mask)
	info := iconInfo{fIcon: 1, hbmMask: mask, hbmColor: color}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	if icon == 0 {
		return errors.New("app: CreateIconIndirect failed")
	}
	const wmSetIcon = 0x0080
	sendMessageW(w.hwnd, wmSetIcon, 1 /*ICON_BIG*/, icon)
	sendMessageW(w.hwnd, wmSetIcon, 0 /*ICON_SMALL*/, icon)
	return nil
}
