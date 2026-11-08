package gml

// QtMultimedia's object model: MediaPlayer with its AudioOutput, VideoOutput,
// and the capture side (CaptureSession / Camera / ImageCapture /
// MediaDevices).
//
// There is NO decoder here — this framework has no cgo, so no ffmpeg — and
// nothing fetches media over the network. So the player is honest about it:
// assigning a source moves it to InvalidMedia, playbackState stays
// StoppedState, and position/duration stay 0. Pages built on it still work
// as pages: the transport controls, sliders, time labels and status text all
// bind to a real state machine and report what it really is (Qt without the
// multimedia plugin behaves the same way). VideoOutput lays out and paints
// its black frame; the capture side reports no devices, so a camera page
// shows its "no camera found" branch.
//
// Local WAV playback exists in the audio package; wiring MediaPlayer to it
// (and adding ogg/mp3 decoding) is the step that would make these real.

import (
	"time"

	"github.com/sushvibez/gfx/ui"
)

// mediaState is one MediaPlayer's retained state. Every field is an engine
// property rather than a plain value: a seek slider assigning `position` and
// a transport button calling play() must invalidate the frame like any other
// property write, and the bindings that read them must be tracked.
type mediaState struct {
	source   Ref
	status   Ref // QMediaPlayer::MediaStatus
	playback Ref // QMediaPlayer::PlaybackState
	position Ref // ms
	duration Ref // ms
	volume   Ref
	muted    Ref
}

// mediaFor returns the retained state for a MediaPlayer element.
func (doc *docState) mediaFor(key string) *mediaState {
	if doc.media == nil {
		doc.media = map[string]*mediaState{}
	}
	if m, ok := doc.media[key]; ok {
		return m
	}
	m := &mediaState{
		source:   doc.strProp(key+".source", ""),
		status:   doc.floatProp(key+".mediaStatus", float32(mediaNoMedia)),
		playback: doc.floatProp(key+".playbackState", float32(mediaStoppedState)),
		position: doc.floatProp(key+".position", 0),
		duration: doc.floatProp(key+".duration", 0),
		volume:   doc.floatProp(key+".volume", 1),
		muted:    doc.boolProp(key+".muted", false),
	}
	doc.media[key] = m
	return m
}

// num reads one of the state's numeric properties.
func mediaNum(r Ref) float64 {
	f, _ := toFloat64(r.get())
	return f
}

// Qt's QMediaPlayer enums, by their QML names (Qt 6 spells the media
// statuses both ways in different docs, so both are registered).
const (
	mediaStoppedState = float64(0)
	mediaPlayingState = float64(1)
	mediaPausedState  = float64(2)

	mediaNoMedia      = float64(0)
	mediaLoadingMedia = float64(1)
	mediaLoadedMedia  = float64(2)
	mediaStalledMedia = float64(3)
	mediaBuffering    = float64(4)
	mediaBuffered     = float64(5)
	mediaEndOfMedia   = float64(6)
	mediaInvalidMedia = float64(7)
)

// mediaPlayerNS is the MediaPlayer enum namespace markup reads
// (MediaPlayer.PlayingState, MediaPlayer.InvalidMedia, …).
func mediaPlayerNS() map[string]any {
	return map[string]any{
		"StoppedState": mediaStoppedState,
		"PlayingState": mediaPlayingState,
		"PausedState":  mediaPausedState,
		"NoMedia":      mediaNoMedia,
		"LoadingMedia": mediaLoadingMedia, "Loading": mediaLoadingMedia,
		"LoadedMedia": mediaLoadedMedia, "Loaded": mediaLoadedMedia,
		"StalledMedia": mediaStalledMedia, "Stalled": mediaStalledMedia,
		"BufferingMedia": mediaBuffering, "Buffering": mediaBuffering,
		"BufferedMedia": mediaBuffered, "Buffered": mediaBuffered,
		"EndOfMedia":   mediaEndOfMedia,
		"InvalidMedia": mediaInvalidMedia, "Invalid": mediaInvalidMedia,
		"NoError": float64(0), "ResourceError": float64(1),
		"FormatError": float64(2), "NetworkError": float64(3),
		"AccessDeniedError": float64(4),
		"Infinite":          float64(-1), "Once": float64(1),
		// QMediaPlayer::Loops
		"LoopCount": float64(1),
	}
}

// refreshMediaPlayer services a MediaPlayer declaration: it keeps the
// element's state current and exposes the QML contract on its id.
func (doc *docState) refreshMediaPlayer(n *node, env *Env) {
	key := doc.key(n)
	m := doc.mediaFor(key)
	for _, a := range n.attrs {
		switch a.name {
		case "source":
			if v, err := a.expr.eval(env); err == nil {
				_ = setMediaSource(m, str(v))
			}
		case "audioOutput":
			if a.elem != nil {
				for _, ao := range a.elem.attrs {
					switch ao.name {
					case "volume":
						if f, err := attrFloat(ao, env); err == nil {
							_ = m.volume.set(f)
						}
					case "muted":
						if b, err := attrBool(ao, env); err == nil {
							_ = m.muted.set(b)
						}
					}
				}
			}
		}
	}
	// The read/write contract. position is writable (a seek slider assigns
	// it); the rest are read-only values other bindings watch.
	doc.exposeIDProp(n, "source", mediaSourceRef{m})
	doc.exposeIDProp(n, "position", m.position)
	doc.exposeIDProp(n, "duration", m.duration)
	doc.exposeIDProp(n, "playbackState", m.playback)
	doc.exposeIDProp(n, "mediaStatus", m.status)
	doc.exposeIDProp(n, "playing", mediaNum(m.playback) == mediaPlayingState)
	doc.exposeIDProp(n, "seekable", false)
	doc.exposeIDProp(n, "hasAudio", false)
	doc.exposeIDProp(n, "hasVideo", false)
	doc.exposeIDProp(n, "bufferProgress", float64(0))
	doc.exposeIDProp(n, "playbackRate", float64(1))
	doc.exposeIDProp(n, "volume", m.volume)
	doc.exposeIDProp(n, "muted", m.muted)
	doc.exposeIDProp(n, "error", float64(0))
	doc.exposeIDProp(n, "errorString", mediaErrorString(m))
	doc.exposeIDMethods(n, map[string]any{
		// The transport API. With no decoder these settle the state machine
		// rather than starting anything: play() on an unplayable source
		// leaves it stopped, as Qt's does.
		"play": jsFunc(func([]any) (any, error) {
			if s := mediaNum(m.status); s == mediaLoadedMedia || s == mediaBuffered {
				return nil, m.playback.set(mediaPlayingState)
			}
			return nil, nil
		}),
		"pause": jsFunc(func([]any) (any, error) {
			if mediaNum(m.playback) == mediaPlayingState {
				return nil, m.playback.set(mediaPausedState)
			}
			return nil, nil
		}),
		"stop": jsFunc(func([]any) (any, error) {
			if err := m.playback.set(mediaStoppedState); err != nil {
				return nil, err
			}
			return nil, m.position.set(0)
		}),
		"seek":        mediaSeek(m),
		"setPosition": mediaSeek(m),
	})
}

// mediaSeek is the position-setting API (seek / setPosition).
func mediaSeek(m *mediaState) jsFunc {
	return func(args []any) (any, error) {
		if len(args) == 0 {
			return nil, nil
		}
		f, err := toFloat64(args[0])
		if err != nil {
			return nil, nil
		}
		return nil, m.position.set(f)
	}
}

// setMediaSource assigns a source and settles the status it implies: with no
// decoder in this build, anything but the empty source is invalid media.
func setMediaSource(m *mediaState, src string) error {
	if src == str(m.source.get()) {
		return nil
	}
	if err := m.source.set(src); err != nil {
		return err
	}
	status := mediaInvalidMedia
	if src == "" {
		status = mediaNoMedia
	}
	if err := m.status.set(status); err != nil {
		return err
	}
	if err := m.playback.set(mediaStoppedState); err != nil {
		return err
	}
	if err := m.position.set(0); err != nil {
		return err
	}
	return m.duration.set(0)
}

// mediaSourceRef makes `player.source` assignable (a URL field's Load button
// writes it) and settles the status that follows.
type mediaSourceRef struct{ m *mediaState }

func (r mediaSourceRef) get() any                         { return r.m.source.get() }
func (r mediaSourceRef) set(v any) error                  { return setMediaSource(r.m, str(v)) }
func (r mediaSourceRef) animate(any, time.Duration) error { return nil }
func (r mediaSourceRef) onChange(fn func())               { r.m.source.onChange(fn) }

// mediaErrorString is what the player reports about its current media.
func mediaErrorString(m *mediaState) string {
	if mediaNum(m.status) == mediaInvalidMedia {
		return "no media backend: this build decodes no audio or video"
	}
	return ""
}

// buildVideoOutput renders a VideoOutput: the frame area, black, since no
// frames are produced.
func buildVideoOutput(n *node, env *Env, doc *docState) (ui.Element, error) {
	var w, h float32
	grow := false
	for _, a := range n.attrs {
		switch a.name {
		case "width":
			if f, err := attrFloat(a, env); err == nil {
				w = f
			}
		case "height":
			if f, err := attrFloat(a, env); err == nil {
				h = f
			}
		case "anchors.fill":
			grow = true
		default:
			// fillMode, orientation, the source/captureSession binding and
			// the rest of the item chrome: accepted, nothing to apply.
		}
	}
	box := ui.Div().Bg(ui.RGBA{A: 1})
	if w > 0 || h > 0 {
		box.Size(w, h)
	}
	if grow {
		box.Grow()
	}
	return box, nil
}

// refreshCaptureSession services the capture side: a CaptureSession with its
// Camera and ImageCapture. There is no camera backend, so the session stays
// inactive and its objects report that.
func (doc *docState) refreshCaptureSession(n *node, env *Env) {
	for _, a := range n.attrs {
		if a.elem == nil {
			continue
		}
		switch a.name {
		case "camera":
			doc.exposeIDProp(a.elem, "active", false)
			doc.exposeIDProp(a.elem, "cameraDevice", nil)
			doc.exposeIDProp(a.elem, "errorString", "no camera backend")
			doc.exposeIDMethods(a.elem, map[string]any{
				"start": jsFunc(func([]any) (any, error) { return nil, nil }),
				"stop":  jsFunc(func([]any) (any, error) { return nil, nil }),
			})
		case "imageCapture":
			doc.exposeIDProp(a.elem, "readyForCapture", false)
			doc.exposeIDMethods(a.elem, map[string]any{
				"capture":       jsFunc(func([]any) (any, error) { return float64(-1), nil }),
				"captureToFile": jsFunc(func([]any) (any, error) { return float64(-1), nil }),
			})
		case "recorder", "videoOutput", "audioInput":
			// accepted; nothing to drive
		}
	}
}

// refreshMediaDevices exposes MediaDevices' device lists — all empty, since
// nothing here enumerates hardware.
func (doc *docState) refreshMediaDevices(n *node, env *Env) {
	doc.exposeIDProp(n, "videoInputs", []any{})
	doc.exposeIDProp(n, "audioInputs", []any{})
	doc.exposeIDProp(n, "audioOutputs", []any{})
	doc.exposeIDProp(n, "defaultVideoInput", nil)
	doc.exposeIDProp(n, "defaultAudioInput", nil)
	doc.exposeIDProp(n, "defaultAudioOutput", nil)
}

// exposeIDMethods attaches methods to an element's id object.
// exposeIDMethodsIn publishes methods on EVERY object this id names — the
// one bound in the current scope, and the ones registered under the id with
// and without the instance prefix. Inside a file component those are not the
// same object: the declare pass bound the id bare, the build pass runs under
// the instance's prefix, and a handler carries whichever it captured. A
// method published on only one of them fails as `no member "open"` from the
// button that was supposed to call it.
func (doc *docState) exposeIDMethodsIn(n *node, env *Env, methods map[string]any) {
	id := nodeID(n)
	if id == "" {
		return
	}
	targets := []*elemObject{doc.elemObjs.get(doc.here(id)), doc.elemObjs.get(doc.intern(id))}
	if obj, ok := env.vars[id].(*elemObject); ok {
		targets = append(targets, obj)
	}
	// A styled control publishes under the STYLE's own id (`root`), but the
	// page calls it by the id it gave the control — `infoDialog.open()` —
	// and the page's handlers captured whichever object that id named where
	// they were written. Every one of them answers.
	if us := doc.useSite; us.id != "" {
		targets = append(targets, us.objs...)
		if obj, ok := env.vars[us.id].(*elemObject); ok {
			targets = append(targets, obj)
		}
	}
	seen := map[*elemObject]bool{}
	for _, obj := range targets {
		if obj == nil || seen[obj] {
			continue
		}
		seen[obj] = true
		if obj.methods == nil {
			obj.methods = map[string]any{}
		}
		for k, v := range methods {
			obj.methods[k] = v
		}
	}
	if len(seen) == 0 {
		doc.exposeIDMethods(n, methods) // creates the object
	}
}

func (doc *docState) exposeIDMethods(n *node, methods map[string]any) {
	id := nodeID(n)
	if id == "" {
		return
	}
	obj := doc.elemObjs.get(doc.here(id))
	if obj == nil {
		obj = &elemObject{props: map[string]any{}}
		doc.elemObjs.set(doc.here(id), obj)
	}
	if obj.methods == nil {
		obj.methods = map[string]any{}
	}
	for k, v := range methods {
		obj.methods[k] = v
	}
}

// isMediaType reports the QtMultimedia declarations serviced here (they
// render nothing themselves).
func isMediaType(t string) bool {
	switch t {
	case "MediaPlayer", "CaptureSession", "MediaDevices", "AudioOutput",
		"AudioInput", "Camera", "ImageCapture", "MediaRecorder", "SoundEffect":
		return true
	}
	return false
}
