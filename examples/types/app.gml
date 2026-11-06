// Phase E: the type system & cross-object references. A `color` property
// drives several rectangles; a `var` holds a parsed JSON object read with
// field access; and `id.property` reads/writes another element's declared
// property. Edit live; it hot-reloads on save.
import QtQuick 2.15

Column {
    padding: 20; spacing: 14
    color: "#131318"
    property color accent: "#7048e8"
    property var meta: JSON.parse("{\"name\": \"swatch\", \"version\": 2}")
    // alias exposes a nested element's property under a local name.
    property alias taps: chip.hits

    Text { text: "Phase E: types & cross-object refs"; color: "#f1f1f7" }

    // The color-typed property drives two rectangles; the button cycles it.
    Row {
        spacing: 10; items: "center"
        Rectangle { width: 64; height: 40; radius: 8; color: accent }
        Rectangle { id: chip; width: 64; height: 40; radius: 8; color: accent; property int hits: 0 }
        Button {
            text: "cycle"; width: 90
            onClicked: { if (accent == "#7048e8") accent = "#e64980"; else accent = "#7048e8" }
        }
    }

    // id.property: read and write another element's declared property.
    Row {
        spacing: 10; items: "center"
        Button { text: "tap chip"; width: 110; onClicked: chip.hits = chip.hits + 1 }
        Text { text: "chip.hits = " + chip.hits; color: "#91a7ff" }
    }

    Rectangle {
        padding: 16; spacing: 6
        color: "#1d1d27"; radius: 12
        border.width: 1; border.color: "#32323f"
        // var holding a JSON object — field access via meta.name / meta.version.
        Text { text: "accent  = " + accent; color: "#69db7c" }
        Text { text: "meta    = " + meta.name + " v" + meta.version; color: "#ffd43b" }
        // `taps` is an alias to chip.hits; color uses the Qt enum namespace.
        Text { text: "taps    = " + taps; color: Qt.red }
    }
}
