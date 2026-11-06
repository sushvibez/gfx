//go:build android

package audio

// Android playback through AAudio (API 26+; the APK's minSdk is 29). The
// Android backend is the one place the engine uses cgo anyway (the
// NativeActivity glue), so the stream is driven directly. AAudioStream_write
// blocks until the frames are queued, which paces the mixer like every
// other backend.

/*
#cgo LDFLAGS: -laaudio
#include <aaudio/AAudio.h>
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

type aaudioBackend struct {
	stream *C.AAudioStream
}

func openBackend() (backend, error) {
	var builder *C.AAudioStreamBuilder
	if rc := C.AAudio_createStreamBuilder(&builder); rc != C.AAUDIO_OK {
		return nil, fmt.Errorf("audio: AAudio_createStreamBuilder: %s", C.GoString(C.AAudio_convertResultToText(rc)))
	}
	defer C.AAudioStreamBuilder_delete(builder)
	C.AAudioStreamBuilder_setDirection(builder, C.AAUDIO_DIRECTION_OUTPUT)
	C.AAudioStreamBuilder_setFormat(builder, C.AAUDIO_FORMAT_PCM_I16)
	C.AAudioStreamBuilder_setChannelCount(builder, 2)
	C.AAudioStreamBuilder_setSampleRate(builder, SampleRate)
	C.AAudioStreamBuilder_setPerformanceMode(builder, C.AAUDIO_PERFORMANCE_MODE_LOW_LATENCY)
	C.AAudioStreamBuilder_setSharingMode(builder, C.AAUDIO_SHARING_MODE_SHARED)
	var stream *C.AAudioStream
	if rc := C.AAudioStreamBuilder_openStream(builder, &stream); rc != C.AAUDIO_OK {
		return nil, fmt.Errorf("audio: AAudioStreamBuilder_openStream: %s", C.GoString(C.AAudio_convertResultToText(rc)))
	}
	if int(C.AAudioStream_getSampleRate(stream)) != SampleRate {
		// The device refused our rate; AAudio would resample for a
		// callback stream but not for blocking writes. Rare on API 29+.
		C.AAudioStream_close(stream)
		return nil, fmt.Errorf("audio: device rate %d, want %d", int(C.AAudioStream_getSampleRate(stream)), SampleRate)
	}
	if rc := C.AAudioStream_requestStart(stream); rc != C.AAUDIO_OK {
		C.AAudioStream_close(stream)
		return nil, fmt.Errorf("audio: AAudioStream_requestStart: %s", C.GoString(C.AAudio_convertResultToText(rc)))
	}
	return &aaudioBackend{stream: stream}, nil
}

func (a *aaudioBackend) write(buf []int16) error {
	frames := len(buf) / 2
	for frames > 0 {
		n := C.AAudioStream_write(a.stream, unsafe.Pointer(&buf[0]), C.int32_t(frames), C.int64_t(time.Second))
		if n < 0 {
			if n == C.AAUDIO_ERROR_DISCONNECTED {
				return fmt.Errorf("audio: AAudio stream disconnected")
			}
			return fmt.Errorf("audio: AAudioStream_write: %s", C.GoString(C.AAudio_convertResultToText(C.aaudio_result_t(n))))
		}
		frames -= int(n)
		if frames > 0 {
			buf = buf[int(n)*2:]
		}
	}
	return nil
}

func (a *aaudioBackend) close() {
	if a.stream != nil {
		C.AAudioStream_requestStop(a.stream)
		C.AAudioStream_close(a.stream)
		a.stream = nil
	}
}
