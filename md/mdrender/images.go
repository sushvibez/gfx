package mdrender

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sushvibez/gfx/render"
)

// ImageSource is where a page's pictures come from. Layout asks for a
// picture's size, Draw for its texture; a picture that has no size yet
// (still loading, or failed) is laid out as its alt text instead.
type ImageSource interface {
	// Size is a picture's natural size in pixels.
	Size(dest string) (w, h float32, ok bool)
	// Image is the picture as a texture on r, or nil.
	Image(r *render.Renderer2D, dest string) *render.Image
}

// MaxImageDim caps a picture's texture: anything larger is scaled down on
// the CPU before upload (the 2D sampler has no mipmaps).
const MaxImageDim = 2048

// Images loads the pictures a document names: files relative to Base,
// http(s) URLs (fetched in the background), data: URIs, and anything
// added with Add. Sizes come from the image header, so a picture is laid
// out before it is decoded; the texture is made on first draw, once per
// renderer.
type Images struct {
	// Base is the directory relative paths are resolved against.
	Base string
	// OnLoad, when set, is called (on the fetching goroutine) whenever a
	// remote picture arrives or fails, so the host can lay the page out
	// again: hand it to the event loop with loop.Post.
	OnLoad func()

	mu      sync.Mutex
	m       map[string]*picture
	gen     int
	pending sync.WaitGroup
}

type picture struct {
	data    []byte
	w, h    int
	loading bool
	err     error
	tex     map[*render.Renderer2D]*render.Image
}

// NewImages is an image source rooted at a directory.
func NewImages(base string) *Images { return &Images{Base: base, m: map[string]*picture{}} }

// Add supplies a picture's bytes under a name, for pictures the host
// already holds (embedded files, say).
func (im *Images) Add(dest string, data []byte) error {
	p := &picture{data: data}
	p.decodeSize()
	im.mu.Lock()
	im.init()
	im.m[dest] = p
	im.gen++
	im.mu.Unlock()
	return p.err
}

// Gen changes whenever a picture arrives, so a cached layout can tell it
// is out of date.
func (im *Images) Gen() int {
	im.mu.Lock()
	defer im.mu.Unlock()
	return im.gen
}

// Wait blocks until every fetch under way has finished.
func (im *Images) Wait() { im.pending.Wait() }

// Err is why a picture did not load, if it did not.
func (im *Images) Err(dest string) error {
	im.mu.Lock()
	defer im.mu.Unlock()
	if p := im.m[dest]; p != nil {
		return p.err
	}
	return nil
}

func (im *Images) init() {
	if im.m == nil {
		im.m = map[string]*picture{}
	}
}

// Size starts loading a picture the first time it is asked for. Local
// files and data: URIs are read there and then; URLs arrive later.
func (im *Images) Size(dest string) (float32, float32, bool) {
	im.mu.Lock()
	im.init()
	p := im.m[dest]
	if p == nil {
		p = &picture{}
		im.m[dest] = p
		if isRemote(dest) {
			p.loading = true
			im.pending.Add(1)
			go im.fetch(dest, p)
		} else {
			p.data, p.err = im.readLocal(dest)
			if p.err == nil {
				p.decodeSize()
			}
		}
	}
	defer im.mu.Unlock()
	if p.loading || p.err != nil || p.w == 0 || p.h == 0 {
		return 0, 0, false
	}
	return float32(p.w), float32(p.h), true
}

// Image uploads a picture to r the first time it is drawn there.
func (im *Images) Image(r *render.Renderer2D, dest string) *render.Image {
	im.mu.Lock()
	defer im.mu.Unlock()
	p := im.m[dest]
	if p == nil || p.loading || p.err != nil || p.data == nil {
		return nil
	}
	if t := p.tex[r]; t != nil {
		return t
	}
	t, err := r.LoadImageScaled(p.data, MaxImageDim)
	if err != nil {
		p.err = err
		return nil
	}
	if p.tex == nil {
		p.tex = map[*render.Renderer2D]*render.Image{}
	}
	p.tex[r] = t
	return t
}

// Release frees every texture made; the pictures stay loaded.
func (im *Images) Release() {
	im.mu.Lock()
	defer im.mu.Unlock()
	for _, p := range im.m {
		for _, t := range p.tex {
			t.Release()
		}
		p.tex = nil
	}
}

func (p *picture) decodeSize() {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(p.data))
	if err != nil {
		p.err = fmt.Errorf("mdrender: image: %w", err)
		return
	}
	p.w, p.h = cfg.Width, cfg.Height
}

func isRemote(dest string) bool {
	return strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://")
}

func (im *Images) readLocal(dest string) ([]byte, error) {
	if rest, ok := strings.CutPrefix(dest, "data:"); ok {
		meta, payload, ok := strings.Cut(rest, ",")
		if !ok {
			return nil, fmt.Errorf("mdrender: malformed data URI")
		}
		if strings.HasSuffix(meta, ";base64") {
			return base64.StdEncoding.DecodeString(payload)
		}
		s, err := url.PathUnescape(payload)
		return []byte(s), err
	}
	path := dest
	if rest, ok := strings.CutPrefix(dest, "file://"); ok {
		path = rest
	} else if u, err := url.PathUnescape(dest); err == nil {
		path = u // ![a](my%20picture.png)
	}
	if !filepath.IsAbs(path) && im.Base != "" {
		path = filepath.Join(im.Base, path)
	}
	return os.ReadFile(path)
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

func (im *Images) fetch(dest string, p *picture) {
	defer im.pending.Done()
	data, err := get(dest)
	q := &picture{data: data, err: err}
	if err == nil {
		q.decodeSize()
	}
	im.mu.Lock()
	p.data, p.w, p.h, p.err, p.loading = q.data, q.w, q.h, q.err, false
	im.gen++
	cb := im.OnLoad
	im.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func get(u string) ([]byte, error) {
	resp, err := httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mdrender: %s: %s", u, resp.Status)
	}
	// 32 MB is more than any picture in a document should be.
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}
