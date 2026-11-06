package render

// Minimal color-emoji font reader: CBDT/CBLC embedded-PNG glyphs (the format
// Noto Color Emoji ships) plus the cmap needed to map runes to glyph ids.
// x/image/font/sfnt has no CBDT support, and we need exactly three tables, so
// this parses them directly. Single-codepoint emoji only — ZWJ sequences and
// variation selectors render as their component glyphs (shaping comes later,
// with the rest of complex-text support).

import (
	"encoding/binary"
	"fmt"
)

// emojiFont is a parsed CBDT/CBLC color font: rune → glyph id (cmap) and
// glyph id → embedded PNG (best/largest strike).
type emojiFont struct {
	cmap   map[rune]uint16
	ppem   int               // strike size the PNGs were rasterized at
	glyphs map[uint16][]byte // glyph id → PNG bytes
}

func (e *emojiFont) covers(r rune) bool {
	if e == nil {
		return false
	}
	gid, ok := e.cmap[r]
	if !ok {
		return false
	}
	_, ok = e.glyphs[gid]
	return ok
}

func (e *emojiFont) lookup(r rune) ([]byte, bool) {
	if e == nil {
		return nil, false
	}
	gid, ok := e.cmap[r]
	if !ok {
		return nil, false
	}
	png, ok := e.glyphs[gid]
	return png, ok
}

func be16(b []byte, o int) uint16 { return binary.BigEndian.Uint16(b[o:]) }
func be32(b []byte, o int) uint32 { return binary.BigEndian.Uint32(b[o:]) }

// parseEmojiFont reads the cmap + CBLC/CBDT tables out of an sfnt file.
func parseEmojiFont(data []byte) (*emojiFont, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("emoji: not a font")
	}
	numTables := int(be16(data, 4))
	if len(data) < 12+numTables*16 {
		return nil, fmt.Errorf("emoji: truncated table directory")
	}
	table := map[string][]byte{}
	for i := 0; i < numTables; i++ {
		rec := 12 + i*16
		tag := string(data[rec : rec+4])
		off, ln := be32(data, rec+8), be32(data, rec+12)
		if int(off)+int(ln) > len(data) {
			continue
		}
		table[tag] = data[off : off+ln]
	}
	cmapT, cblc, cbdt := table["cmap"], table["CBLC"], table["CBDT"]
	if cmapT == nil || cblc == nil || cbdt == nil {
		return nil, fmt.Errorf("emoji: font has no CBDT/CBLC color bitmaps")
	}
	cm, err := parseCmap(cmapT)
	if err != nil {
		return nil, err
	}
	ppem, glyphs, err := parseCBLC(cblc, cbdt)
	if err != nil {
		return nil, err
	}
	return &emojiFont{cmap: cm, ppem: ppem, glyphs: glyphs}, nil
}

// parseCmap reads the best unicode subtable: format 12 (full range) if
// present, else format 4 (BMP).
func parseCmap(t []byte) (map[rune]uint16, error) {
	if len(t) < 4 {
		return nil, fmt.Errorf("emoji: bad cmap")
	}
	n := int(be16(t, 2))
	var f12, f4 []byte
	for i := 0; i < n; i++ {
		rec := 4 + i*8
		if rec+8 > len(t) {
			break
		}
		off := int(be32(t, rec+4))
		if off+2 > len(t) {
			continue
		}
		switch be16(t, off) {
		case 12:
			f12 = t[off:]
		case 4:
			f4 = t[off:]
		}
	}
	m := map[rune]uint16{}
	switch {
	case f12 != nil:
		nGroups := int(be32(f12, 12))
		for g := 0; g < nGroups; g++ {
			rec := 16 + g*12
			if rec+12 > len(f12) {
				break
			}
			start, end, gid := be32(f12, rec), be32(f12, rec+4), be32(f12, rec+8)
			for c := start; c <= end && c-start < 0x10000; c++ {
				m[rune(c)] = uint16(gid + (c - start))
			}
		}
	case f4 != nil:
		segX2 := int(be16(f4, 6))
		ends, starts := 14, 14+segX2+2
		deltas, ranges := starts+segX2, starts+segX2*2
		for s := 0; s < segX2; s += 2 {
			start, end := be16(f4, starts+s), be16(f4, ends+s)
			delta, ro := be16(f4, deltas+s), be16(f4, ranges+s)
			for c := int(start); c <= int(end) && c != 0xffff; c++ {
				var gid uint16
				if ro == 0 {
					gid = uint16(c) + delta
				} else {
					idx := ranges + s + int(ro) + (c-int(start))*2
					if idx+2 > len(f4) {
						continue
					}
					gid = be16(f4, idx)
					if gid != 0 {
						gid += delta
					}
				}
				if gid != 0 {
					m[rune(c)] = gid
				}
			}
		}
	default:
		return nil, fmt.Errorf("emoji: no usable cmap subtable")
	}
	return m, nil
}

// parseCBLC walks the largest strike's index subtables and slices each
// glyph's PNG out of CBDT. Handles index formats 1 and 3 (offset arrays) and
// image formats 17/18/19 (PNG with small/big/shared metrics).
func parseCBLC(cblc, cbdt []byte) (int, map[uint16][]byte, error) {
	if len(cblc) < 8 {
		return 0, nil, fmt.Errorf("emoji: bad CBLC")
	}
	numSizes := int(be32(cblc, 4))
	best, bestPpem := -1, -1
	for i := 0; i < numSizes; i++ {
		rec := 8 + i*48
		if rec+48 > len(cblc) {
			break
		}
		if p := int(cblc[rec+45]); p > bestPpem { // ppemY
			best, bestPpem = i, p
		}
	}
	if best < 0 {
		return 0, nil, fmt.Errorf("emoji: no strikes")
	}
	rec := 8 + best*48
	arrOff := int(be32(cblc, rec))
	numSub := int(be32(cblc, rec+8))
	glyphs := map[uint16][]byte{}
	for s := 0; s < numSub; s++ {
		e := arrOff + s*8
		if e+8 > len(cblc) {
			break
		}
		first, last := be16(cblc, e), be16(cblc, e+2)
		sub := arrOff + int(be32(cblc, e+4))
		if sub+8 > len(cblc) {
			continue
		}
		idxFmt, imgFmt := be16(cblc, sub), be16(cblc, sub+2)
		imgOff := int(be32(cblc, sub+4))
		count := int(last) - int(first) + 1
		offAt := func(i int) (int, bool) { // offset of glyph i's image data within CBDT
			switch idxFmt {
			case 1:
				o := sub + 8 + i*4
				if o+8 > len(cblc) {
					return 0, false
				}
				return imgOff + int(be32(cblc, o)), true
			case 3:
				o := sub + 8 + i*2
				if o+4 > len(cblc) {
					return 0, false
				}
				return imgOff + int(be16(cblc, o)), true
			}
			return 0, false
		}
		for i := 0; i < count; i++ {
			start, ok1 := offAt(i)
			end, ok2 := offAt(i + 1)
			if !ok1 || !ok2 || end <= start || end > len(cbdt) {
				continue
			}
			png := sliceGlyphPNG(cbdt[start:end], imgFmt)
			if png != nil {
				glyphs[first+uint16(i)] = png
			}
		}
	}
	return bestPpem, glyphs, nil
}

// sliceGlyphPNG strips the per-glyph metrics header of a CBDT entry, leaving
// raw PNG bytes. Format 17: smallGlyphMetrics(5)+len(4); 18: bigGlyphMetrics
// (8)+len(4); 19: len(4) only (metrics live in CBLC).
func sliceGlyphPNG(b []byte, imgFmt uint16) []byte {
	var hdr int
	switch imgFmt {
	case 17:
		hdr = 5 + 4
	case 18:
		hdr = 8 + 4
	case 19:
		hdr = 4
	default:
		return nil
	}
	if len(b) <= hdr {
		return nil
	}
	return b[hdr:]
}
