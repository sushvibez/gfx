//go:build android

package app

/*
#include <stdlib.h>
#include "android_glue.h"
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

// Asset returns a file path for an asset packed into the APK (the
// build-apk.sh GFX_APK_ASSETS directory): the copy in the app's data
// directory, made on first use — SQLite and the model loaders want a
// real file, and an APK's assets are compressed entries in a zip. A
// copy already there (same size as the asset) is reused.
func Asset(name string) (string, error) {
	dir := DataDir()
	if dir == "" {
		return "", fmt.Errorf("app: no data directory for asset %s", name)
	}
	dest := filepath.Join(dir, filepath.FromSlash(name))
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return dest, nil
	}
	os.MkdirAll(filepath.Dir(dest), 0o755)
	cn, cd := C.CString(name), C.CString(dest)
	defer C.free(unsafe.Pointer(cn))
	defer C.free(unsafe.Pointer(cd))
	if n := C.gfx_asset_copy(cn, cd); n < 0 {
		os.Remove(dest)
		return "", fmt.Errorf("app: asset %s not in the APK", name)
	}
	return dest, nil
}
