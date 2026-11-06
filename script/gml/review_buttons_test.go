package gml

import (
	"testing"

	"github.com/sushvibez/gfx/app"
	"github.com/sushvibez/gfx/ui"
)

// The buttons page review (QML-EXAMPLES.md item 88): two interaction gaps
// the side-by-side run found.

func walkTree(e ui.Element, visit func(ui.Element)) {
	if e == nil {
		return
	}
	visit(e)
	for _, c := range ui.Children(e) {
		walkTree(c, visit)
	}
}

// A style computes its background colour in a `readonly property color`
// block that reads root.pressed / root.hovered, and the Rectangle binds to
// that property — the corpus' Button.qml. The colour has to follow the
// press like a direct `root.down ? a : b` binding does.
func TestStyledButtonDerivedColourFollowsThePress(t *testing.T) {
	button := `
import QtQuick
import QtQuick.Templates as T
import QtQuick.Effects
T.Button {
    id: root
    implicitWidth: 120
    implicitHeight: 40
    palette.button: "#00ff00"
    palette.buttonText: "#ffffff"
    readonly property color __bgColor: {
        if (!root.enabled)
            return "#555555"
        if (root.flat) {
            if (root.pressed)
                return Qt.rgba(root.palette.button.r, root.palette.button.g, root.palette.button.b, 0.2)
            if (root.hovered)
                return Qt.rgba(root.palette.button.r, root.palette.button.g, root.palette.button.b, 0.1)
            return "transparent"
        }
        if (root.pressed)
            return Qt.darker(root.palette.button, 1.2)
        if (root.hovered)
            return Qt.lighter(root.palette.button, 1.1)
        if (root.highlighted)
            return Qt.lighter(root.palette.button, 1.15)
        return root.palette.button
    }
    readonly property color __textColor: {
        if (!root.enabled)
            return root.flat ? "#555555" : root.palette.buttonText
        if (root.flat)
            return root.palette.button
        return root.palette.buttonText
    }
    background: Item {
        anchors.fill: parent
        MultiEffect {
            source: backgroundFill
            anchors.fill: backgroundFill
            visible: !root.flat
            shadowEnabled: true
            shadowVerticalOffset: root.highlighted ? 4 : 3
            shadowBlur: root.highlighted ? 0.20 : 0.13
            shadowColor: root.highlighted ? "#90000000" : "#80000000"
        }
        Rectangle {
            id: backgroundFill
            anchors.fill: parent
            radius: 30
            color: root.__bgColor
        }
    }
    contentItem: Item {
        anchors.fill: parent
        Label {
            anchors.centerIn: parent
            color: root.__textColor
            text: root.text
            font.bold: root.highlighted
        }
    }
}`
	dir := writeStyleModule(t, map[string]string{"Button": button})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
Item {
    width: 300; height: 100
    ColumnLayout {
        x: 0; y: 0; width: 300
        RowLayout {
            spacing: 10
            Layout.fillWidth: true
            Button {
                text: "Success"
                Layout.preferredWidth: 120
                Layout.preferredHeight: 40
                palette.button: "#00d1a8"
                palette.buttonText: "white"
            }
            Button {
                text: "Warning"
                Layout.preferredWidth: 120
                Layout.preferredHeight: 40
                palette.button: "#ffe361"
                palette.buttonText: "#333"
            }
        }
    }
}`, env)
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(300, 100)
	fill := func() (ui.RGBA, bool) {
		var out ui.RGBA
		var found bool
		walkTree(tree, func(e ui.Element) {
			if c, ok := ui.FillColor(e); ok && c.A > 0 && !found {
				r := ui.Bounds(e)
				if r.W == 120 && r.H == 40 && r.X == 0 {
					out, found = c, true
				}
			}
		})
		return out, found
	}
	rest, ok := fill()
	if !ok {
		t.Fatal("no background rectangle in the resting button")
	}
	if rest.G < 0.7 || rest.B < 0.6 {
		t.Fatalf("resting colour %v is not the use site's palette.button #00d1a8", rest)
	}
	u.Mouse([]app.MouseEvent{
		{Kind: app.MouseMove, X: 40, Y: 20},
		{Kind: app.MouseDown, X: 40, Y: 20, Button: 1},
	})
	u.Layout(300, 100)
	u.Layout(300, 100)
	down, _ := fill()
	if down == rest {
		t.Errorf("held button kept its resting colour %v: the derived __bgColor did not re-evaluate", rest)
	}
	if down.G >= rest.G {
		t.Errorf("held colour %v is not darker than resting %v", down, rest)
	}
	u.Mouse([]app.MouseEvent{{Kind: app.MouseUp, X: 40, Y: 20, Button: 1}, {Kind: app.MouseMove, X: 200, Y: 90}})
	u.Layout(300, 100)
	u.Layout(300, 100)
	if up, _ := fill(); up != rest {
		t.Errorf("released button colour %v did not return to %v", up, rest)
	}
}

// A Menu declared INSIDE a ToolButton with `y: btn.height` opens in the
// button's coordinates — directly under it — not centred in the window.
func TestMenuInsideItsButtonOpensUnderIt(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
import QtQuick.Controls
Item {
    width: 600; height: 500
    ToolButton {
        id: fileBtn
        x: 300; y: 200; width: 100; height: 40
        text: "File"
        onClicked: fileMenu.open()
        Menu {
            id: fileMenu
            y: fileBtn.height
            MenuItem { text: "New" }
            MenuItem { text: "Open" }
            MenuSeparator {}
            MenuItem { text: "Close" }
        }
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(600, 500)
	u.Layout(600, 500)
	if !u.Click(350, 220) {
		t.Fatal("the tool button was not clickable")
	}
	u.Layout(600, 500)
	u.Layout(600, 500)
	var entry, last ui.Rect
	walkTree(tree, func(e ui.Element) {
		if s, ok := ui.TextOf(e); ok {
			switch s {
			case "New":
				entry = ui.Bounds(e)
			case "Close":
				last = ui.Bounds(e)
			}
		}
	})
	if entry == (ui.Rect{}) {
		t.Fatal("the menu did not open")
	}
	// Under the button (200+40) and left-aligned with it (300), within the
	// menu's own padding.
	if entry.Y < 240 || entry.Y > 260 {
		t.Errorf("first entry at y=%v, want just under the button's bottom edge (240)", entry.Y)
	}
	if entry.X < 300 || entry.X > 320 {
		t.Errorf("first entry at x=%v, want aligned with the button's left edge (300)", entry.X)
	}
	// Sized to its items: four rows and a separator, not the window.
	if last.Y+last.H > entry.Y+220 {
		t.Errorf("menu stretches to y=%v: it should be as tall as its four items", last.Y+last.H)
	}
}

// The same menu through the corpus' STYLE: a T.Menu whose content is a
// ListView of styled MenuItems, declared inside a styled ToolButton in a
// RowLayout. It opens under the button, sized to its items.
func TestStyledMenuInsideStyledToolButtonOpensUnderIt(t *testing.T) {
	menu := `
import QtQuick
import QtQuick.Templates as T
import QtQuick.Effects
T.Menu {
    id: root
    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)
    padding: 4
    delegate: MenuItem {}
    contentItem: ListView {
        implicitHeight: contentHeight
        model: root.contentModel
        interactive: Window.window
                     ? contentHeight + root.topPadding + root.bottomPadding > Window.window.height
                     : false
        clip: true
        currentIndex: root.currentIndex
    }
    background: Rectangle {
        implicitWidth: 180
        color: "#1b1e24"
        radius: 8
        border.color: "#2a2d35"
        border.width: 1
        layer.enabled: true
        layer.effect: MultiEffect { shadowEnabled: true; shadowVerticalOffset: 4; shadowBlur: 0.3 }
    }
}`
	item := `
import QtQuick
import QtQuick.Templates as T
T.MenuItem {
    id: root
    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding,
                             indicator ? indicator.implicitHeight + topPadding + bottomPadding : 0)
    padding: 8
    leftPadding: 12
    rightPadding: 12
    contentItem: Text {
        leftPadding: root.checkable ? root.indicator.width + root.spacing : 0
        text: root.text
        font.pixelSize: 14
        color: root.highlighted ? "#00d1a8" : "#d8d8e0"
        verticalAlignment: Text.AlignVCenter
    }
    indicator: Rectangle {
        x: 8
        y: root.topPadding + (root.availableHeight - height) / 2
        width: 16; height: 16; radius: 3
        visible: root.checkable
        color: "transparent"
        border.color: root.checked ? "#00d1a8" : "#555555"
        border.width: 1
    }
    background: Rectangle {
        implicitWidth: 170
        implicitHeight: 36
        radius: 4
        color: root.highlighted ? "#12141a" : "transparent"
    }
}`
	sep := `
import QtQuick
import QtQuick.Templates as T
T.MenuSeparator {
    id: root
    implicitWidth: Math.max(implicitBackgroundWidth + leftInset + rightInset,
                            implicitContentWidth + leftPadding + rightPadding)
    implicitHeight: Math.max(implicitBackgroundHeight + topInset + bottomInset,
                             implicitContentHeight + topPadding + bottomPadding)
    padding: 4
    leftPadding: 12
    rightPadding: 12
    contentItem: Rectangle { implicitWidth: 160; implicitHeight: 1; color: "#12141a" }
}`
	tool := `
import QtQuick
import QtQuick.Templates as T
T.ToolButton {
    id: root
    implicitWidth: 140
    implicitHeight: 40
    background: Item { anchors.fill: parent }
    contentItem: Item {
        anchors.fill: parent
        Label { anchors.fill: parent; verticalAlignment: Text.AlignVCenter; text: root.text
                color: root.pressed ? "#00d1a8" : "#555555" }
    }
}`
	dir := writeStyleModule(t, map[string]string{"Menu": menu, "MenuItem": item, "MenuSeparator": sep, "ToolButton": tool})
	env := NewEnv()
	d := loadPage(t, dir, `
import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
Item {
    width: 600; height: 500
    Rectangle {
        x: 200; y: 150; width: 380; height: 200; color: "#1b1e24"
        RowLayout {
            x: 20; y: 50
            spacing: 20
            ToolButton {
                id: fileToolBtn
                text: "File"
                Layout.preferredWidth: 100
                Layout.preferredHeight: 40
                onClicked: fileMenu.open()
                Menu {
                    id: fileMenu
                    y: fileToolBtn.height
                    MenuItem { text: "New" }
                    MenuItem { text: "Open" }
                    MenuItem { text: "Save" }
                    MenuSeparator {}
                    MenuItem { text: "Close" }
                }
            }
            ToolButton {
                id: settingsToolBtn
                text: "Settings"
                Layout.preferredWidth: 130
                Layout.preferredHeight: 40
                onClicked: settingsMenu.open()
                Menu {
                    id: settingsMenu
                    y: settingsToolBtn.height
                    MenuItem { text: "Preferences" }
                }
            }
        }
    }
}`, env)
	var tree ui.Element
	u := ui.New(nil, nil, func() ui.Element { tree = d.View()(); return tree })
	u.Layout(600, 500)
	u.Layout(600, 500)
	// The File button sits at (220, 200): the card's origin plus the row's.
	if !u.Click(250, 220) {
		t.Fatal("the tool button was not clickable")
	}
	u.Layout(600, 500)
	u.Layout(600, 500)
	var entry, last ui.Rect
	var fills []ui.Rect
	walkTree(tree, func(e ui.Element) {
		if s, ok := ui.TextOf(e); ok {
			switch s {
			case "New":
				entry = ui.Bounds(e)
			case "Close":
				last = ui.Bounds(e)
			}
		}
		if c, ok := ui.FillColor(e); ok && c.A > 0 {
			if r := ui.Bounds(e); r.W > 170 && r.W < 190 {
				fills = append(fills, r)
			}
		}
	})
	if entry == (ui.Rect{}) {
		t.Fatal("the menu did not open")
	}
	t.Logf("entry=%v last=%v fills=%v", entry, last, fills)
	if entry.Y < 240 || entry.Y > 265 {
		t.Errorf("first entry at y=%v, want just under the button's bottom edge (240)", entry.Y)
	}
	if entry.X < 220 || entry.X > 245 {
		t.Errorf("first entry at x=%v, want aligned with the button's left edge (220)", entry.X)
	}
	// Four 36px rows and a 9px separator inside 4px padding: ~160px, not
	// a panel stretched to the window.
	if last.Y+last.H > entry.Y+200 {
		t.Errorf("menu content runs to y=%v: it should be as tall as its items", last.Y+last.H)
	}
	for _, r := range fills {
		if r.H > 220 {
			t.Errorf("menu background is %v tall; the items add up to ~160", r.H)
		}
	}
}
