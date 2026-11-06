// Package corpus is the shared vocabulary for the hand-written Go ports of
// the QML-Examples-Collection pages (examples/qmlgo/<page>).
//
// Why the ports exist: every corpus page is already rendered twice — by Qt
// and by our .gml engine — but both of those go through markup. A third
// rendering written directly against the ui builder API is what shows
// whether a widget is really IN the framework or only in the gml layer,
// and it keeps the two front ends honest about each other. Anything a port
// cannot say without a local helper is a gap in ui, and it is recorded in
// script/gml/check/QML-EXAMPLES.md.
//
// The corpus dresses its controls through its own QML style module
// (styles/qmlsnippetsstyle), so a Go port has to restate that look; this
// package is that restatement, once, for every page.
package corpus

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sushvibez/gfx/render"
	"github.com/sushvibez/gfx/ui"
	"github.com/sushvibez/gfx/ui/shell"
)

// The corpus' Style singleton (imports/utils/Style.qml), as constants.
var (
	Bg            = ui.RGB(0x1A1D23)
	BgDark        = ui.RGB(0x14171C)
	Card          = ui.RGB(0x252830)
	Surface       = ui.RGB(0x2A2D35)
	Main          = ui.RGB(0x00D1A9)
	Inactive      = ui.RGB(0x999999)
	FontContrast  = ui.RGB(0xFFFFFF)
	FontPrimary   = ui.RGB(0xFFFFFF)
	FontSecondary = ui.RGB(0xCCCCCC)
	Grey          = ui.RGB(0x2A2D35)
)

// Font sizes: Style.fontSizeS/M/L, and the Label default (M).
const (
	FontS = 15
	FontM = 19
	FontL = 26
)

// R is Style.resize(), the corpus' one scaling hook. It returns its
// argument today, exactly as the QML does.
func R(v float32) float32 { return v }

// Root is the corpus checkout the assets come from.
func Root() string {
	if v := os.Getenv("CORPUS"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "workspace", "qml-examples")
}

// Fonts are the two faces every page loads through FontLoader (Quicksand
// Book and Bold), for shell.Options.FontPaths.
func Fonts() []string {
	d := filepath.Join(Root(), "imports", "assets", "fonts")
	return []string{filepath.Join(d, "Quicksand_Book.otf"), filepath.Join(d, "Quicksand_Bold.otf")}
}

// Options is the window every port opens: the same 900x700 the compare
// harness renders both other sides at.
func Options(page string) shell.Options {
	return shell.Options{
		Title: "go: " + page, Width: 900, Height: 700,
		FontSize: FontM, FontPaths: Fonts(), ClearHex: 0x1A1D23,
	}
}

// Icons decodes the corpus' PNG icons once, on the app's renderer.
type Icons struct {
	r     *render.Renderer2D
	cache map[string]*render.Image
}

func NewIcons(a *shell.App) *Icons {
	return &Icons{r: a.R2, cache: map[string]*render.Image{}}
}

// Get is Style.icon(name): the PNG under assets/icons. A missing icon
// returns nil, which Img draws as nothing (the page still runs).
func (ic *Icons) Get(name string) *render.Image {
	if img, ok := ic.cache[name]; ok {
		return img
	}
	if ic.r == nil {
		return nil // no renderer (a headless benchmark): no icons
	}
	data, err := os.ReadFile(filepath.Join(Root(), "imports", "assets", "icons", name+".png"))
	var img *render.Image
	if err == nil {
		img, _ = ic.r.LoadImage(data)
	}
	ic.cache[name] = img
	return img
}

// Lighter and Darker are Qt.lighter / Qt.darker: the colour's HSV VALUE
// scaled by the factor, everything else untouched.
func Lighter(c ui.RGBA, f float32) ui.RGBA { return scaleValue(c, f) }
func Darker(c ui.RGBA, f float32) ui.RGBA  { return scaleValue(c, 1/f) }

func scaleValue(c ui.RGBA, f float32) ui.RGBA {
	v := max(c.R, max(c.G, c.B))
	if v <= 0 {
		return c
	}
	nv := min(v*f, 1)
	k := nv / v
	return ui.RGBA{R: min(c.R*k, 1), G: min(c.G*k, 1), B: min(c.B*k, 1), A: c.A}
}

// Alpha is Qt.rgba(c.r, c.g, c.b, a) — the same colour at another alpha.
func Alpha(c ui.RGBA, a float32) ui.RGBA { return ui.RGBA{R: c.R, G: c.G, B: c.B, A: a} }

// Label is the style's Label.qml: Quicksand at fontSizeM, primary colour.
func Label(s string) *ui.TextEl { return ui.Text(s, FontPrimary).SizePx(FontM) }

// Title is a page's or a card's heading.
func Title(s string, px float64) *ui.TextEl {
	return ui.Text(s, Main).SizePx(px).Bold()
}

// Note is the small secondary line every card closes with. A Label does
// not wrap unless it is told to (wrapMode), so a note with newlines in it
// is a stack of lines that CLIP at the card's edge, not a paragraph that
// reflows — which is what Qt's own render of the buttons page shows.
func Note(s string) ui.Element {
	lines := strings.Split(s, "\n")
	if len(lines) == 1 {
		return ui.Text(s, FontSecondary).SizePx(12)
	}
	kids := make([]ui.Element, len(lines))
	for i, ln := range lines {
		kids[i] = ui.Text(ln, FontSecondary).SizePx(12)
	}
	return ui.Div().Col().Children(kids...)
}

// CardBox is the corpus' card: cardColor, radius 8, a 20px margin and a
// 15px column inside, headed by its title.
func CardBox(title string, body ...ui.Element) *ui.DivEl {
	kids := []ui.Element{Title(title, 20)}
	return ui.Div().Col().Bg(Card).Rounded(R(8)).P(R(20)).Gap(R(15)).
		Children(append(kids, body...)...)
}

// Page is the frame every Main.qml shares: bgColor behind a 40px margin,
// a ScrollView, and inside it a column of 40 gap headed by the page title.
//
// build is handed the scroll's availableWidth, because that is what the
// markup does — `ColumnLayout { width: scrollView.availableWidth }` is in
// every Main.qml of the corpus, and without it the cards size to their
// content and run off the right edge.
func Page(st *ui.ScrollState, title string, build func(availW float32) []ui.Element) ui.Element {
	avail := st.ViewW()
	if avail <= 0 {
		avail = 900 - 2*R(40) // first frame: the window we open at
	}
	kids := append([]ui.Element{Title(title, 32)}, build(avail)...)
	return ui.Div().Col().Bg(Bg).Grow().Children(
		ui.Div().Col().P(R(40)).Grow().Children(
			ui.Scroll(st).AutoHideBar().Grow().Gap(R(40)).Children(kids...),
		),
	)
}

// Run is every port's main: the corpus window, with the same -screenshot
// and -frames knobs examples/qml has, so a port can be captured for the
// side-by-side board exactly like the other two renders.
func Run(page string, init func(*shell.App) (func() ui.Element, error)) {
	// Qt's LINE metrics, not the face's own: Qt lays text out on the OS/2
	// winAscent/winHeight box, which sits lower and taller than the hhea
	// one, and a port that skips this centres every button label two or
	// three pixels high against the page it is copying. The gml engine
	// turns this on for the same reason (script/gml/doc.go).
	render.SetQtTextMetrics(true)
	frames := flag.Int("frames", 0, "exit after N frames (0 = run until closed)")
	shot := flag.String("screenshot", "", "render headlessly and write a PNG")
	width := flag.Int("w", 900, "window width")
	height := flag.Int("h", 700, "window height")
	script := flag.String("script", "", "interaction script (text, or @file) — capture a PNG per step")
	flag.Parse()
	o := Options(page)
	o.Width, o.Height = *width, *height
	o.MaxFrames = *frames
	hooks := shell.Hooks{Init: init}
	var err error
	if *shot != "" && *script != "" {
		err = shell.RunScript(o, hooks, *script, *shot)
	} else if *shot != "" {
		err = shell.Screenshot(o, hooks, *shot)
	} else {
		err = shell.Run(o, hooks)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "qmlgo/"+page+":", err)
		os.Exit(1)
	}
}
