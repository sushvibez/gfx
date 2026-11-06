package app

import (
	"os"
	"strconv"
)

// scaleOverride returns the DPI scale forced by the GFX_SCALE environment
// variable (0 when unset/invalid). It lets HiDPI rendering be exercised on a
// 1x monitor and pins a scale when the platform's detection misbehaves.
// Backends consult it first in Scale()/window creation.
func scaleOverride() float32 {
	v := os.Getenv("GFX_SCALE")
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 32)
	if err != nil || f <= 0 || f > 8 {
		return 0
	}
	return float32(f)
}
