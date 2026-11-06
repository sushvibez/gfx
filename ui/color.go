package ui

// Color parsing shared by markup and Go code: the hex forms plus the
// SVG/Qt color names.

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseColor parses a color string: "#rgb", "#argb", "#rrggbb",
// "#rrggbbaa", a CSS functional form ("rgb(255,0,0)",
// "rgba(255,200,80,0.4)", "hsl(210,50%,40%)", "hsla(...)"), or an SVG/Qt
// color name ("white", "transparent", "steelblue"). Alpha is last in the
// 8-digit form, first in the 4-digit one, matching Qt.
func ParseColor(s string) (RGBA, error) {
	// CSS functional notation, which canvas code writes constantly
	// ("rgba(255,200,80,0.40)" for a soft glow).
	if c, ok, err := parseFuncColor(strings.TrimSpace(s)); ok {
		return c, err
	}
	// SVG/Qt named colors ("white", "transparent", "steelblue", ...).
	if len(s) > 0 && s[0] != '#' {
		if hex, ok := NamedColors[strings.ToLower(strings.TrimSpace(s))]; ok {
			s = hex
		}
	}
	s = strings.TrimPrefix(s, "#")
	var r, g, b, a uint64
	a = 255
	var err error
	switch len(s) {
	case 3, 4: // #rgb / #argb short form (Qt: alpha first when 4 digits)
		if len(s) == 4 {
			a, err = strconv.ParseUint(s[0:1]+s[0:1], 16, 8)
			s = s[1:]
		}
		if err == nil {
			r, err = strconv.ParseUint(s[0:1]+s[0:1], 16, 8)
		}
		if err == nil {
			g, err = strconv.ParseUint(s[1:2]+s[1:2], 16, 8)
		}
		if err == nil {
			b, err = strconv.ParseUint(s[2:3]+s[2:3], 16, 8)
		}
	case 6, 8: // #rrggbb / #rrggbbaa (alpha last — our convention)
		r, err = strconv.ParseUint(s[0:2], 16, 8)
		if err == nil {
			g, err = strconv.ParseUint(s[2:4], 16, 8)
		}
		if err == nil {
			b, err = strconv.ParseUint(s[4:6], 16, 8)
		}
		if err == nil && len(s) == 8 {
			a, err = strconv.ParseUint(s[6:8], 16, 8)
		}
	default:
		return RGBA{}, fmt.Errorf("bad color %q", s)
	}
	if err != nil {
		return RGBA{}, fmt.Errorf("bad color %q", s)
	}
	return RGBA{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: float32(a) / 255}, nil
}

// NamedColors maps SVG/Qt color names to hex. "transparent" maps to a
// fully-transparent black (#00000000, RRGGBBAA).
var NamedColors = map[string]string{
	"transparent": "#00000000",
	"aliceblue":   "#f0f8ff", "antiquewhite": "#faebd7", "aqua": "#00ffff",
	"aquamarine": "#7fffd4", "azure": "#f0ffff", "beige": "#f5f5dc",
	"bisque": "#ffe4c4", "black": "#000000", "blanchedalmond": "#ffebcd",
	"blue": "#0000ff", "blueviolet": "#8a2be2", "brown": "#a52a2a",
	"burlywood": "#deb887", "cadetblue": "#5f9ea0", "chartreuse": "#7fff00",
	"chocolate": "#d2691e", "coral": "#ff7f50", "cornflowerblue": "#6495ed",
	"cornsilk": "#fff8dc", "crimson": "#dc143c", "cyan": "#00ffff",
	"darkblue": "#00008b", "darkcyan": "#008b8b", "darkgoldenrod": "#b8860b",
	"darkgray": "#a9a9a9", "darkgrey": "#a9a9a9", "darkgreen": "#006400",
	"darkkhaki": "#bdb76b", "darkmagenta": "#8b008b", "darkolivegreen": "#556b2f",
	"darkorange": "#ff8c00", "darkorchid": "#9932cc", "darkred": "#8b0000",
	"darksalmon": "#e9967a", "darkseagreen": "#8fbc8f", "darkslateblue": "#483d8b",
	"darkslategray": "#2f4f4f", "darkslategrey": "#2f4f4f", "darkturquoise": "#00ced1",
	"darkviolet": "#9400d3", "deeppink": "#ff1493", "deepskyblue": "#00bfff",
	"dimgray": "#696969", "dimgrey": "#696969", "dodgerblue": "#1e90ff",
	"firebrick": "#b22222", "floralwhite": "#fffaf0", "forestgreen": "#228b22",
	"fuchsia": "#ff00ff", "gainsboro": "#dcdcdc", "ghostwhite": "#f8f8ff",
	"gold": "#ffd700", "goldenrod": "#daa520", "gray": "#808080",
	"grey": "#808080", "green": "#008000", "greenyellow": "#adff2f",
	"honeydew": "#f0fff0", "hotpink": "#ff69b4", "indianred": "#cd5c5c",
	"indigo": "#4b0082", "ivory": "#fffff0", "khaki": "#f0e68c",
	"lavender": "#e6e6fa", "lavenderblush": "#fff0f5", "lawngreen": "#7cfc00",
	"lemonchiffon": "#fffacd", "lightblue": "#add8e6", "lightcoral": "#f08080",
	"lightcyan": "#e0ffff", "lightgoldenrodyellow": "#fafad2", "lightgray": "#d3d3d3",
	"lightgrey": "#d3d3d3", "lightgreen": "#90ee90", "lightpink": "#ffb6c1",
	"lightsalmon": "#ffa07a", "lightseagreen": "#20b2aa", "lightskyblue": "#87cefa",
	"lightslategray": "#778899", "lightslategrey": "#778899", "lightsteelblue": "#b0c4de",
	"lightyellow": "#ffffe0", "lime": "#00ff00", "limegreen": "#32cd32",
	"linen": "#faf0e6", "magenta": "#ff00ff", "maroon": "#800000",
	"mediumaquamarine": "#66cdaa", "mediumblue": "#0000cd", "mediumorchid": "#ba55d3",
	"mediumpurple": "#9370db", "mediumseagreen": "#3cb371", "mediumslateblue": "#7b68ee",
	"mediumspringgreen": "#00fa9a", "mediumturquoise": "#48d1cc", "mediumvioletred": "#c71585",
	"midnightblue": "#191970", "mintcream": "#f5fffa", "mistyrose": "#ffe4e1",
	"moccasin": "#ffe4b5", "navajowhite": "#ffdead", "navy": "#000080",
	"oldlace": "#fdf5e6", "olive": "#808000", "olivedrab": "#6b8e23",
	"orange": "#ffa500", "orangered": "#ff4500", "orchid": "#da70d6",
	"palegoldenrod": "#eee8aa", "palegreen": "#98fb98", "paleturquoise": "#afeeee",
	"palevioletred": "#db7093", "papayawhip": "#ffefd5", "peachpuff": "#ffdab9",
	"peru": "#cd853f", "pink": "#ffc0cb", "plum": "#dda0dd",
	"powderblue": "#b0e0e6", "purple": "#800080", "rebeccapurple": "#663399",
	"red": "#ff0000", "rosybrown": "#bc8f8f", "royalblue": "#4169e1",
	"saddlebrown": "#8b4513", "salmon": "#fa8072", "sandybrown": "#f4a460",
	"seagreen": "#2e8b57", "seashell": "#fff5ee", "sienna": "#a0522d",
	"silver": "#c0c0c0", "skyblue": "#87ceeb", "slateblue": "#6a5acd",
	"slategray": "#708090", "slategrey": "#708090", "snow": "#fffafa",
	"springgreen": "#00ff7f", "steelblue": "#4682b4", "tan": "#d2b48c",
	"teal": "#008080", "thistle": "#d8bfd8", "tomato": "#ff6347",
	"turquoise": "#40e0d0", "violet": "#ee82ee", "wheat": "#f5deb3",
	"white": "#ffffff", "whitesmoke": "#f5f5f5", "yellow": "#ffff00",
	"yellowgreen": "#9acd32",
}

// parseFuncColor parses the CSS functional colour forms. The bool reports
// whether the string looked like one at all (so other forms still get their
// turn).
func parseFuncColor(s string) (RGBA, bool, error) {
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return RGBA{}, false, nil
	}
	fn := strings.ToLower(strings.TrimSpace(s[:open]))
	switch fn {
	case "rgb", "rgba", "hsl", "hsla":
	default:
		return RGBA{}, false, nil
	}
	body := s[open+1 : len(s)-1]
	// CSS allows commas or spaces (and a slashed alpha); treat all as
	// separators.
	body = strings.NewReplacer(",", " ", "/", " ").Replace(body)
	parts := strings.Fields(body)
	if len(parts) < 3 {
		return RGBA{}, true, fmt.Errorf("bad color %q", s)
	}
	num := func(i int, scale float32) (float32, error) {
		p := parts[i]
		pct := strings.HasSuffix(p, "%")
		f, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 32)
		if err != nil {
			return 0, fmt.Errorf("bad color %q", s)
		}
		v := float32(f)
		if pct {
			return v / 100, nil
		}
		if scale > 0 {
			return v / scale, nil
		}
		return v, nil
	}
	a := float32(1)
	if len(parts) >= 4 {
		v, err := num(3, 0)
		if err != nil {
			return RGBA{}, true, err
		}
		// An alpha above 1 is the 0..255 form.
		if v > 1 {
			v /= 255
		}
		a = clamp01(v)
	}
	if fn == "rgb" || fn == "rgba" {
		r, err := num(0, 255)
		if err != nil {
			return RGBA{}, true, err
		}
		g, err := num(1, 255)
		if err != nil {
			return RGBA{}, true, err
		}
		b, err := num(2, 255)
		if err != nil {
			return RGBA{}, true, err
		}
		return RGBA{R: clamp01(r), G: clamp01(g), B: clamp01(b), A: a}, true, nil
	}
	h, err := num(0, 0) // degrees
	if err != nil {
		return RGBA{}, true, err
	}
	sat, err := num(1, 0)
	if err != nil {
		return RGBA{}, true, err
	}
	l, err := num(2, 0)
	if err != nil {
		return RGBA{}, true, err
	}
	r, g, b := hslToRGB(h, clamp01(sat), clamp01(l))
	return RGBA{R: r, G: g, B: b, A: a}, true, nil
}

func clamp01(f float32) float32 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// hslToRGB converts CSS HSL (hue in degrees) to RGB.
func hslToRGB(h, s, l float32) (float32, float32, float32) {
	h = float32(int(h)%360) + (h - float32(int(h)))
	if h < 0 {
		h += 360
	}
	c := (1 - absF(2*l-1)) * s
	hp := h / 60
	x := c * (1 - absF(float32(int(hp)%2)+(hp-float32(int(hp)))-1))
	var r, g, b float32
	switch int(hp) {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	return clamp01(r + m), clamp01(g + m), clamp01(b + m)
}

func absF(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
