//go:build linux && !android

package app

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// OpenFile shows the desktop's own file picker — zenity (GNOME and most
// desktops), else kdialog (KDE) — and blocks until it closes, as the
// Windows dialog does. With neither installed it returns ErrNoFileDialog
// so the app can show ui.FileBrowser instead. ok is false when the user
// cancelled.
func (w *Window) OpenFile(o FileDialogOptions) (path string, ok bool, err error) {
	if z, err := exec.LookPath("zenity"); err == nil {
		args := []string{"--file-selection"}
		if o.Title != "" {
			args = append(args, "--title="+o.Title)
		}
		if o.Dir != "" {
			args = append(args, "--filename="+strings.TrimSuffix(o.Dir, "/")+"/")
		}
		for _, f := range o.Filters {
			args = append(args, "--file-filter="+f.Label+" | "+strings.Join(f.Patterns, " "))
		}
		// GTK's own chooser, not the desktop portal's: zenity 4 asks
		// xdg-desktop-portal for the dialog, and where the portal cannot
		// place it (no app id for an X11 client under GNOME Wayland) zenity
		// exits at once as if cancelled and no dialog ever shows.
		cmd := exec.Command(z, args...)
		cmd.Env = append(os.Environ(), "GDK_DEBUG=no-portals")
		return runPicker(cmd)
	}
	if k, err := exec.LookPath("kdialog"); err == nil {
		dir := o.Dir
		if dir == "" {
			dir = "."
		}
		var pats []string
		for _, f := range o.Filters {
			pats = append(pats, f.Label+" ("+strings.Join(f.Patterns, " ")+")")
		}
		args := []string{"--getopenfilename", dir, strings.Join(pats, "\n")}
		if o.Title != "" {
			args = append(args, "--title", o.Title)
		}
		return runPicker(exec.Command(k, args...))
	}
	return "", false, ErrNoFileDialog
}

// runPicker runs a picker that prints the chosen path; exit status 1 is
// a cancel, not an error.
func runPicker(cmd *exec.Cmd) (string, bool, error) {
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	p := strings.TrimSpace(string(out))
	return p, p != "", nil
}
