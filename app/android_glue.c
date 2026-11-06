//go:build android

#include "android_glue.h"
#include <android/native_activity.h>
#include <android/native_window.h>
#include <android/input.h>
#include <android/looper.h>
#include <android/sensor.h>
#include <android/configuration.h>
#include <android/asset_manager.h>
#include <stdio.h>
#include <pthread.h>
#include <string.h>
#include <math.h>
#include <jni.h>
#include <android/log.h>

// Implemented in Go (cgo //export). The render thread enters here.
extern void gfxAndroidMain(void);

#define RING 256
#define LOOPER_ID_INPUT 1
#define LOOPER_ID_SENSOR 2

static pthread_mutex_t g_mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t  g_cond = PTHREAD_COND_INITIALIZER;

static ANativeActivity *g_activity;
static ANativeWindow *g_window;
static int g_width, g_height;
static int g_destroyed;

static gfx_event g_ring[RING];
static int g_head, g_tail;

static ALooper *g_looper;
static AInputQueue *g_queue;        // attached to the render looper
static AInputQueue *g_pendingQueue; // handed off from the main thread

static void push_id(int kind, int id, float x, float y);

// Accelerometer: the latest sample, read by gfx_accel. Enabled on the
// render thread's looper (the same one that pumps touch input).
static ASensorEventQueue *g_sensorQueue;
static float g_accel[3];
static int g_accelSeq;
static float g_rot[4]; // game rotation vector as a quaternion x, y, z, w
static int g_rotSeq;

static void sensors_start(void) {
    ASensorManager *mgr = ASensorManager_getInstanceForPackage("");
    if (!mgr) return;
    const ASensor *acc = ASensorManager_getDefaultSensor(mgr, ASENSOR_TYPE_ACCELEROMETER);
    if (!acc) return;
    g_sensorQueue = ASensorManager_createEventQueue(mgr, g_looper, LOOPER_ID_SENSOR, NULL, NULL);
    if (!g_sensorQueue) return;
    ASensorEventQueue_enableSensor(g_sensorQueue, acc);
    ASensorEventQueue_setEventRate(g_sensorQueue, acc, 1000000 / 60);
    // Orientation without the magnetometer (no heading drift from nearby
    // metal, no absolute north — a game only needs relative turns).
    const ASensor *rot = ASensorManager_getDefaultSensor(mgr, ASENSOR_TYPE_GAME_ROTATION_VECTOR);
    if (!rot) rot = ASensorManager_getDefaultSensor(mgr, ASENSOR_TYPE_ROTATION_VECTOR);
    if (rot) {
        ASensorEventQueue_enableSensor(g_sensorQueue, rot);
        ASensorEventQueue_setEventRate(g_sensorQueue, rot, 1000000 / 90);
    }
}

static void sensors_drain(void) {
    if (!g_sensorQueue) return;
    ASensorEvent ev;
    while (ASensorEventQueue_getEvents(g_sensorQueue, &ev, 1) > 0) {
        if (ev.type == ASENSOR_TYPE_ACCELEROMETER) {
            g_accel[0] = ev.acceleration.x;
            g_accel[1] = ev.acceleration.y;
            g_accel[2] = ev.acceleration.z;
            g_accelSeq++;
        } else if (ev.type == ASENSOR_TYPE_GAME_ROTATION_VECTOR || ev.type == ASENSOR_TYPE_ROTATION_VECTOR) {
            // data[0..2] = x, y, z of the rotation vector (the quaternion's
            // vector part); data[3] is w where the sensor provides it.
            float x = ev.data[0], y = ev.data[1], z = ev.data[2];
            float w = ev.data[3];
            float n = x * x + y * y + z * z;
            if (!(w > -1.0f && w < 1.0f) || n > 1.0f) w = n < 1.0f ? sqrtf(1.0f - n) : 0.0f;
            g_rot[0] = x; g_rot[1] = y; g_rot[2] = z; g_rot[3] = w;
            g_rotSeq++;
        }
    }
}

int gfx_rotation(float *xyzw) {
    pthread_mutex_lock(&g_mu);
    xyzw[0] = g_rot[0]; xyzw[1] = g_rot[1]; xyzw[2] = g_rot[2]; xyzw[3] = g_rot[3];
    int seq = g_rotSeq;
    pthread_mutex_unlock(&g_mu);
    return seq;
}

int gfx_accel(float *xyz) {
    pthread_mutex_lock(&g_mu);
    xyz[0] = g_accel[0]; xyz[1] = g_accel[1]; xyz[2] = g_accel[2];
    int seq = g_accelSeq;
    pthread_mutex_unlock(&g_mu);
    return seq;
}

static void push(int kind, float x, float y) { push_id(kind, 0, x, y); }

static void push_id(int kind, int id, float x, float y) {
    int next = (g_tail + 1) % RING;
    if (next == g_head) return; // full: drop
    g_ring[g_tail].kind = kind;
    g_ring[g_tail].id = id;
    g_ring[g_tail].x = x;
    g_ring[g_tail].y = y;
    g_tail = next;
}

// ---- activity callbacks (Android main thread) ----

static void onWindowCreated(ANativeActivity *a, ANativeWindow *w) {
    __android_log_print(ANDROID_LOG_INFO, "gfx", "activity: window created %p %dx%d", w, ANativeWindow_getWidth(w), ANativeWindow_getHeight(w));
    pthread_mutex_lock(&g_mu);
    g_window = w;
    g_width = ANativeWindow_getWidth(w);
    g_height = ANativeWindow_getHeight(w);
    push(GFX_EV_WINDOW_CREATED, 0, 0);
    pthread_cond_broadcast(&g_cond);
    pthread_mutex_unlock(&g_mu);
}

static void onWindowResized(ANativeActivity *a, ANativeWindow *w) {
    pthread_mutex_lock(&g_mu);
    g_width = ANativeWindow_getWidth(w);
    g_height = ANativeWindow_getHeight(w);
    push(GFX_EV_WINDOW_RESIZED, (float)g_width, (float)g_height);
    pthread_mutex_unlock(&g_mu);
}

static void onWindowDestroyed(ANativeActivity *a, ANativeWindow *w) {
    __android_log_print(ANDROID_LOG_INFO, "gfx", "activity: window destroyed %p", w);
    pthread_mutex_lock(&g_mu);
    g_window = NULL;
    push(GFX_EV_WINDOW_DESTROYED, 0, 0);
    pthread_cond_broadcast(&g_cond);
    pthread_mutex_unlock(&g_mu);
}

static void onDestroy(ANativeActivity *a) {
    __android_log_write(ANDROID_LOG_INFO, "gfx", "activity: destroy");
    pthread_mutex_lock(&g_mu);
    g_destroyed = 1;
    push(GFX_EV_DESTROY, 0, 0);
    pthread_cond_broadcast(&g_cond);
    pthread_mutex_unlock(&g_mu);
}

static void onInputQueueCreated(ANativeActivity *a, AInputQueue *q) {
    pthread_mutex_lock(&g_mu);
    g_pendingQueue = q;
    pthread_mutex_unlock(&g_mu);
}

static void onInputQueueDestroyed(ANativeActivity *a, AInputQueue *q) {
    pthread_mutex_lock(&g_mu);
    if (g_queue == q && g_looper) AInputQueue_detachLooper(q);
    if (g_queue == q) g_queue = NULL;
    if (g_pendingQueue == q) g_pendingQueue = NULL;
    pthread_mutex_unlock(&g_mu);
}

static void *render_thread(void *arg) {
    g_looper = ALooper_prepare(ALOOPER_PREPARE_ALLOW_NON_CALLBACKS);
    sensors_start();
    gfxAndroidMain();
    return NULL;
}

static void onPause(ANativeActivity *a) { __android_log_write(ANDROID_LOG_INFO, "gfx", "activity: pause"); }
static void onResume(ANativeActivity *a) { __android_log_write(ANDROID_LOG_INFO, "gfx", "activity: resume"); }
static void onStop(ANativeActivity *a) { __android_log_write(ANDROID_LOG_INFO, "gfx", "activity: stop"); }

void ANativeActivity_onCreate(ANativeActivity *activity, void *saved, size_t savedSize) {
    __android_log_write(ANDROID_LOG_INFO, "gfx", "activity: create");
    g_activity = activity;
    activity->callbacks->onPause = onPause;
    activity->callbacks->onResume = onResume;
    activity->callbacks->onStop = onStop;
    activity->callbacks->onNativeWindowCreated = onWindowCreated;
    activity->callbacks->onNativeWindowResized = onWindowResized;
    activity->callbacks->onNativeWindowDestroyed = onWindowDestroyed;
    activity->callbacks->onInputQueueCreated = onInputQueueCreated;
    activity->callbacks->onInputQueueDestroyed = onInputQueueDestroyed;
    activity->callbacks->onDestroy = onDestroy;

    pthread_t t;
    pthread_create(&t, NULL, render_thread, NULL);
    pthread_detach(t);
}

// ---- render-thread API (called from Go) ----

void *gfx_window(void) {
    pthread_mutex_lock(&g_mu);
    void *w = g_window;
    pthread_mutex_unlock(&g_mu);
    return w;
}

void gfx_window_size(int *w, int *h) {
    pthread_mutex_lock(&g_mu);
    *w = g_width;
    *h = g_height;
    pthread_mutex_unlock(&g_mu);
}

int gfx_wait_window(void) {
    pthread_mutex_lock(&g_mu);
    while (g_window == NULL && !g_destroyed) {
        pthread_cond_wait(&g_cond, &g_mu);
    }
    int ok = g_window != NULL;
    pthread_mutex_unlock(&g_mu);
    return ok;
}

// drainInput pulls motion events from the attached queue into the ring.
static void drainInput(void) {
    if (!g_queue) return;
    AInputEvent *ev = NULL;
    while (AInputQueue_getEvent(g_queue, &ev) >= 0) {
        if (AInputQueue_preDispatchEvent(g_queue, ev)) continue;
        int handled = 0;
        if (AInputEvent_getType(ev) == AINPUT_EVENT_TYPE_MOTION) {
            // Full multi-touch: every pointer, with its stable id. The Go
            // side keeps mapping the primary finger onto the mouse stream.
            int raw = AMotionEvent_getAction(ev);
            int action = raw & AMOTION_EVENT_ACTION_MASK;
            size_t idx = (size_t)((raw & AMOTION_EVENT_ACTION_POINTER_INDEX_MASK)
                                  >> AMOTION_EVENT_ACTION_POINTER_INDEX_SHIFT);
            size_t i, n;
            switch (action) {
            case AMOTION_EVENT_ACTION_DOWN:
            case AMOTION_EVENT_ACTION_POINTER_DOWN:
                push_id(GFX_EV_TOUCH_DOWN, AMotionEvent_getPointerId(ev, idx),
                        AMotionEvent_getX(ev, idx), AMotionEvent_getY(ev, idx));
                break;
            case AMOTION_EVENT_ACTION_MOVE:
                n = AMotionEvent_getPointerCount(ev);
                for (i = 0; i < n; i++)
                    push_id(GFX_EV_TOUCH_MOVE, AMotionEvent_getPointerId(ev, i),
                            AMotionEvent_getX(ev, i), AMotionEvent_getY(ev, i));
                break;
            case AMOTION_EVENT_ACTION_UP:
            case AMOTION_EVENT_ACTION_POINTER_UP:
                push_id(GFX_EV_TOUCH_UP, AMotionEvent_getPointerId(ev, idx),
                        AMotionEvent_getX(ev, idx), AMotionEvent_getY(ev, idx));
                break;
            case AMOTION_EVENT_ACTION_CANCEL:
                n = AMotionEvent_getPointerCount(ev);
                for (i = 0; i < n; i++)
                    push_id(GFX_EV_TOUCH_CANCEL, AMotionEvent_getPointerId(ev, i),
                            AMotionEvent_getX(ev, i), AMotionEvent_getY(ev, i));
                break;
            }
            handled = 1;
        }
        AInputQueue_finishEvent(g_queue, ev, handled);
    }
}

void gfx_poll_event(gfx_event *out) {
    pthread_mutex_lock(&g_mu);
    // Attach a freshly-created input queue to this thread's looper.
    if (g_pendingQueue && g_pendingQueue != g_queue) {
        if (g_queue) AInputQueue_detachLooper(g_queue);
        g_queue = g_pendingQueue;
        AInputQueue_attachLooper(g_queue, g_looper, LOOPER_ID_INPUT, NULL, NULL);
    }
    pthread_mutex_unlock(&g_mu);

    // Pump the looper (non-blocking) so input becomes available, then
    // drain. ALooper_pollOnce wakes for the attached input source.
    int events;
    void *data;
    ALooper_pollOnce(0, NULL, &events, &data);
    pthread_mutex_lock(&g_mu);
    drainInput();
    sensors_drain();
    if (g_head == g_tail) {
        out->kind = GFX_EV_NONE;
    } else {
        *out = g_ring[g_head];
        g_head = (g_head + 1) % RING;
    }
    pthread_mutex_unlock(&g_mu);
}

// ---- clipboard (JNI) ----
//
// The render thread is not the JVM's main thread, so each call attaches it
// (a no-op once attached; the thread stays attached for the process's
// life, which is what every long-lived native thread does). Every JNI
// call is followed by an exception check, because a SecurityException is
// how Android 10+ refuses a background read, and an unhandled one would
// take the process down on the next JNI call.

static JNIEnv *jni_env(void) {
    if (!g_activity) return NULL;
    JavaVM *vm = g_activity->vm;
    JNIEnv *env = NULL;
    if ((*vm)->GetEnv(vm, (void **)&env, JNI_VERSION_1_6) == JNI_OK) return env;
    if ((*vm)->AttachCurrentThread(vm, &env, NULL) != JNI_OK) return NULL;
    return env;
}

static int jni_failed(JNIEnv *env) {
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionClear(env);
        return 1;
    }
    return 0;
}

// clipboard_manager returns a local ref to the activity's ClipboardManager
// (getSystemService(Context.CLIPBOARD_SERVICE)), or NULL.
static jobject clipboard_manager(JNIEnv *env) {
    jobject activity = g_activity->clazz;
    jclass ctx = (*env)->GetObjectClass(env, activity);
    jmethodID getService = (*env)->GetMethodID(env, ctx, "getSystemService",
                                               "(Ljava/lang/String;)Ljava/lang/Object;");
    if (jni_failed(env) || !getService) return NULL;
    jstring name = (*env)->NewStringUTF(env, "clipboard");
    jobject cm = (*env)->CallObjectMethod(env, activity, getService, name);
    (*env)->DeleteLocalRef(env, name);
    (*env)->DeleteLocalRef(env, ctx);
    if (jni_failed(env)) return NULL;
    return cm;
}

int gfx_clipboard_get(char *buf, int cap) {
    JNIEnv *env = jni_env();
    if (!env) return -1;
    if ((*env)->PushLocalFrame(env, 16) < 0) return -1;
    int result = -1;
    jobject cm = clipboard_manager(env);
    if (!cm) goto out;
    jclass cmClass = (*env)->GetObjectClass(env, cm);
    jmethodID getClip = (*env)->GetMethodID(env, cmClass, "getPrimaryClip", "()Landroid/content/ClipData;");
    if (jni_failed(env) || !getClip) goto out;
    jobject clip = (*env)->CallObjectMethod(env, cm, getClip);
    if (jni_failed(env) || !clip) goto out;
    jclass clipClass = (*env)->GetObjectClass(env, clip);
    jmethodID count = (*env)->GetMethodID(env, clipClass, "getItemCount", "()I");
    jmethodID itemAt = (*env)->GetMethodID(env, clipClass, "getItemAt", "(I)Landroid/content/ClipData$Item;");
    if (jni_failed(env) || !count || !itemAt) goto out;
    if ((*env)->CallIntMethod(env, clip, count) < 1 || jni_failed(env)) goto out;
    jobject item = (*env)->CallObjectMethod(env, clip, itemAt, 0);
    if (jni_failed(env) || !item) goto out;
    jclass itemClass = (*env)->GetObjectClass(env, item);
    // coerceToText renders a URI or intent clip as text too, like a paste
    // into an EditText does.
    jmethodID coerce = (*env)->GetMethodID(env, itemClass, "coerceToText",
                                           "(Landroid/content/Context;)Ljava/lang/CharSequence;");
    if (jni_failed(env) || !coerce) goto out;
    jobject cs = (*env)->CallObjectMethod(env, item, coerce, g_activity->clazz);
    if (jni_failed(env) || !cs) goto out;
    jclass objClass = (*env)->GetObjectClass(env, cs);
    jmethodID toString = (*env)->GetMethodID(env, objClass, "toString", "()Ljava/lang/String;");
    if (jni_failed(env) || !toString) goto out;
    jstring js = (jstring)(*env)->CallObjectMethod(env, cs, toString);
    if (jni_failed(env) || !js) goto out;
    const char *utf = (*env)->GetStringUTFChars(env, js, NULL);
    if (!utf) goto out;
    // Modified UTF-8 from JNI; identical to UTF-8 for everything but NUL
    // and supplementary characters, which the Go side re-decodes.
    int n = (int)strlen(utf);
    if (n < cap) {
        memcpy(buf, utf, n + 1);
    }
    (*env)->ReleaseStringUTFChars(env, js, utf);
    result = n;
out:
    (*env)->PopLocalFrame(env, NULL);
    return result;
}

void gfx_clipboard_set(const char *s) {
    JNIEnv *env = jni_env();
    if (!env) return;
    if ((*env)->PushLocalFrame(env, 16) < 0) return;
    jobject cm = clipboard_manager(env);
    if (!cm) goto out;
    jclass clipData = (*env)->FindClass(env, "android/content/ClipData");
    if (jni_failed(env) || !clipData) goto out;
    jmethodID newPlain = (*env)->GetStaticMethodID(env, clipData, "newPlainText",
        "(Ljava/lang/CharSequence;Ljava/lang/CharSequence;)Landroid/content/ClipData;");
    if (jni_failed(env) || !newPlain) goto out;
    jstring label = (*env)->NewStringUTF(env, "text");
    jstring text = (*env)->NewStringUTF(env, s);
    if (jni_failed(env) || !text) goto out;
    jobject clip = (*env)->CallStaticObjectMethod(env, clipData, newPlain, label, text);
    if (jni_failed(env) || !clip) goto out;
    jclass cmClass = (*env)->GetObjectClass(env, cm);
    jmethodID setClip = (*env)->GetMethodID(env, cmClass, "setPrimaryClip", "(Landroid/content/ClipData;)V");
    if (jni_failed(env) || !setClip) goto out;
    (*env)->CallVoidMethod(env, cm, setClip, clip);
    jni_failed(env);
out:
    (*env)->PopLocalFrame(env, NULL);
}

void gfx_log(const char *line) {
    __android_log_write(ANDROID_LOG_INFO, "gfx", line);
}

const char *gfx_data_dir(void) {
    return g_activity && g_activity->internalDataPath ? g_activity->internalDataPath : "";
}

int gfx_density(void) {
    if (!g_activity || !g_activity->assetManager) return 0;
    AConfiguration *cfg = AConfiguration_new();
    if (!cfg) return 0;
    AConfiguration_fromAssetManager(cfg, g_activity->assetManager);
    int d = AConfiguration_getDensity(cfg);
    AConfiguration_delete(cfg);
    return d;
}

long long gfx_asset_copy(const char *name, const char *dest) {
    if (!g_activity || !g_activity->assetManager) return -1;
    AAsset *a = AAssetManager_open(g_activity->assetManager, name, AASSET_MODE_STREAMING);
    if (!a) return -1;
    FILE *f = fopen(dest, "wb");
    if (!f) { AAsset_close(a); return -1; }
    char buf[1 << 16];
    long long total = 0;
    int n;
    while ((n = AAsset_read(a, buf, sizeof buf)) > 0) {
        if (fwrite(buf, 1, (size_t)n, f) != (size_t)n) { total = -1; break; }
        total += n;
    }
    fclose(f);
    AAsset_close(a);
    return total;
}
