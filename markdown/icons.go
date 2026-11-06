//go:build linux || windows

package main

// Inline 24×24 lucide-style stroke icons for the Obsidian-parity chrome.

import "github.com/sushvibez/gfx/render"

const st = `fill="none" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"`

var iconSrc = map[string]string{
	"panelL":   `<svg viewBox="0 0 24 24"><g ` + st + `><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18"/></g></svg>`,
	"panelR":   `<svg viewBox="0 0 24 24"><g ` + st + `><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M15 3v18"/></g></svg>`,
	"folder":   `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2z"/></g></svg>`,
	"search":   `<svg viewBox="0 0 24 24"><g ` + st + `><circle cx="11" cy="11" r="7"/><path d="M21 21l-4.35-4.35"/></g></svg>`,
	"bookmark": `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M19 21l-7-4-7 4V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v16z"/></g></svg>`,
	"filesrch": `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><circle cx="11" cy="14" r="2.5"/><path d="M13 16l2 2"/></g></svg>`,
	"graph":    `<svg viewBox="0 0 24 24"><g ` + st + `><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="6" r="2.5"/><circle cx="17" cy="17" r="2.5"/><path d="M8 16.4L15.5 8M8.4 17.5h6.1"/></g></svg>`,
	"grid":     `<svg viewBox="0 0 24 24"><g ` + st + `><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></g></svg>`,
	"terminal": `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M4 17l6-6-6-6"/><path d="M12 19h8"/></g></svg>`,
	"binary":   `<svg viewBox="0 0 24 24"><g ` + st + `><rect x="14" y="14" width="4" height="6" rx="1"/><rect x="6" y="4" width="4" height="6" rx="1"/><path d="M6 20h4M14 10h4M6 14h2v6M14 4h2v6"/></g></svg>`,
	"listtree": `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></g></svg>`,
	"pencil":   `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"/></g></svg>`,
	"folderP":  `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2z"/><path d="M12 10v6M9 13h6"/></g></svg>`,
	"sort":     `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M11 5h10M11 9h7M11 13h4"/><path d="M3 17l3 3 3-3M6 18V4"/></g></svg>`,
	"expand":   `<svg viewBox="0 0 24 24"><g ` + st + `><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 9v12"/></g></svg>`,
	"collapse": `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M7 15l5 5 5-5M7 9l5-5 5 5"/></g></svg>`,
	"chevD":    `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M6 9l6 6 6-6"/></g></svg>`,
	"chevR":    `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M9 6l6 6-6 6"/></g></svg>`,
	"chevUD":   `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M7 15l5 5 5-5M7 9l5-5 5 5"/></g></svg>`,
	"help":     `<svg viewBox="0 0 24 24"><g ` + st + `><circle cx="12" cy="12" r="9"/><path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3"/><path d="M12 17h.01"/></g></svg>`,
	"gear":     `<svg viewBox="0 0 24 24"><g ` + st + `><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></g></svg>`,
	"canvasT":  `<svg viewBox="0 0 24 24"><g ` + st + `><rect x="3" y="3" width="8" height="8" rx="1"/><rect x="13" y="3" width="8" height="8" rx="1"/><rect x="3" y="13" width="8" height="8" rx="1"/></g></svg>`,
	"close":    `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M18 6L6 18M6 6l12 12"/></g></svg>`,
	"plus":     `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M12 5v14M5 12h14"/></g></svg>`,
	"minus":    `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M5 12h14"/></g></svg>`,
	"maxim":    `<svg viewBox="0 0 24 24"><g ` + st + `><rect x="5" y="5" width="14" height="14" rx="2"/></g></svg>`,
	"moreV":    `<svg viewBox="0 0 24 24"><g fill="#000"><circle cx="12" cy="5" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="12" cy="19" r="1.7"/></g></svg>`,
	"arrowL":   `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M19 12H5M12 19l-7-7 7-7"/></g></svg>`,
	"arrowR":   `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M5 12h14M12 5l7 7-7 7"/></g></svg>`,
	"undo":     `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M3 7v6h6"/><path d="M21 17a9 9 0 0 0-15-6.7L3 13"/></g></svg>`,
	"redo":     `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M21 7v6h-6"/><path d="M3 17a9 9 0 0 1 15-6.7L21 13"/></g></svg>`,
	"refresh":  `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M21 12a9 9 0 1 1-2.64-6.36L21 8"/><path d="M21 3v5h-5"/></g></svg>`,
	"fullscr":  `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M8 3H5a2 2 0 0 0-2 2v3M16 3h3a2 2 0 0 1 2 2v3M8 21H5a2 2 0 0 1-2-2v-3M16 21h3a2 2 0 0 0 2-2v-3"/></g></svg>`,
	"fileCard": `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/></g></svg>`,
	"noteCard": `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="M8 13h8M8 17h5"/></g></svg>`,
	"imgCard":  `<svg viewBox="0 0 24 24"><g ` + st + `><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><circle cx="10" cy="12" r="1.6"/><path d="M20 19l-4.5-4.5L8 22"/></g></svg>`,
}

var icons = map[string]*render.SVG{}

func loadIcons() error {
	for name, src := range iconSrc {
		s, err := render.ParseSVG([]byte(src))
		if err != nil {
			return err
		}
		icons[name] = s
	}
	return nil
}
