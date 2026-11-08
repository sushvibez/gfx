package gml

import (
	"strings"
	"testing"

	"github.com/sushvibez/gfx/ui"
)

// Accessible.role/name/description tag the element; a control's own role
// and state stay where the markup does not override them.
func TestAccessibleAttached(t *testing.T) {
	src := `
import QtQuick 2.15
import QtQuick.Controls 2.15
Column {
    width: 300; height: 200
    Rectangle { width: 40; height: 40; color: "red"
        Accessible.role: Accessible.Button; Accessible.name: "Close" }
    CheckBox { text: "Wrap"; checked: true; Accessible.description: "Wraps long lines" }
    Item { width: 10; height: 10; Accessible.name: "Spacer note" }
}`
	d, err := LoadString(src, NewEnv())
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	u := ui.New(nil, nil, d.View())
	u.Layout(300, 200)
	u.Layout(300, 200)
	got := u.AccessTree().String()
	for _, want := range []string{`button "Close"`, `checkbox "Wrap" checked`, `group "Spacer note"`} {
		if !strings.Contains(got, want) {
			t.Errorf("tree lacks %q:\n%s", want, got)
		}
	}
	if n := u.AccessTree().Find(ui.RoleCheckBox, "Wrap"); n == nil || n.Description != "Wraps long lines" {
		t.Errorf("the check box's description: %+v", n)
	}
}
