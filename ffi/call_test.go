//go:build !js && !windows

package ffi

import (
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

func TestCall(t *testing.T) {
	lib := openLibc(t)
	labs, _ := lib.Sym("labs")
	if got := int64(Call(labs, ^uintptr(41), 0, 0, 0, 0, 0)); got != 42 { // labs(-42)
		t.Fatalf("labs(-42) = %d", got)
	}
	strlen, _ := lib.Sym("strlen")
	s := []byte("hello\x00")
	if got := Call(strlen, uintptr(unsafe.Pointer(&s[0])), 0, 0, 0, 0, 0); got != 5 {
		t.Fatalf("strlen = %d", got)
	}
	// All six registers: snprintf(buf, n, "%d %d %d %d", a, b, c, d) uses six.
	snprintf, _ := lib.Sym("snprintf")
	buf := make([]byte, 64)
	fmtS := []byte("%ld %ld %ld %ld\x00")
	n := Call(snprintf, uintptr(unsafe.Pointer(&buf[0])), 64, uintptr(unsafe.Pointer(&fmtS[0])), 1, 2, 3)
	_ = n // only 3 varargs fit in registers here; the 4th comes from the stack (garbage) so just check the prefix
	if string(buf[:5]) != "1 2 3" {
		t.Fatalf("snprintf = %q", buf[:n])
	}
}

func BenchmarkCall(b *testing.B) {
	lib := openLibc(b)
	labs, _ := lib.Sym("labs")
	var viaReg func(int64) int64
	lib.Bind(&viaReg, "labs")
	b.Run("RegisterFunc", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			viaReg(-7)
		}
	})
	b.Run("SyscallN", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			purego.SyscallN(labs, ^uintptr(6))
		}
	})
	b.Run("Call", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			Call(labs, ^uintptr(6), 0, 0, 0, 0, 0)
		}
	})
}

// CallStack carries the seventh argument onwards (stack on amd64, X6/X7
// then stack on arm64): snprintf(buf, n, fmt, a, b, c, d, e) names eight.
func TestCallStack(t *testing.T) {
	lib := openLibc(t)
	snprintf, _ := lib.Sym("snprintf")
	buf := make([]byte, 64)
	f := []byte("%ld %ld %ld %ld %ld\x00")
	n := CallStack(snprintf, uintptr(unsafe.Pointer(&buf[0])), 64, uintptr(unsafe.Pointer(&f[0])), 1, 2, 3, []uintptr{4, 5})
	if got := string(buf[:n]); got != "1 2 3 4 5" {
		t.Fatalf("snprintf = %q", got)
	}
	// Still fine through Call (stack words zero).
	n = Call(snprintf, uintptr(unsafe.Pointer(&buf[0])), 64, uintptr(unsafe.Pointer(&f[0])), 7, 8, 9)
	if got := string(buf[:n]); got != "7 8 9 0 0" {
		t.Fatalf("snprintf via Call = %q", got)
	}
}

// CallF puts float arguments in the vector registers: snprintf with %f
// and %g is a mixed int/float signature (the string pointer and size in
// integer registers, the doubles in XMM/D registers).
func TestCallF(t *testing.T) {
	lib := openLibc(t)
	snprintf, _ := lib.Sym("snprintf")
	buf := make([]byte, 64)
	f := []byte("%.1f %.2f %ld\x00")
	n := CallF(snprintf, uintptr(unsafe.Pointer(&buf[0])), 64, uintptr(unsafe.Pointer(&f[0])), 42, 0, 0, []uint64{F64(1.5), F64(2.25)})
	if got := string(buf[:n]); got != "1.5 2.25 42" {
		t.Fatalf("snprintf = %q", got)
	}
	// Single-precision arguments reach the callee as singles: %f of a
	// float promoted by the callee is not available, so go through
	// strtof's reverse — snprintf cannot take floats; use a float32 pair
	// with libm's powf, whose result lands in XMM0/S0 (not read back by
	// CallF), and check the call survives with the registers it was given.
	libm, err := Open("libm.so.6", "libm.so")
	if err != nil {
		t.Skip(err)
	}
	powf, err := libm.Sym("powf")
	if err != nil {
		t.Skip(err)
	}
	CallF(powf, 0, 0, 0, 0, 0, 0, []uint64{F32(2), F32(10)})
}
