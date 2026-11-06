// NativeActivity glue for the Android backend (DESIGN.md §8). Minimal
// gomobile-style native-app loop: ANativeActivity_onCreate wires the
// activity callbacks and spawns a render thread that calls into Go.
#ifndef GFX_ANDROID_GLUE_H
#define GFX_ANDROID_GLUE_H
#include <stdint.h>

// Event kinds pushed from the Android main thread to the render thread.
enum {
    GFX_EV_NONE = 0,
    GFX_EV_WINDOW_CREATED,
    GFX_EV_WINDOW_RESIZED,
    GFX_EV_WINDOW_DESTROYED,
    GFX_EV_DESTROY,
    GFX_EV_TOUCH_DOWN,
    GFX_EV_TOUCH_MOVE,
    GFX_EV_TOUCH_UP,
    GFX_EV_TOUCH_CANCEL,
};

// gfx_event is one dequeued event for Go to act on.
typedef struct {
    int32_t kind;
    int32_t id; // touch pointer id (stable per finger); 0 for window events
    float x, y;
} gfx_event;

// gfx_window returns the current ANativeWindow* (0 if none).
void *gfx_window(void);
// gfx_window_size writes the current surface size.
void gfx_window_size(int *w, int *h);
// gfx_wait_window blocks until a window exists or the activity is
// destroyed; returns 1 if a window is ready, 0 on destroy.
int gfx_wait_window(void);
// gfx_poll_event drains one queued event into *ev (kind GFX_EV_NONE when
// empty). Also pumps the input looper.
void gfx_poll_event(gfx_event *ev);

// gfx_clipboard_get copies the primary clip's text into buf (UTF-8, NUL-
// terminated when it fits) and returns its full length, or -1 when there is
// no text clip. A result >= cap means buf was too small: call again with a
// bigger one.
int gfx_clipboard_get(char *buf, int cap);
// gfx_clipboard_set makes s (UTF-8) the primary clip.
void gfx_clipboard_set(const char *s);

// gfx_accel writes the latest accelerometer sample (m/s², device axes)
// and returns its sequence number: 0 until the first sample arrives.
int gfx_accel(float *xyz);
// gfx_data_dir returns the activity's internal data path (writable,
// private to the app) or an empty string before the activity exists.
const char *gfx_data_dir(void);
// gfx_density returns the display density in dpi (160 = mdpi), 0 if
// unknown.
int gfx_density(void);
// gfx_rotation writes the latest game-rotation-vector quaternion (x, y, z,
// w; device axes) and returns its sequence number: 0 until one arrives.
int gfx_rotation(float *xyzw);
// gfx_asset_copy copies the APK asset name to the file dest; returns
// the bytes written, or -1 when the asset is missing or the copy failed.
long long gfx_asset_copy(const char *name, const char *dest);
// gfx_log writes one line to the Android log under the "gfx" tag.
void gfx_log(const char *line);

#endif
