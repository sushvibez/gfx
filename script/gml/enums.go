package gml

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/sushvibez/gfx/ui"
)

// QML enumerations as namespace objects. Real .qml files reference scoped
// enum values (Text.AlignHCenter, Image.PreserveAspectFit, Font.Bold, Qt.…);
// these register the common ones as constants so such expressions evaluate
// instead of erroring. Values mirror Qt's. Some are accepted but not yet
// consumed by the renderer (e.g. Text alignment) — they still type/evaluate.
//
// The namespaces are stored under the same names as elements (Text, Image,
// ListView); that only affects expression evaluation, never markup structure
// (which dispatches on the element's type name), so there is no conflict.
// Qt::CursorShape values (real Qt enum numbering, so any expression using the
// raw int instead of the named constant also matches).
const (
	qtCursorArrow        = float64(0)
	qtCursorIBeam        = float64(1)
	qtCursorCross        = float64(2)
	qtCursorPointingHand = float64(13)
	qtCursorSplitH       = float64(15)
	qtCursorSplitV       = float64(16)

	// Text.renderType. Qt numbers QtRendering 0 and NativeRendering 1;
	// QtRendering is the distance field on both sides.
	qtRenderingText = float64(0)
)

func registerEnums(vars map[string]any) {
	// Qt::AlignmentFlag bits, shared by Text and Qt.
	align := map[string]any{
		"AlignLeft":    float64(0x0001),
		"AlignRight":   float64(0x0002),
		"AlignHCenter": float64(0x0004),
		"AlignJustify": float64(0x0008),
		"AlignTop":     float64(0x0020),
		"AlignBottom":  float64(0x0040),
		"AlignVCenter": float64(0x0080),
		"AlignCenter":  float64(0x0084), // HCenter|VCenter
	}

	textNS := map[string]any{
		// wrapMode
		"NoWrap":       float64(0),
		"WordWrap":     float64(1),
		"WrapAnywhere": float64(3),
		"Wrap":         float64(4),
		// textFormat
		"PlainText":    float64(0),
		"RichText":     float64(1),
		"AutoText":     float64(2),
		"StyledText":   float64(4),
		"MarkdownText": float64(3), // Qt::MarkdownText; it was 8, and no Markdown text ever rendered as Markdown
		// renderType (Qt's own numbering)
		"QtRendering":     qtRenderingText,
		"NativeRendering": float64(1),
		// elide
		"ElideNone":   float64(0),
		"ElideLeft":   float64(1),
		"ElideMiddle": float64(2),
		"ElideRight":  float64(3),
	}
	mergeEnum(textNS, align)

	imageNS := map[string]any{
		// fillMode
		"Stretch":            float64(0),
		"PreserveAspectFit":  float64(1),
		"PreserveAspectCrop": float64(2),
		"Tile":               float64(3),
		"TileVertically":     float64(4),
		"TileHorizontally":   float64(5),
		"Pad":                float64(6),
		// status
		"Null":    float64(0),
		"Ready":   float64(1),
		"Loading": float64(2),
		"Error":   float64(3),
	}

	fontNS := map[string]any{
		"Thin":       float64(0),
		"ExtraLight": float64(12),
		"Light":      float64(25),
		"Normal":     float64(50),
		"Medium":     float64(57),
		"DemiBold":   float64(63),
		"Bold":       float64(75),
		"ExtraBold":  float64(81),
		"Black":      float64(87),
	}

	listViewNS := map[string]any{
		// Qt's values (Qt.Horizontal / Qt.Vertical), so either spelling
		// of an orientation compares equal.
		"Horizontal": float64(1),
		"Vertical":   float64(2),
		// verticalLayoutDirection / layoutDirection.
		"TopToBottom": float64(0),
		"BottomToTop": float64(1),
		"LeftToRight": float64(0),
		"RightToLeft": float64(1),
	}

	// TableView selection/edit enums (values mirror Qt's; consumed as no-ops
	// today but they must evaluate). Flickable.StopAtBounds likewise.
	tableViewNS := map[string]any{
		"SelectionDisabled": float64(0), "SelectCells": float64(1),
		"SelectRows": float64(2), "SelectColumns": float64(3),
		"SingleSelection": float64(1), "ContiguousSelection": float64(4),
		"ExtendedSelection": float64(3), "MultiSelection": float64(2),
		"NoEditTriggers": float64(0), "SingleTapped": float64(1),
		"DoubleTapped": float64(2), "SelectedTapped": float64(4),
		"EditKeyPressed": float64(8), "AnyKeyPressed": float64(16),
	}
	flickableNS := map[string]any{
		"StopAtBounds": float64(0), "DragOverBounds": float64(1),
		"OvershootBounds": float64(2), "DragAndOvershootBounds": float64(3),
	}
	itemSelectionNS := map[string]any{
		"NoUpdate": float64(0), "Clear": float64(1), "Select": float64(2),
		"Deselect": float64(4), "Toggle": float64(8), "Current": float64(16),
		"ClearAndSelect": float64(3), "SelectCurrent": float64(18),
		"Rows": float64(32), "Columns": float64(64),
	}

	qtNS := map[string]any{
		"Horizontal": float64(1),
		"Vertical":   float64(2),
		// Qt::Edge — a Drawer's edge, and the ApplicationWindow edges.
		"TopEdge":    float64(1),
		"LeftEdge":   float64(2),
		"RightEdge":  float64(4),
		"BottomEdge": float64(8),
		// Qt::Orientation is above; Qt::SortOrder next.
		// Qt::SortOrder
		"AscendingOrder":  float64(0),
		"DescendingOrder": float64(1),
		// Qt::CheckState (tristate checkboxes)
		"Unchecked":        float64(0),
		"PartiallyChecked": float64(1),
		"Checked":          float64(2),
		// a few named colors (our colors are hex strings)
		"black":       "#000000",
		"white":       "#ffffff",
		"red":         "#ff0000",
		"green":       "#00ff00",
		"blue":        "#0000ff",
		"yellow":      "#ffff00",
		"transparent": "#00000000",
		// Qt::CursorShape (values match Qt's own enum; only Pointing/IBeam map
		// to a real app.Cursor today — see cursorFromQt — the rest evaluate
		// but fall back to the default arrow).
		"ArrowCursor":        qtCursorArrow,
		"IBeamCursor":        qtCursorIBeam,
		"CrossCursor":        qtCursorCross,
		"PointingHandCursor": qtCursorPointingHand,
		"SplitHCursor":       qtCursorSplitH,
		"SplitVCursor":       qtCursorSplitV,
	}
	mergeEnum(qtNS, align)
	registerQtFuncs(qtNS)
	for k, code := range qtKeyCodes { // Qt.Key_Left etc. for Keys handlers
		qtNS[k] = code
	}
	for k, bit := range qtModifiers { // Qt.ShiftModifier etc., for event.modifiers
		if _, taken := qtNS[k]; !taken {
			qtNS[k] = bit
		}
	}

	// Animation.Infinite for loops; Easing.* names (string-valued so
	// easingByName resolves them) for easing.type.
	animationNS := map[string]any{"Infinite": float64(-1)}
	easingNames := []string{
		"Linear", "InQuad", "OutQuad", "InOutQuad", "InCubic", "OutCubic",
		"InOutCubic", "InQuart", "OutQuart", "InOutQuart", "InSine", "OutSine",
		"InOutSine", "InExpo", "OutExpo", "InOutExpo", "InBack", "OutBack",
		"InOutBack", "InBounce", "OutBounce", "InOutBounce", "InElastic",
		"OutElastic", "InOutElastic",
	}
	easingNS := map[string]any{}
	for _, n := range easingNames {
		easingNS[n] = n
	}

	// PathView highlight-range and attached-property fallbacks (delegates
	// get a live attached map from the row scope; this covers reads outside
	// delegates so expressions still evaluate).
	vars["PathView"] = map[string]any{
		"NoHighlightRange": float64(0), "ApplyRange": float64(1),
		"StrictlyEnforceRange": float64(2),
		"isCurrentItem":        false, "onPath": true,
	}
	// Gradient.orientation (QtQuick): Vertical is the default.
	vars["Gradient"] = map[string]any{"Vertical": float64(0), "Horizontal": float64(1)}
	// QtQuick.Shapes' ShapePath enums. fillRule decides which side of a
	// self-crossing outline is inside; the caps and joins are read where
	// the stroke is built.
	vars["ShapePath"] = map[string]any{
		"OddEvenFill": float64(0), "WindingFill": float64(1),
		// strokeStyle takes Qt::PenStyle values.
		"SolidLine": float64(1), "DashLine": float64(2), "DotLine": float64(3),
		"DashDotLine": float64(4), "DashDotDotLine": float64(5),
		"FlatCap": float64(0), "SquareCap": float64(16), "RoundCap": float64(32),
		"MiterJoin": float64(0), "BevelJoin": float64(64), "RoundJoin": float64(128),
	}
	// QtQuick3D material enums. `lighting: DefaultMaterial.NoLighting`
	// draws a model in FLAT colour — the shading is skipped, which is how
	// a 3D page states a solid swatch. Unregistered, that expression
	// failed to evaluate and the model came out shaded.
	materialNS := namedEnum([]string{
		"NoLighting", "FragmentLighting", "VertexLighting",
		"SourceOver", "Screen", "Multiply", "Overlay", "ColorBurn", "ColorDodge",
		"Default", "KGGX", "DefaultSpecular",
		"NoCulling", "BackFaceCulling", "FrontFaceCulling",
		"Opaque", "Blend", "Mask",
	})
	vars["DefaultMaterial"] = materialNS
	vars["PrincipledMaterial"] = materialNS
	vars["SpecularGlossyMaterial"] = materialNS
	vars["Material"] = materialNS
	// Shape's rendering hints, accepted wherever they appear.
	vars["Shape"] = map[string]any{
		"GeometryRenderer": float64(0), "CurveRenderer": float64(1),
		"ApplyRandomization": float64(0),
	}
	// QtGraphs (Graphs3D/GraphsTheme/AbstractSeries and the 3D series
	// types). The charts read cameraPreset by name and accept the rest, so
	// these exist to make the scoped references evaluate.
	vars["Graphs3D"] = map[string]any{
		"CameraPreset":     namedEnum(graphs3DCameraPresets),
		"ShadowQuality":    namedEnum([]string{"None", "Low", "Medium", "High", "SoftLow", "SoftMedium", "SoftHigh"}),
		"ElementType":      namedEnum([]string{"None", "Series", "AxisXLabel", "AxisYLabel", "AxisZLabel", "CustomItem"}),
		"SelectionFlag":    namedEnum([]string{"None", "Item", "Row", "Column", "Slice", "MultiSeries"}),
		"OptimizationHint": namedEnum([]string{"Default", "Legacy"}),
		"GridLineType":     namedEnum([]string{"Shader", "Geometry"}),
		"Theme":            namedEnum([]string{"QtGreen", "QtGreenNeon", "MixSeries", "OrangeSeries", "YellowSeries", "BlueSeries", "PurpleSeries", "GreySeries", "UserDefined"}),
	}
	graphsThemeNS := map[string]any{
		"ColorScheme": namedEnum([]string{"Automatic", "Light", "Dark"}),
		"Theme":       namedEnum([]string{"QtGreen", "QtGreenNeon", "MixSeries", "OrangeSeries", "YellowSeries", "BlueSeries", "PurpleSeries", "GreySeries", "UserDefined"}),
		"ColorStyle":  namedEnum([]string{"Uniform", "ObjectGradient", "RangeGradient"}),
	}
	vars["GraphsTheme"] = graphsThemeNS
	// Surface3DSeries.DrawSurface & co are flags, not scoped values.
	vars["Surface3DSeries"] = map[string]any{
		"DrawWireframe": float64(1), "DrawSurface": float64(2),
		"DrawSurfaceAndWireframe": float64(3),
	}
	vars["Bar3DSeries"] = map[string]any{"MeshBar": float64(0)}
	vars["Scatter3DSeries"] = map[string]any{
		"MeshSphere": float64(9), "MeshCube": float64(4), "MeshPoint": float64(1),
	}
	vars["Abstract3DSeries"] = map[string]any{
		"MeshUserDefined": float64(0), "MeshBar": float64(1), "MeshCube": float64(2),
		"MeshPyramid": float64(3), "MeshCone": float64(4), "MeshCylinder": float64(5),
		"MeshBevelBar": float64(6), "MeshBevelCube": float64(7), "MeshSphere": float64(8),
		"MeshMinimal": float64(9), "MeshArrow": float64(10), "MeshPoint": float64(11),
	}
	vars["MediaPlayer"] = mediaPlayerNS()
	vars["QtPositioning"] = qtPositioningNS()
	vars["PdfDocument"] = pdfDocumentNS()
	// Item.<TransformOrigin>: the nine anchor points QML's transformOrigin
	// takes. They evaluate to their own names, which is what
	// transformOriginFrac reads.
	vars["Item"] = namedEnum([]string{
		"TopLeft", "Top", "TopRight", "Left", "Center", "Right",
		"BottomLeft", "Bottom", "BottomRight",
	})
	vars["Text"] = textNS
	vars["Image"] = imageNS
	// Loader.status values, which a page compares against in
	// onStatusChanged (`status === Loader.Ready`).
	vars["Loader"] = map[string]any{
		"Null": float64(0), "Ready": float64(1),
		"Loading": float64(2), "Error": float64(3),
	}
	// BorderImage.TileMode, alongside the Image fill modes it shares.
	borderImageNS := map[string]any{
		"Stretch": float64(0), "Repeat": float64(1), "Round": float64(2),
	}
	mergeEnum(borderImageNS, imageNS)
	vars["BorderImage"] = borderImageNS
	vars["AnimatedImage"] = imageNS
	vars["Font"] = fontNS
	vars["ListView"] = listViewNS
	vars["TableView"] = tableViewNS
	vars["Flickable"] = flickableNS
	vars["ItemSelectionModel"] = itemSelectionNS
	vars["Qt"] = qtNS
	vars["Animation"] = animationNS
	// TextInput/TextEdit: the echo modes a password field states, plus the
	// Text alignment/wrap members these items share.
	textInputNS := namedEnum([]string{
		"Normal", "Password", "NoEcho", "PasswordEchoOnEdit",
		"SelectCharacters", "SelectWords", "SelectAll",
	})
	mergeEnum(textInputNS, textNS)
	// Slider/RangeSlider::SnapMode — whether a drag is held to stepSize.
	snapNS := namedEnum([]string{"NoSnap", "SnapAlways", "SnapOnRelease"})
	vars["Slider"] = snapNS
	vars["RangeSlider"] = snapNS
	vars["Dial"] = snapNS
	vars["TextInput"] = textInputNS
	vars["TextEdit"] = textInputNS
	vars["Easing"] = easingNS
	// QDialogButtonBox::StandardButton — a Dialog's `standardButtons` is an
	// OR of these, and the numbering is Qt's so a raw int matches too.
	dialogNS := map[string]any{
		"Ok": float64(0x00000400), "Save": float64(0x00000800),
		"SaveAll": float64(0x00001000), "Open": float64(0x00002000),
		"Yes": float64(0x00004000), "YesToAll": float64(0x00008000),
		"No": float64(0x00010000), "NoToAll": float64(0x00020000),
		"Abort": float64(0x00040000), "Retry": float64(0x00080000),
		"Ignore": float64(0x00100000), "Close": float64(0x00200000),
		"Cancel": float64(0x00400000), "Discard": float64(0x00800000),
		"Help": float64(0x01000000), "Apply": float64(0x02000000),
		"Reset": float64(0x04000000), "RestoreDefaults": float64(0x08000000),
		"NoButton": float64(0),
	}
	// Popup::ClosePolicy, which a Dialog inherits (and its style states).
	popupNS := map[string]any{
		"NoAutoClose": float64(0x00), "CloseOnPressOutside": float64(0x01),
		"CloseOnPressOutsideParent": float64(0x02), "CloseOnReleaseOutside": float64(0x04),
		"CloseOnReleaseOutsideParent": float64(0x08), "CloseOnEscape": float64(0x10),
	}
	mergeEnum(dialogNS, popupNS)
	vars["Popup"] = popupNS
	vars["Dialog"] = dialogNS
	vars["DialogButtonBox"] = dialogNS
}

// graphs3DCameraPresets are Graphs3D.CameraPreset's members, in Qt's order.
var graphs3DCameraPresets = []string{
	"NoPreset", "FrontLow", "Front", "FrontHigh", "LeftLow", "Left", "LeftHigh",
	"RightLow", "Right", "RightHigh", "BehindLow", "Behind", "BehindHigh",
	"IsometricLeft", "IsometricLeftHigh", "IsometricRight", "IsometricRightHigh",
	"DirectlyAbove", "DirectlyAboveCW45", "DirectlyAboveCCW45", "FrontBelow",
	"LeftBelow", "RightBelow", "BehindBelow", "DirectlyBelow",
}

// namedEnum makes a namespace whose members evaluate to their own name, so
// code that only needs to distinguish them (or print them) works and the
// numeric ordinal is not invented.
func namedEnum(names []string) map[string]any {
	ns := make(map[string]any, len(names))
	for _, n := range names {
		ns[n] = n
	}
	return ns
}

func mergeEnum(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}

// registerQtFuncs adds Qt global color factory methods. Colors are hex strings
// (#aarrggbb) in this engine, matching parseColor's convention.
func registerQtFuncs(qt map[string]any) {
	clamp01 := func(f float64) int {
		// NaN comes from a component ratio computed before layout
		// (redPanel.width / colorSplit.width on the first frame): Qt clamps
		// an invalid component to 0 rather than producing a bad colour.
		if f < 0 || math.IsNaN(f) {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		return int(f*255 + 0.5)
	}
	// A colour string in QML is #AARRGGBB — alpha first, as parseColor
	// reads it.
	hex := func(r, g, b, a float64) string {
		return fmt.Sprintf("#%02x%02x%02x%02x", clamp01(a), clamp01(r), clamp01(g), clamp01(b))
	}
	argf := func(a []any, i int) float64 {
		if i < len(a) {
			if f, err := toFloat64(a[i]); err == nil {
				return f
			}
		}
		return 0
	}
	qt["resolvedUrl"] = jsFunc(func(a []any) (any, error) {
		// URLs stay strings here; path resolution happens where they're
		// consumed (resolveImagePath walks the document/imports tree).
		if len(a) == 0 {
			return "", nil
		}
		return str(a[0]), nil
	})
	qt["formatTime"] = jsFunc(func(a []any) (any, error) {
		layout := "15:04:05"
		if len(a) >= 2 {
			layout = qtTimeLayout(str(a[1]))
		}
		return argTime(a, 0).Format(layout), nil
	})
	qt["formatDateTime"] = jsFunc(func(a []any) (any, error) {
		layout := "2006-01-02 15:04:05"
		if len(a) >= 2 {
			layout = qtTimeLayout(str(a[1]))
		}
		return argTime(a, 0).Format(layout), nil
	})
	qt["formatDate"] = jsFunc(func(a []any) (any, error) {
		layout := "2006-01-02"
		if len(a) >= 2 {
			layout = qtTimeLayout(str(a[1]))
		}
		return argTime(a, 0).Format(layout), nil
	})
	// Qt.callLater(fn, args...): run fn once the current pass is done. The
	// posted queue drains at the top of the next frame, which is exactly
	// "after everything settles" here — these calls sync a highlight to
	// geometry that only exists once the layout has run.
	qt["callLater"] = jsFunc(func(a []any) (any, error) {
		if len(a) == 0 {
			return nil, nil
		}
		fn, args := a[0], append([]any{}, a[1:]...)
		Post(func() {
			if _, err := callAny(fn, args); err != nil {
				logf("gml: Qt.callLater: %v", err)
			}
		})
		return nil, nil
	})
	// Qt.vector2d/3d/4d and Qt.size/point/rect: value types QML builds
	// inline. They are plain objects here, read by component
	// (`position.x`, `size.width`).
	qt["vector2d"] = jsFunc(func(a []any) (any, error) {
		return map[string]any{"x": argf(a, 0), "y": argf(a, 1)}, nil
	})
	qt["vector3d"] = jsFunc(func(a []any) (any, error) {
		return map[string]any{"x": argf(a, 0), "y": argf(a, 1), "z": argf(a, 2)}, nil
	})
	qt["vector4d"] = jsFunc(func(a []any) (any, error) {
		return map[string]any{"x": argf(a, 0), "y": argf(a, 1),
			"z": argf(a, 2), "w": argf(a, 3)}, nil
	})
	qt["size"] = jsFunc(func(a []any) (any, error) {
		return map[string]any{"width": argf(a, 0), "height": argf(a, 1)}, nil
	})
	qt["point"] = jsFunc(func(a []any) (any, error) {
		return map[string]any{"x": argf(a, 0), "y": argf(a, 1)}, nil
	})
	qt["rect"] = jsFunc(func(a []any) (any, error) {
		return map[string]any{"x": argf(a, 0), "y": argf(a, 1),
			"width": argf(a, 2), "height": argf(a, 3)}, nil
	})
	// Qt.locale(name): the locale object pages hand to toLocaleString and
	// to a Calendar. Formatting here is en-US-like whatever the name says —
	// enough for the pages, honest about the rest.
	qt["locale"] = jsFunc(func(a []any) (any, error) {
		name := strArg(a, 0)
		if name == "" {
			name = "C"
		}
		l := localeFor(name)
		return map[string]any{
			"name": name, "decimalPoint": l.dec, "groupSeparator": l.group,
			"currencySymbol": l.currency, "amText": l.am, "pmText": l.pm,
			"nativeLanguageName": l.language,
		}, nil
	})
	qt["rgba"] = jsFunc(func(a []any) (any, error) {
		return colorVal(hex(argf(a, 0), argf(a, 1), argf(a, 2), argf(a, 3))), nil
	})
	// Qt.color(name) turns a string into a COLOUR — which is how markup
	// gets at the components of a colour held as text
	// (`let c = Qt.color(Style.mainColor); Qt.rgba(c.r, c.g, c.b, 0.1)`,
	// the corpus' own workaround for the string/colour distinction).
	qt["color"] = jsFunc(func(a []any) (any, error) {
		s := strArg(a, 0)
		if _, err := parseColor(s); err != nil {
			return colorVal("#000000"), nil // Qt returns an invalid colour; keep it renderable
		}
		return colorVal(s), nil
	})
	qt["rgb"] = jsFunc(func(a []any) (any, error) {
		return colorVal(hex(argf(a, 0), argf(a, 1), argf(a, 2), 1)), nil
	})
	qt["hsla"] = jsFunc(func(a []any) (any, error) {
		r, g, b := hslToRGB(argf(a, 0), argf(a, 1), argf(a, 2))
		return colorVal(hex(r, g, b, argfDefault(a, 3, 1))), nil
	})
	qt["hsva"] = jsFunc(func(a []any) (any, error) {
		r, g, b := hsvToRGB(argf(a, 0), argf(a, 1), argf(a, 2))
		return colorVal(hex(r, g, b, argfDefault(a, 3, 1))), nil
	})
	// lighter/darker scale value (HSV V) by a factor (default 1.5 / 2.0).
	qt["lighter"] = jsFunc(func(a []any) (any, error) {
		return colorVal(scaleColor(strArg(a, 0), factorArg(a, 1, 1.5))), nil
	})
	qt["darker"] = jsFunc(func(a []any) (any, error) {
		return colorVal(scaleColor(strArg(a, 0), 1/factorArg(a, 1, 2.0))), nil
	})
}

func argfDefault(a []any, i int, def float64) float64 {
	if i < len(a) {
		if f, err := toFloat64(a[i]); err == nil {
			return f
		}
	}
	return def
}

func strArg(a []any, i int) string {
	if i < len(a) {
		return str(a[i])
	}
	return ""
}

func factorArg(a []any, i int, def float64) float64 {
	if i < len(a) {
		if f, err := toFloat64(a[i]); err == nil && f > 0 {
			return f
		}
	}
	return def
}

// colorHex writes a colour the way this engine reads them — Qt's
// #AARRGGBB, alpha FIRST.
func colorHex(c ui.RGBA) string {
	cl := func(v float32) int {
		x := int(float64(v)*255 + 0.5)
		return min(max(x, 0), 255)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", cl(c.A), cl(c.R), cl(c.G), cl(c.B))
}

// scaleColor multiplies a color's brightness by f (clamped), preserving alpha.
// scaleColor is Qt.lighter/Qt.darker: QColor::lighter scales the HSV VALUE
// by the factor and, when that overflows, takes the excess off the
// SATURATION — which is what turns a bright colour whitish rather than just
// clipping its channels. Scaling RGB instead kept a lightened teal fully
// saturated (#00FFFF), where Qt gives a pale one; the sprite-stack page's
// lit slices were cyan against Qt's near-white.
func scaleColor(str string, f float64) string {
	c, err := parseColor(str)
	if err != nil {
		return str
	}
	h, sat, v := rgbToHSV(float64(c.R), float64(c.G), float64(c.B))
	switch {
	case f <= 0:
		// no change
	case f < 1:
		v *= f // darker
	default:
		v *= f
		if v > 1 {
			sat -= v - 1 // Qt: the overflow comes off the saturation
			if sat < 0 {
				sat = 0
			}
			v = 1
		}
	}
	r, g, b := hsvToRGB(h, sat, v)
	cl := func(x float64) int {
		n := int(x*255 + 0.5)
		if n < 0 {
			n = 0
		}
		if n > 255 {
			n = 255
		}
		return n
	}
	// Qt's 8-digit hex is #AARRGGBB — alpha FIRST. Writing the channels in
	// RGBA order here made every lightened colour transparent (a teal's
	// zero red channel became its alpha), which is what hid the background
	// of every hovered, pressed and highlighted button.
	return fmt.Sprintf("#%02x%02x%02x%02x", int(c.A*255+0.5), cl(r), cl(g), cl(b))
}

// rgbToHSV is the standard conversion, in 0..1 for every component.
func rgbToHSV(r, g, b float64) (h, s, v float64) {
	mx := math.Max(r, math.Max(g, b))
	mn := math.Min(r, math.Min(g, b))
	v = mx
	d := mx - mn
	if mx > 0 {
		s = d / mx
	}
	if d == 0 {
		return 0, s, v
	}
	switch mx {
	case r:
		h = (g - b) / d / 6
	case g:
		h = (2 + (b-r)/d) / 6
	default:
		h = (4 + (r-g)/d) / 6
	}
	if h < 0 {
		h++
	}
	return h, s, v
}

func hslToRGB(h, s, l float64) (float64, float64, float64) {
	if s == 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hk := func(t float64) float64 {
		if t < 0 {
			t += 1
		}
		if t > 1 {
			t -= 1
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 1.0/2:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return hk(h + 1.0/3), hk(h), hk(h - 1.0/3)
}

func hsvToRGB(h, s, v float64) (float64, float64, float64) {
	if s == 0 {
		return v, v, v
	}
	h *= 6
	i := int(h)
	f := h - float64(i)
	p := v * (1 - s)
	q := v * (1 - s*f)
	t := v * (1 - s*(1-f))
	switch i % 6 {
	case 0:
		return v, t, p
	case 1:
		return q, v, p
	case 2:
		return p, v, t
	case 3:
		return p, q, v
	case 4:
		return t, p, v
	}
	return v, p, q
}

// timeNow is stubbed in tests for deterministic formatting.
var timeNow = time.Now

// qtTimeLayout converts a Qt date/time format (hh:mm:ss, yyyy-MM-dd) to a
// Go layout — the common tokens only.
func qtTimeLayout(f string) string {
	r := strings.NewReplacer(
		"yyyy", "2006", "yy", "06",
		"MM", "01", "dd", "02",
		"hh", "15", "HH", "15",
		"mm", "04", "ss", "05", "zzz", "000",
		"AP", "PM", "ap", "pm",
	)
	return r.Replace(f)
}

// argTime reads a date argument — a `new Date()` object or epoch
// milliseconds — falling back to now, which is what Qt.formatTime() with no
// argument means.
func argTime(args []any, i int) time.Time {
	if i >= len(args) {
		return timeNow()
	}
	switch t := args[i].(type) {
	case map[string]any:
		if ms, err := toFloat64(t["time"]); err == nil {
			return time.UnixMilli(int64(ms))
		}
	default:
		if ms, err := toFloat64(args[i]); err == nil && ms > 0 {
			return time.UnixMilli(int64(ms))
		}
	}
	return timeNow()
}

// localeData is the part of a QLocale that markup reads through
// Qt.locale(name): separators, currency and the AM/PM texts.
type localeData struct{ dec, group, currency, am, pm, language string }

// locales holds the common ones by name; localeFor falls back to the
// language alone ("de_AT" → "de") and then to en_US, Qt's "C" shape.
var locales = map[string]localeData{
	"en_US": {".", ",", "$", "AM", "PM", "American English"},
	"en_GB": {".", ",", "£", "am", "pm", "British English"},
	"en_IN": {".", ",", "₹", "am", "pm", "Indian English"},
	"de":    {",", ".", "€", "AM", "PM", "Deutsch"},
	"de_CH": {".", "’", "CHF", "AM", "PM", "Schweizer Hochdeutsch"},
	"fr":    {",", "\u202f", "€", "AM", "PM", "français"},
	"es":    {",", ".", "€", "a. m.", "p. m.", "español"},
	"it":    {",", ".", "€", "AM", "PM", "italiano"},
	"nl":    {",", ".", "€", "a.m.", "p.m.", "Nederlands"},
	"pt":    {",", ".", "€", "da manhã", "da tarde", "português"},
	"pt_BR": {",", ".", "R$", "AM", "PM", "português"},
	"sv":    {",", "\u00a0", "kr", "fm", "em", "svenska"},
	"ru":    {",", "\u00a0", "₽", "AM", "PM", "русский"},
	"pl":    {",", "\u00a0", "zł", "AM", "PM", "polski"},
	"ja":    {".", ",", "￥", "午前", "午後", "日本語"},
	"zh":    {".", ",", "¥", "上午", "下午", "中文"},
	"ko":    {".", ",", "₩", "오전", "오후", "한국어"},
	"hi":    {".", ",", "₹", "am", "pm", "हिन्दी"},
}

func localeFor(name string) localeData {
	name = strings.ReplaceAll(name, "-", "_")
	if l, ok := locales[name]; ok {
		return l
	}
	lang, _, _ := strings.Cut(name, "_")
	if l, ok := locales[lang]; ok {
		return l
	}
	return locales["en_US"]
}
