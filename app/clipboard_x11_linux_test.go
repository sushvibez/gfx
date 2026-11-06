//go:build linux && !android

package app

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The clipboard is the system's: what one process copies another pastes,
// and the other way round. The second process is this test binary again,
// in a helper mode, so no clipboard tool has to be installed.
func TestClipboardBetweenProcesses(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	startClipboard()
	if clipCh == nil {
		t.Skip("could not open the display")
	}
	helper := func(mode, text string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run", "^TestClipboardHelper$")
		cmd.Env = append(os.Environ(), "GFX_CLIP_MODE="+mode, "GFX_CLIP_TEXT="+text)
		return cmd
	}

	// We copy; the other process pastes. Unicode, so the UTF-8 target is
	// what carried it.
	want := "copied from one — pasted in another ✓"
	SetClipboardText(want)
	if got := ClipboardText(); got != want {
		t.Fatalf("our own paste: %q", got)
	}
	out, err := helper("read", "").Output()
	if err != nil {
		t.Fatalf("helper: %v", err)
	}
	if got := strings.TrimSpace(lastLine(string(out))); got != "GOT:"+want {
		t.Errorf("the other process pasted %q", got)
	}

	// The other process copies (and stays to answer); we paste.
	cmd := helper("write", "the other way round")
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	sc := bufio.NewScanner(stdout)
	for sc.Scan() && sc.Text() != "OWNED" {
	}
	if got := ClipboardText(); got != "the other way round" {
		t.Errorf("we pasted %q from the other process", got)
	}
}

func lastLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "GOT:") {
			return l
		}
	}
	return s
}

// TestClipboardHelper is the second process of the test above.
func TestClipboardHelper(t *testing.T) {
	switch os.Getenv("GFX_CLIP_MODE") {
	case "read":
		os.Stdout.WriteString("GOT:" + ClipboardText() + "\n")
	case "write":
		SetClipboardText(os.Getenv("GFX_CLIP_TEXT"))
		os.Stdout.WriteString("OWNED\n")
		time.Sleep(5 * time.Second) // answering requests until killed
	default:
		t.Skip("helper for TestClipboardBetweenProcesses")
	}
}
