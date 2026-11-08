package gml

// QML's attached Accessible properties, over ui.Access: an item's
// Accessible.role, .name and .description tag the element it builds for
// assistive technology (the controls kit already tags its own controls).

import (
	"strings"

	"github.com/sushvibez/gfx/ui"
)

// accessibleRoles are QML's Accessible.Role names; each evaluates to its own
// name, which ui.RoleByName reads.
var accessibleRoles = []string{
	"NoRole", "TitleBar", "MenuBar", "ScrollBar", "Grip", "Sound", "Cursor",
	"Caret", "AlertMessage", "Window", "Client", "PopupMenu", "MenuItem",
	"ToolTip", "Application", "Document", "Pane", "Chart", "Dialog", "Border",
	"Grouping", "Separator", "ToolBar", "StatusBar", "Table", "ColumnHeader",
	"RowHeader", "Column", "Row", "Cell", "Link", "HelpBalloon", "Assistant",
	"List", "ListItem", "Tree", "TreeItem", "PageTab", "PropertyPage",
	"Indicator", "Graphic", "StaticText", "EditableText", "Button",
	"PushButton", "CheckBox", "RadioButton", "ComboBox", "ProgressBar",
	"Dial", "HotkeyField", "Slider", "SpinBox", "Canvas", "Animation",
	"Equation", "ButtonDropDown", "ButtonMenu", "ButtonDropGrid", "Whitespace",
	"PageTabList", "Clock", "Splitter", "LayeredPane", "Terminal", "Desktop",
	"Paragraph", "WebDocument", "Section", "Notification", "ColorChooser",
	"Footer", "Form", "Heading", "Note",
}

func registerAccessible(vars map[string]any) {
	ns := map[string]any{}
	for _, r := range accessibleRoles {
		ns[r] = r
	}
	vars["Accessible"] = ns
}

// applyAccessible puts an item's Accessible.* on the box it built.
func applyAccessible(n *node, env *Env, el ui.Element) {
	d, ok := el.(*ui.DivEl)
	if !ok {
		return
	}
	var a ui.Access
	found := false
	for _, at := range n.attrs {
		if !strings.HasPrefix(at.name, "Accessible.") || at.expr == nil {
			continue
		}
		v, err := at.expr.eval(env)
		if err != nil {
			continue
		}
		switch at.name {
		case "Accessible.role":
			a.Role, found = qmlRole(str(v)), true
		case "Accessible.name":
			a.Name, found = str(v), true
		case "Accessible.description":
			a.Description, found = str(v), true
		}
	}
	if !found {
		return
	}
	// Keep what the control already says about itself, overriding only
	// what the markup states.
	cur, had := ui.AccessOf(d)
	if had {
		if a.Role == ui.RoleNone {
			a.Role = cur.Role
		}
		if a.Name == "" {
			a.Name = cur.Name
		}
		a.Value, a.Checked, a.Expanded, a.Selected, a.Disabled, a.Level =
			cur.Value, cur.Checked, cur.Expanded, cur.Selected, cur.Disabled, cur.Level
	} else {
		a.Checked, a.Expanded = -1, -1
		if a.Role == ui.RoleNone {
			a.Role = ui.RoleGroup // named, so something a reader can announce
		}
	}
	d.Access(a)
}

// qmlRole maps a QML Accessible.Role name to a ui role.
func qmlRole(name string) ui.Role {
	switch name {
	case "Dial":
		return ui.RoleSlider
	case "PageTab":
		return ui.RoleTab
	case "PageTabList":
		return ui.RoleTabList
	case "Graphic":
		return ui.RoleImage
	case "Grouping", "Pane", "Form", "Section":
		return ui.RoleGroup
	case "Heading", "Paragraph", "Note":
		return ui.RoleStaticText
	}
	return ui.RoleByName(name)
}
