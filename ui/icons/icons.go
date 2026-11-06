// Package icons is the framework's built-in glyph set: Lucide-flavored
// (MIT) 24×24 stroke icons, tinted at request time, plus a registry for
// app-specific glyphs. Icons live in the binary (no asset files), so they
// work identically on native and js/wasm.
//
// gml markup consumes them through the "icon:" Image source scheme:
//
//	Image { source: "icon:undo" }            // default ink (#000000)
//	Image { source: "icon:undo/" + thText }  // tinted (any CSS hex color)
//
// Apps register their own glyphs with Register (stroked, inheriting the
// tint) or RegisterFull (self-colored artwork the tint must not touch).
package icons

import (
	"fmt"
	"sort"
	"sync"

	"github.com/sushvibez/gfx/render"
)

var (
	mu     sync.RWMutex
	stroke = map[string]string{} // inner SVG, inherits stroke color
	full   = map[string]string{} // inner SVG, self-colored (tint ignored)
	// parsed caches Parse by name and tint; Register clears it.
	parsed = map[[2]string]*render.SVG{}
)

// Register adds (or replaces) a stroked glyph: body is inner SVG drawn
// with the requested tint (fill=none, stroke inherited, width 2).
func Register(name, body string) {
	mu.Lock()
	stroke[name] = body
	clear(parsed)
	mu.Unlock()
}

// RegisterFull adds (or replaces) a self-colored glyph (brand tiles,
// multi-color artwork): body keeps its own fills; tint is ignored.
func RegisterFull(name, body string) {
	mu.Lock()
	full[name] = body
	clear(parsed)
	mu.Unlock()
}

// Has reports whether a glyph is registered under name.
func Has(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, s := stroke[name]
	_, f := full[name]
	return s || f
}

// SVG builds the complete SVG document for a glyph tinted with color
// (a CSS color; "" means #000000). It returns nil for unknown names.
func SVG(name, color string) []byte {
	mu.RLock()
	defer mu.RUnlock()
	if body, ok := full[name]; ok {
		return []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` + body + `</svg>`)
	}
	body, ok := stroke[name]
	if !ok {
		return nil
	}
	if color == "" {
		color = "#000000"
	}
	return []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="` +
		color + `" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`)
}

// The built-in Lucide-flavored set (lucide.dev, MIT license).
func init() {
	for name, body := range map[string]string{
		"back":   `<path d="M19 12H5"/><path d="m12 19-7-7 7-7"/>`,
		"close":  `<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`,
		"minus":  `<path d="M5 12h14"/>`,
		"maxim":  `<rect width="16" height="16" x="4" y="4" rx="2"/>`,
		"plus":   `<path d="M5 12h14"/><path d="M12 5v14"/>`,
		"search": `<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`,
		"eye":    `<path d="M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0"/><circle cx="12" cy="12" r="3"/>`,
		"eyeOff": `<path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49"/><path d="M14.084 14.158a3 3 0 0 1-4.242-4.242"/><path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143"/><path d="m2 2 20 20"/>`,
		"pencil": `<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/><path d="m15 5 4 4"/>`,
		"clock":  `<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>`,
		"filter": `<path d="M10 20a1 1 0 0 0 .553.895l2 1A1 1 0 0 0 14 21v-7a2 2 0 0 1 .517-1.341L21.74 4.67A1 1 0 0 0 21 3H3a1 1 0 0 0-.742 1.67l7.225 7.989A2 2 0 0 1 10 14z"/>`,
		"folder": `<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`,
		"dots":   `<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>`,
		"chevD":  `<path d="m6 9 6 6 6-6"/>`,
		"chevR":  `<path d="m9 18 6-6-6-6"/>`,
		"lock":   `<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`,
		"unlock": `<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/>`,
		"magnet": `<path d="m6 15-4-4 6.75-6.77a7.79 7.79 0 0 1 11 11L13 22l-4-4 6.39-6.36a2.14 2.14 0 0 0-3-3L6 15"/><path d="m5 8 4 4"/><path d="m12 15 4 4"/>`,
		"camera": `<path d="M14.5 4h-5L7 7H4a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2h-3l-2.5-3z"/><circle cx="12" cy="13" r="3"/>`,
		"undo":   `<path d="M3 7v6h6"/><path d="M21 17a9 9 0 0 0-9-9 9 9 0 0 0-6 2.3L3 13"/>`,
		"redo":   `<path d="M21 7v6h-6"/><path d="M3 17a9 9 0 0 1 9-9 9 9 0 0 1 6 2.3L21 13"/>`,
		"select": `<path d="M12.586 12.586 19 19"/><path d="M3.688 3.037a.497.497 0 0 0-.651.651l6.5 15.999a.501.501 0 0 0 .947-.062l1.569-6.083a2 2 0 0 1 1.448-1.479l6.124-1.579a.5.5 0 0 0 .063-.947z"/>`,
		"check":  `<path d="M20 6 9 17l-5-5"/>`,
		"rect":   `<rect width="16" height="12" x="4" y="6" rx="1"/>`,
		"circle": `<circle cx="12" cy="12" r="8"/>`,
		"theme":  `<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>`,
		"dock":   `<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/>`,
		"share":  `<path d="M4 12v7a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-7"/><path d="M16 6l-4-4-4 4"/><path d="M12 2v13"/>`,
		// Names QML markup asks for by Qt/Material spelling.
		"delete":     `<path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><path d="M10 11v6"/><path d="M14 11v6"/>`,
		"trash":      `<path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>`,
		"volume":     `<path d="M11 4.702a.705.705 0 0 0-1.203-.498L6.413 7.587A1.4 1.4 0 0 1 5.416 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.416a1.4 1.4 0 0 1 .997.413l3.384 3.383A.705.705 0 0 0 11 19.298z"/><path d="M16 9a5 5 0 0 1 0 6"/>`,
		"volume-off": `<path d="M16 9a5 5 0 0 1 .95 2.293"/><path d="M19.364 5.636a9 9 0 0 1 1.889 9.96"/><path d="m2 2 20 20"/><path d="m7 7-.587.587A1.4 1.4 0 0 1 5.416 8H3a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2.416a1.4 1.4 0 0 1 .997.413l3.383 3.384A.705.705 0 0 0 11 19.298V11"/><path d="M11 4.702a.705.705 0 0 0-1.203-.498L9 5"/>`,
		"settings":   `<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>`,
		"info":       `<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>`,
		"home":       `<path d="M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8"/><path d="M3 10a2 2 0 0 1 .709-1.528l7-5.999a2 2 0 0 1 2.582 0l7 5.999A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`,
	} {
		stroke[name] = body
	}
}

// Names lists every registered glyph, built-in and registered, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(stroke)+len(full))
	for n := range stroke {
		names = append(names, n)
	}
	for n := range full {
		if _, dup := stroke[n]; !dup {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Parse returns the glyph parsed for drawing (render.Renderer2D's SVG
// calls), tinted with color as SVG does; an unknown name is an error. The
// result is cached per name and tint and shared, so callers must not
// modify it; registering a glyph clears the cache.
func Parse(name, color string) (*render.SVG, error) {
	key := [2]string{name, color}
	mu.RLock()
	s, ok := parsed[key]
	mu.RUnlock()
	if ok {
		return s, nil
	}
	data := SVG(name, color)
	if data == nil {
		return nil, fmt.Errorf("icons: no glyph named %q", name)
	}
	s, err := render.ParseSVG(data)
	if err != nil {
		return nil, fmt.Errorf("icons: %s: %w", name, err)
	}
	mu.Lock()
	parsed[key] = s
	mu.Unlock()
	return s, nil
}
