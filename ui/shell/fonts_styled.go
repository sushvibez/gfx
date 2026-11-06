package shell

import (
	"os"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"

	"github.com/sushvibez/gfx/render"
)

// styledFaces lists candidate files for one style of one family (first found
// wins), mirroring namedFamilyFiles' regular lists.
type styledFaces map[render.FontStyle][]string

// namedFamilyStyles are the real bold/italic/bold-italic faces of the named
// families in namedFamilyFiles. Registered best-effort; a missing style
// falls back to synthetic emphasis at draw time.
var namedFamilyStyles = map[string]styledFaces{
	"monospace": {
		render.StyleBold: {
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
			"/usr/share/fonts/truetype/ubuntu/UbuntuMono-B.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Bold.ttf",
		},
		render.StyleItalic: {
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Oblique.ttf",
			"/usr/share/fonts/truetype/ubuntu/UbuntuMono-RI.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Italic.ttf",
		},
		render.StyleBold | render.StyleItalic: {
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-BoldOblique.ttf",
			"/usr/share/fonts/truetype/ubuntu/UbuntuMono-BI.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-BoldItalic.ttf",
		},
	},
	"menlo": {
		render.StyleBold:                      {"/usr/share/fonts/truetype/ubuntu/UbuntuMono-B.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"},
		render.StyleItalic:                    {"/usr/share/fonts/truetype/ubuntu/UbuntuMono-RI.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Oblique.ttf"},
		render.StyleBold | render.StyleItalic: {"/usr/share/fonts/truetype/ubuntu/UbuntuMono-BI.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-BoldOblique.ttf"},
	},
	"monaco": {
		render.StyleBold:                      {"/usr/share/fonts/opentype/urw-base35/NimbusMonoPS-Bold.otf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"},
		render.StyleItalic:                    {"/usr/share/fonts/opentype/urw-base35/NimbusMonoPS-Italic.otf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Oblique.ttf"},
		render.StyleBold | render.StyleItalic: {"/usr/share/fonts/opentype/urw-base35/NimbusMonoPS-BoldItalic.otf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-BoldOblique.ttf"},
	},
	"courier new": {
		render.StyleBold:                      {"/usr/share/fonts/truetype/liberation/LiberationMono-Bold.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"},
		render.StyleItalic:                    {"/usr/share/fonts/truetype/liberation/LiberationMono-Italic.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Oblique.ttf"},
		render.StyleBold | render.StyleItalic: {"/usr/share/fonts/truetype/liberation/LiberationMono-BoldItalic.ttf", "/usr/share/fonts/truetype/dejavu/DejaVuSansMono-BoldOblique.ttf"},
	},
	"serif": {
		render.StyleBold: {
			"/usr/share/fonts/truetype/dejavu/DejaVuSerif-Bold.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationSerif-Bold.ttf",
			"/usr/share/fonts/truetype/noto/NotoSerif-Bold.ttf",
		},
		render.StyleItalic: {
			"/usr/share/fonts/truetype/dejavu/DejaVuSerif-Italic.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationSerif-Italic.ttf",
			"/usr/share/fonts/truetype/noto/NotoSerif-Italic.ttf",
		},
		render.StyleBold | render.StyleItalic: {
			"/usr/share/fonts/truetype/dejavu/DejaVuSerif-BoldItalic.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationSerif-BoldItalic.ttf",
			"/usr/share/fonts/truetype/noto/NotoSerif-BoldItalic.ttf",
		},
	},
}

// defaultStyleFiles maps the file that WON the default-font pick (see
// defaultFontBytes) to its family's styled companions, so Bold()/Italic()
// on default-font text use the real faces. Keyed by the regular file's path.
var defaultStyleFiles = map[string]styledFaces{
	// Ubuntu Sans (variable regular; the italic ships as its own variable
	// file, and there is no static bold — see the note in item 69 of
	// script/gml/check/QML-EXAMPLES.md: a bold face has to be synthesized
	// on a system that ships only variable fonts).
	"/usr/share/fonts/truetype/ubuntu/UbuntuSans[wdth,wght].ttf": {
		render.StyleItalic: {"/usr/share/fonts/truetype/ubuntu/UbuntuSans-Italic[wdth,wght].ttf"},
	},
	// Ubuntu (variable regular; static B/RI/BI companions ship alongside).
	"/usr/share/fonts/truetype/ubuntu/Ubuntu[wdth,wght].ttf": {
		render.StyleBold:                      {"/usr/share/fonts/truetype/ubuntu/Ubuntu-B.ttf"},
		render.StyleItalic:                    {"/usr/share/fonts/truetype/ubuntu/Ubuntu-RI.ttf", "/usr/share/fonts/truetype/ubuntu/Ubuntu-Italic[wdth,wght].ttf"},
		render.StyleBold | render.StyleItalic: {"/usr/share/fonts/truetype/ubuntu/Ubuntu-BI.ttf"},
	},
	"/usr/share/fonts/ubuntu/Ubuntu-Regular.ttf": {
		render.StyleBold:                      {"/usr/share/fonts/ubuntu/Ubuntu-Bold.ttf"},
		render.StyleItalic:                    {"/usr/share/fonts/ubuntu/Ubuntu-Italic.ttf"},
		render.StyleBold | render.StyleItalic: {"/usr/share/fonts/ubuntu/Ubuntu-BoldItalic.ttf"},
	},
	"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf": {
		render.StyleBold:                      {"/usr/share/fonts/truetype/noto/NotoSans-Bold.ttf"},
		render.StyleItalic:                    {"/usr/share/fonts/truetype/noto/NotoSans-Italic.ttf"},
		render.StyleBold | render.StyleItalic: {"/usr/share/fonts/truetype/noto/NotoSans-BoldItalic.ttf"},
	},
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf": {
		render.StyleBold:                      {"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"},
		render.StyleItalic:                    {"/usr/share/fonts/truetype/dejavu/DejaVuSans-Oblique.ttf"},
		render.StyleBold | render.StyleItalic: {"/usr/share/fonts/truetype/dejavu/DejaVuSans-BoldOblique.ttf"},
	},
	"/usr/share/fonts/dejavu/DejaVuSans.ttf": {
		render.StyleBold:                      {"/usr/share/fonts/dejavu/DejaVuSans-Bold.ttf"},
		render.StyleItalic:                    {"/usr/share/fonts/dejavu/DejaVuSans-Oblique.ttf"},
		render.StyleBold | render.StyleItalic: {"/usr/share/fonts/dejavu/DejaVuSans-BoldOblique.ttf"},
	},
	// Windows: Segoe UI / Arial style companions (standard filenames).
	`\Fonts\segoeui.ttf`: {
		render.StyleBold:                      {`\Fonts\segoeuib.ttf`},
		render.StyleItalic:                    {`\Fonts\segoeuii.ttf`},
		render.StyleBold | render.StyleItalic: {`\Fonts\segoeuiz.ttf`},
	},
	`\Fonts\arial.ttf`: {
		render.StyleBold:                      {`\Fonts\arialbd.ttf`},
		render.StyleItalic:                    {`\Fonts\ariali.ttf`},
		render.StyleBold | render.StyleItalic: {`\Fonts\arialbi.ttf`},
	},
	// macOS Helvetica is a .ttc our loader can't split — no companions.
}

// registerStyledFaces loads one styledFaces set into the renderer under
// family (best-effort, first usable file per style wins). A bold candidate
// must really be bold: "-Bold.ttf" names that are symlinks to variable fonts
// render at their regular default instance, which we can't re-weight — skip
// those so a later static candidate (or synthetic emphasis) applies.
func registerStyledFaces(r *render.Renderer2D, family string, faces styledFaces) {
	for style, paths := range faces {
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if style&render.StyleBold != 0 && render.FaceWeightClass(b) < 600 {
				continue
			}
			r.RegisterFontStyled(family, style, b)
			break
		}
	}
}

// registerDefaultStyles registers the default font's real bold/italic faces
// under the empty family. regularPath is the file the default font came from
// ("" when it's the bundled Go font, whose styled companions are bundled
// too and always available).
func registerDefaultStyles(r *render.Renderer2D, regularPath string) {
	if regularPath == "" {
		r.RegisterFontStyled("", render.StyleBold, gobold.TTF)
		r.RegisterFontStyled("", render.StyleItalic, goitalic.TTF)
		r.RegisterFontStyled("", render.StyleBold|render.StyleItalic, gobolditalic.TTF)
		return
	}
	if faces := lookupDefaultStyles(regularPath); faces != nil {
		registerStyledFaces(r, "", faces)
	}
}

// lookupDefaultStyles finds companions for a default-font path; Windows
// entries are keyed by their path suffix under any SystemRoot.
func lookupDefaultStyles(regularPath string) styledFaces {
	if faces, ok := defaultStyleFiles[regularPath]; ok {
		return faces
	}
	for key, faces := range defaultStyleFiles {
		if key[0] == '\\' && len(regularPath) > len(key) &&
			regularPath[len(regularPath)-len(key):] == key {
			// Resolve the relative style paths against the same root.
			root := regularPath[:len(regularPath)-len(key)]
			resolved := styledFaces{}
			for style, paths := range faces {
				for _, p := range paths {
					resolved[style] = append(resolved[style], root+p)
				}
			}
			return resolved
		}
	}
	return nil
}
