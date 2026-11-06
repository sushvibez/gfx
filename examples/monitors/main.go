//go:build linux || windows

// Command monitors prints the display layout the app package sees and, with
// -open N, opens a borderless fullscreen window on monitor N — the shape a
// multi-projector installation uses, one window per projector.
//
//	go run ./monitors
//	go run ./monitors -open 1
//	go run ./monitors -open all -seconds 5
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/sushvibez/gfx/app"
)

func main() {
	open := flag.String("open", "", "open a fullscreen window on monitor N (or \"all\")")
	seconds := flag.Float64("seconds", 2, "how long to keep the window(s) open")
	flag.Parse()

	mons, err := app.Monitors()
	if err != nil {
		fmt.Fprintf(os.Stderr, "monitors: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%d monitor(s):\n", len(mons))
	for i, m := range mons {
		primary := ""
		if m.Primary {
			primary = "  (primary)"
		}
		fmt.Printf("  [%d] %-12s %4dx%-4d at %+d%+d  scale %.2g%s\n",
			i, m.Name, m.Width, m.Height, m.X, m.Y, m.Scale, primary)
	}
	if *open == "" {
		return
	}

	// Which monitors to cover.
	targets := []int{}
	if *open == "all" {
		for i := range mons {
			targets = append(targets, i)
		}
	} else {
		n, err := strconv.Atoi(*open)
		if err != nil || n < 0 || n >= len(mons) {
			fmt.Fprintf(os.Stderr, "monitors: -open %q: not a monitor index in [0,%d)\n", *open, len(mons))
			os.Exit(2)
		}
		targets = append(targets, n)
	}

	// One independent top-level window per monitor: each NewWindow owns its
	// own connection on X11, and on Win32 they share the thread's message
	// pump — either way every window needs pumping every frame.
	var wins []*app.Window
	for _, i := range targets {
		m := mons[i]
		// X/Y name a point INSIDE the target monitor; the backend snaps the
		// window to that monitor's full rect.
		w, err := app.NewWindow(app.Options{
			Title:      fmt.Sprintf("gfx monitor %d (%s)", i, m.Name),
			X:          m.X + m.Width/2,
			Y:          m.Y + m.Height/2,
			Positioned: true,
			Fullscreen: true,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "monitors: open on %d: %v\n", i, err)
			os.Exit(1)
		}
		defer w.Destroy()
		ww, wh := w.Size()
		fmt.Printf("opened on monitor %d (%s): %dx%d, MonitorIndex()=%d\n",
			i, m.Name, ww, wh, w.MonitorIndex())
		wins = append(wins, w)
	}

	deadline := time.Now().Add(time.Duration(*seconds * float64(time.Second)))
	for time.Now().Before(deadline) {
		for _, w := range wins {
			if !w.Poll() {
				return
			}
		}
		time.Sleep(16 * time.Millisecond)
	}
}
