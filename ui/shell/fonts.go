package shell

import (
	"os"
	"runtime"
	"strconv"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/sushvibez/gfx/render"
)

// applyTextTuning applies the GFX_TEXT_GAMMA / GFX_TEXT_CONTRAST env knobs
// (subpixel-text weight tuning: GPUI defaults are gamma 1.8, enhanced
// contrast 0.5 — see Renderer2D.SetTextGamma/SetTextContrast) so text
// weight can be A/B'd against native renderers without a rebuild.
func applyTextTuning(r *render.Renderer2D) {
	// The renderer ships GPUI's wgpu defaults (1.8/0.5), tuned for GPUI's
	// glyph rasterizer. Ours runs a touch heavier on Windows next to real
	// DirectWrite text; 2.0/0.4 was matched by eye against GPUI examples
	// rendering Segoe UI on the same screen.
	gamma, contrast := float64(1.8), float64(0.5)
	set := false
	if runtime.GOOS == "windows" {
		gamma, contrast, set = 2.0, 0.4, true
	}
	if v, err := strconv.ParseFloat(os.Getenv("GFX_TEXT_GAMMA"), 64); err == nil {
		gamma, set = v, true
	}
	if v, err := strconv.ParseFloat(os.Getenv("GFX_TEXT_CONTRAST"), 64); err == nil {
		contrast, set = v, true
	}
	if set {
		r.SetTextGamma(float32(gamma), float32(contrast))
	}
}

// applyTextPath picks the text path: the renderer's per-draw choice (the
// default — the field for transformed text, the raster for the rest; see
// render.SetMSDFAuto) or the distance field for everything. GFX_MSDF=1|0 overrides whatever
// the app asked for, so the two can be compared on the same binary —
// the reason this is an env knob and not only an Option.
//
// Returns whether the field path is actually in use: a device without
// the pipeline silently stays on the raster one.
func applyTextPath(r *render.Renderer2D, want bool) bool {
	switch os.Getenv("GFX_MSDF") {
	case "1":
		want = true
	case "0":
		// The raster everywhere, transformed text included — the
		// comparison this knob exists for.
		want = false
		r.SetMSDFAuto(false)
	}
	r.SetMSDFText(want)
	return r.MSDFTextEnabled()
}

// namedFamilyFiles maps a font-family name to candidate system files
// (first found wins; Linux paths first, then Windows equivalents) so gml
// Text's font.family can resolve "monospace" / "serif". Sans/default
// already comes from defaultFontBytes.
var namedFamilyFiles = map[string][]string{
	"monospace": {
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/truetype/ubuntu/UbuntuMono[wght].ttf",
		"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
		`C:\Windows\Fonts\consola.ttf`, // Consolas, Windows' default mono
	},
	// Closest installed stand-ins for common mono families (gpui's text
	// example cycles them): Liberation Mono is the Courier New metric clone;
	// Ubuntu Mono / Nimbus Mono substitute for Menlo / Monaco on Linux,
	// Consolas / Lucida Console on Windows.
	"menlo": {
		"/usr/share/fonts/truetype/ubuntu/UbuntuMono-R.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		`C:\Windows\Fonts\consola.ttf`,
	},
	"monaco": {
		"/usr/share/fonts/opentype/urw-base35/NimbusMonoPS-Regular.otf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		`C:\Windows\Fonts\lucon.ttf`, // Lucida Console
		`C:\Windows\Fonts\consola.ttf`,
	},
	"courier new": {
		"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		`C:\Windows\Fonts\cour.ttf`, // the real Courier New
	},
	"serif": {
		"/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSerif-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSerif-Regular.ttf",
		`C:\Windows\Fonts\times.ttf`, // Times New Roman
		`C:\Windows\Fonts\georgia.ttf`,
	},
}

// registerNamedFonts loads the named families into the renderer (best-effort),
// including their real bold/italic faces.
func registerNamedFonts(r *render.Renderer2D) {
	for family, paths := range namedFamilyFiles {
		for _, p := range paths {
			if b, err := os.ReadFile(p); err == nil {
				r.RegisterFont(family, b)
				break
			}
		}
	}
	for family, faces := range namedFamilyStyles {
		registerStyledFaces(r, family, faces)
	}
}

// gpuiFallbackFonts lists candidate system UI fonts per OS, in roughly the
// order GPUI's text system walks its fallback stack when its configured family
// (".ZedSans" → "IBM Plex Sans") isn't installed (see Zed
// crates/gpui/src/text_system.rs fallback_font_stack: Helvetica, Segoe UI,
// Ubuntu, Cantarell, Noto Sans, DejaVu Sans, Arial). We point at the file that
// backs each so the example text matches what Zed actually renders on the same
// box. Anything not found is skipped; the bundled Go font is the final,
// always-present fallback (and covers js/wasm, where these paths don't exist).
func gpuiFallbackFonts() []string {
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return []string{ // Segoe UI is GPUI's Windows fallback; Arial is the last.
			root + `\Fonts\segoeui.ttf`,
			root + `\Fonts\arial.ttf`,
		}
	case "darwin":
		return []string{ // Helvetica is GPUI's macOS fallback; Arial/SF as backups.
			"/System/Library/Fonts/Helvetica.ttc",
			"/Library/Fonts/Arial.ttf",
			"/System/Library/Fonts/SFNS.ttf",
		}
	default: // linux and other unixes
		return []string{
			// Ubuntu Sans first: it is what a current Ubuntu resolves
			// "sans-serif" to (so it is what Qt and a browser pick), and
			// its metrics are the ones a page laid out against the desktop
			// expects — every multi-line paragraph in the QML corpus sat a
			// pixel per line tighter than Qt's until we agreed on the face.
			"/usr/share/fonts/truetype/ubuntu/UbuntuSans[wdth,wght].ttf",
			"/usr/share/fonts/truetype/ubuntu/Ubuntu[wdth,wght].ttf", // Ubuntu (variable; default instance = Regular)
			"/usr/share/fonts/ubuntu/Ubuntu-Regular.ttf",
			"/usr/share/fonts/cantarell/Cantarell-VF.otf",
			"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/dejavu/DejaVuSans.ttf",
		}
	}
}

// cjkFallbackFonts lists single-face CJK fonts (OTF/TTF — NOT .ttc collections,
// which x/image/opentype can't open) to use as a glyph fallback behind the Latin
// UI font, so e.g. text_wrapper's 你好世界 / こんにちは renders. GFX_CJK_FONT
// overrides. build/fonts holds the dev-fetched Noto Sans CJK (see scripts).
var cjkFallbackFonts = []string{
	"build/fonts/NotoSansCJKsc-Regular.otf",
	"../build/fonts/NotoSansCJKsc-Regular.otf", // launched from a module dir
	"/usr/share/fonts/opentype/noto/NotoSansCJKsc-Regular.otf",
	"/usr/share/fonts/truetype/noto/NotoSansSC-Regular.otf",
}

// fallbackFontBytes returns CJK fallback font bytes if one is available, else nil.
func fallbackFontBytes() []byte {
	if p := os.Getenv("GFX_CJK_FONT"); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	for _, p := range cjkFallbackFonts {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	return nil
}

// symbolFallbackFonts are the faces carrying the pictographic/dingbat blocks
// that UI fonts routinely skip — a menu's ▸, a toolbar's ⌂ and ↻, a combo
// box's ▾. Qt gets them from its own font substitution; without a fallback
// here those runes simply do not draw.
//
// It is a CHAIN, not a single face, because no one font has all of it: the
// broad Latin-plus-symbols faces (DejaVu) stop before the sparser blocks
// (⎘ NEXT PAGE, ⭳ arrow-to-bar, ⮞ arrowhead), which live in Noto's two
// symbol families. Each group lists interchangeable paths — the first that
// exists is taken — and GFX_SYMBOL_FONT goes in front of all of them.
var symbolFallbackFonts = [][]string{
	{
		"build/fonts/NotoSansSymbols2-Regular.ttf",
		"../build/fonts/NotoSansSymbols2-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansSymbols2-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansSymbols2-Regular.ttf",
	},
	{
		"build/fonts/NotoSansSymbols-Regular.ttf",
		"../build/fonts/NotoSansSymbols-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansSymbols-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansSymbols-Regular.ttf",
	},
	{
		"build/fonts/DejaVuSans.ttf",
		"../build/fonts/DejaVuSans.ttf", // launched from a module dir
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/freefont/FreeSerif.ttf",
	},
}

// symbolFontChain returns every symbol face that is available, in order.
func symbolFontChain() [][]byte {
	var chain [][]byte
	if p := os.Getenv("GFX_SYMBOL_FONT"); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			chain = append(chain, b)
		}
	}
	for _, group := range symbolFallbackFonts {
		for _, p := range group {
			if b, err := os.ReadFile(p); err == nil {
				chain = append(chain, b)
				break // the rest of the group is the same face elsewhere
			}
		}
	}
	return chain
}

// emojiFallbackFonts are the color-emoji (CBDT) faces we look for, in order.
var emojiFallbackFonts = []string{
	"build/fonts/NotoColorEmoji.ttf",
	"../build/fonts/NotoColorEmoji.ttf", // launched from a module dir
	"/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf",
	"/usr/share/fonts/noto/NotoColorEmoji.ttf",
}

// emojiFontBytes returns color-emoji font bytes if a face is available.
func emojiFontBytes() []byte {
	if p := os.Getenv("GFX_EMOJI_FONT"); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	for _, p := range emojiFallbackFonts {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	return nil
}

// defaultFontBytes returns the TTF/OTF bytes used to render example text.
// GFX_FONT (a font file path) overrides everything; otherwise we try the
// GPUI-aligned system fonts for this OS so screenshots match Zed, and finally
// the bundled Go font when none are found (always works, including wasm).
func defaultFontBytes() []byte {
	b, _ := defaultFontFile()
	return b
}

// defaultFontFile is defaultFontBytes plus the path the bytes came from
// ("" = the bundled Go font), so callers can register the file's styled
// bold/italic companions (see fonts_styled.go).
func defaultFontFile() ([]byte, string) {
	if p := os.Getenv("GFX_FONT"); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			return b, p
		}
	}
	for _, p := range gpuiFallbackFonts() {
		if b, err := os.ReadFile(p); err == nil {
			return b, p
		}
	}
	return goregular.TTF, ""
}
