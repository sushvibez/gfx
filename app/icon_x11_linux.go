//go:build linux && !android

package app

import (
	"errors"
	"image"
	"image/color"
	"unsafe"
)

// SetIcon sets the window's icon (_NET_WM_ICON: the taskbar and the
// window switcher read it).
func (w *Window) SetIcon(img image.Image) error {
	b := img.Bounds()
	wd, ht := b.Dx(), b.Dy()
	if wd == 0 || ht == 0 {
		return errors.New("app: empty icon")
	}
	// Format-32 property data is C longs (8 bytes here): width, height,
	// then one unpremultiplied ARGB pixel per long.
	data := make([]uint64, 2+wd*ht)
	data[0], data[1] = uint64(wd), uint64(ht)
	for y := 0; y < ht; y++ {
		for x := 0; x < wd; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			data[2+y*wd+x] = uint64(c.A)<<24 | uint64(c.R)<<16 | uint64(c.G)<<8 | uint64(c.B)
		}
	}
	prop := xInternAtom(w.display, "_NET_WM_ICON", 0)
	const xaCardinal = 6
	xChangeProperty(w.display, w.win, prop, xaCardinal, 32, 0 /*PropModeReplace*/, uintptr(unsafe.Pointer(&data[0])), int32(len(data)))
	xFlush(w.display)
	return nil
}
