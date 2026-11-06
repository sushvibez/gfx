//go:build !js

package ffi

import (
	"os"
	"runtime"
	"testing"
)

func openLibc(t testing.TB) *Lib {
	t.Helper()
	var names []string
	switch runtime.GOOS {
	case "linux", "android":
		names = []string{"libc.so.6", "libc.so"}
	case "darwin":
		names = []string{"/usr/lib/libSystem.B.dylib"}
	case "windows":
		names = []string{"kernel32.dll"}
	default:
		t.Skipf("no libc spelling for %s", runtime.GOOS)
	}
	lib, err := Open(names...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return lib
}

func TestOpenMissingLibrary(t *testing.T) {
	if _, err := Open("libdefinitely-not-a-real-library.so.999"); err == nil {
		t.Fatal("expected error for missing library")
	}
}

func TestBindAndCall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("getpid is unix")
	}
	lib := openLibc(t)
	var getpid func() int32
	lib.Bind(&getpid, "getpid")
	if got := getpid(); got != int32(os.Getpid()) {
		t.Fatalf("getpid() = %d, want %d", got, os.Getpid())
	}
}

func TestBindStringArg(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("strlen via libc is unix here")
	}
	lib := openLibc(t)
	var strlen func(string) int
	lib.Bind(&strlen, "strlen")
	if got := strlen("hello"); got != 5 {
		t.Fatalf("strlen = %d, want 5", got)
	}
}

func TestSymMissing(t *testing.T) {
	lib := openLibc(t)
	if _, err := lib.Sym("definitely_not_a_symbol_xyz"); err == nil {
		t.Fatal("expected error for missing symbol")
	}
}
