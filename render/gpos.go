package render

import "encoding/binary"

// GPOS pair kerning. x/image/font/opentype's Kern reads only the legacy
// 'kern' table, but modern shapers (DirectWrite, HarfBuzz, browsers) take
// kerning from the GPOS 'kern' feature and ignore the legacy table when
// GPOS is present — the two can differ wildly (Segoe UI's legacy table says
// "To" = -12.5 units/64 ≈ -3px at 16px; its GPOS value is a fraction of
// that), so using the legacy table visibly over-tucks kerned pairs. This
// file extracts the horizontal XAdvance adjustments of the GPOS 'kern'
// feature (PairPos lookups, formats 1 and 2, including type-9 extension
// wrappers) for the default script, which is what simple UI-text shaping
// applies.
//
// Values are font units; Font.kern scales by sizePx/unitsPerEm.

type gposKern struct {
	data    []byte   // the full GPOS table
	subs    []uint32 // PairPos subtable offsets (in lookup order)
	classed bool     // set once subs is populated
}

func gp16(b []byte, o int) int {
	if o+2 > len(b) {
		return 0
	}
	return int(binary.BigEndian.Uint16(b[o:]))
}

func gps16(b []byte, o int) int16 {
	if o+2 > len(b) {
		return 0
	}
	return int16(binary.BigEndian.Uint16(b[o:]))
}

// parseGPOSKern locates the GPOS 'kern' feature's PairPos subtables for the
// default (or first) script. Returns nil when the font has no usable GPOS
// kerning — callers then fall back to the legacy 'kern' table.
func parseGPOSKern(ttf []byte) *gposKern {
	gpos := sfntTable(ttf, "GPOS")
	if gpos == nil || len(gpos) < 10 {
		return nil
	}
	scriptList := gp16(gpos, 4)
	featureList := gp16(gpos, 6)
	lookupList := gp16(gpos, 8)
	if scriptList == 0 || featureList == 0 || lookupList == 0 {
		return nil
	}

	// Script: prefer 'DFLT', then 'latn', else the first entry; use its
	// default LangSys. The LangSys lists feature indices into FeatureList.
	scriptOff := 0
	n := gp16(gpos, scriptList)
	for i := 0; i < n; i++ {
		rec := scriptList + 2 + i*6
		tag := string(gpos[rec : rec+4])
		off := scriptList + gp16(gpos, rec+4)
		if tag == "DFLT" {
			scriptOff = off
			break
		}
		if tag == "latn" || scriptOff == 0 {
			scriptOff = off
		}
	}
	if scriptOff == 0 {
		return nil
	}
	langSys := 0
	if d := gp16(gpos, scriptOff); d != 0 {
		langSys = scriptOff + d
	} else if gp16(gpos, scriptOff+2) > 0 { // no default LangSys: first record
		langSys = scriptOff + gp16(gpos, scriptOff+2+4)
	}
	if langSys == 0 {
		return nil
	}

	// Collect the lookup indices of every 'kern' feature in the LangSys.
	var lookups []int
	featCount := gp16(gpos, langSys+4)
	for i := 0; i < featCount; i++ {
		fi := gp16(gpos, langSys+6+i*2)
		rec := featureList + 2 + fi*6
		if rec+6 > len(gpos) || string(gpos[rec:rec+4]) != "kern" {
			continue
		}
		feat := featureList + gp16(gpos, rec+4)
		lc := gp16(gpos, feat+2)
		for j := 0; j < lc; j++ {
			lookups = append(lookups, gp16(gpos, feat+4+j*2))
		}
	}
	if len(lookups) == 0 {
		return nil
	}

	// Resolve lookups to PairPos (type 2) subtable offsets; type 9 extension
	// subtables wrap another type at a 32-bit offset.
	k := &gposKern{data: gpos}
	for _, li := range lookups {
		lookup := lookupList + gp16(gpos, lookupList+2+li*2)
		typ := gp16(gpos, lookup)
		subCount := gp16(gpos, lookup+4)
		for s := 0; s < subCount; s++ {
			sub := lookup + gp16(gpos, lookup+6+s*2)
			st, ok := sub, typ == 2
			if typ == 9 && gp16(gpos, sub) == 1 && gp16(gpos, sub+2) == 2 {
				if sub+8 <= len(gpos) {
					st = sub + int(binary.BigEndian.Uint32(gpos[sub+4:]))
					ok = true
				}
			}
			if ok && st < len(gpos) {
				k.subs = append(k.subs, uint32(st))
			}
		}
	}
	if len(k.subs) == 0 {
		return nil
	}
	return k
}

// sfntTable returns the named table's bytes from a TTF/OTF file.
func sfntTable(ttf []byte, tag string) []byte {
	if len(ttf) < 12 {
		return nil
	}
	n := gp16(ttf, 4)
	for i := 0; i < n; i++ {
		rec := 12 + i*16
		if rec+16 > len(ttf) {
			return nil
		}
		if string(ttf[rec:rec+4]) != tag {
			continue
		}
		off := binary.BigEndian.Uint32(ttf[rec+8:])
		length := binary.BigEndian.Uint32(ttf[rec+12:])
		if uint64(off)+uint64(length) > uint64(len(ttf)) {
			return nil
		}
		return ttf[off : off+length]
	}
	return nil
}

// coverageIndex returns g's index in an OpenType coverage table, or -1.
func coverageIndex(b []byte, cov int, g int) int {
	switch gp16(b, cov) {
	case 1: // sorted glyph list
		count := gp16(b, cov+2)
		lo, hi := 0, count
		for lo < hi {
			mid := (lo + hi) / 2
			v := gp16(b, cov+4+mid*2)
			switch {
			case v < g:
				lo = mid + 1
			case v > g:
				hi = mid
			default:
				return mid
			}
		}
	case 2: // ranges with running start-coverage index
		count := gp16(b, cov+2)
		lo, hi := 0, count
		for lo < hi {
			mid := (lo + hi) / 2
			rec := cov + 4 + mid*6
			start, end := gp16(b, rec), gp16(b, rec+2)
			switch {
			case end < g:
				lo = mid + 1
			case start > g:
				hi = mid
			default:
				return gp16(b, rec+4) + g - start
			}
		}
	}
	return -1
}

// classOf returns g's class in an OpenType ClassDef table (default 0).
func classOf(b []byte, cd int, g int) int {
	switch gp16(b, cd) {
	case 1:
		start := gp16(b, cd+2)
		count := gp16(b, cd+4)
		if g >= start && g < start+count {
			return gp16(b, cd+6+(g-start)*2)
		}
	case 2:
		count := gp16(b, cd+4)
		lo, hi := 0, count
		for lo < hi {
			mid := (lo + hi) / 2
			rec := cd + 6 + mid*6
			start, end := gp16(b, rec), gp16(b, rec+2)
			switch {
			case end < g:
				lo = mid + 1
			case start > g:
				hi = mid
			default:
				return gp16(b, rec+4)
			}
		}
	}
	return 0
}

// valueRecordSize returns a GPOS ValueRecord's byte size for a value format.
func valueRecordSize(format int) int {
	n := 0
	for f := format; f != 0; f >>= 1 {
		n += f & 1
	}
	return n * 2
}

// xAdvanceIn extracts the XAdvance field (bit 0x0004) of a value record.
func xAdvanceIn(b []byte, rec, format int) int16 {
	if format&0x0004 == 0 {
		return 0
	}
	off := 0
	if format&0x0001 != 0 {
		off += 2
	}
	if format&0x0002 != 0 {
		off += 2
	}
	return gps16(b, rec+off)
}

// Pair returns the XAdvance kerning (font units) for the glyph pair, first
// match across the feature's subtables — OpenType application order.
func (k *gposKern) Pair(g1, g2 int) int16 {
	b := k.data
	for _, so := range k.subs {
		st := int(so)
		ci := coverageIndex(b, st+gp16(b, st+2), g1)
		if ci < 0 {
			continue
		}
		vf1, vf2 := gp16(b, st+4), gp16(b, st+6)
		sz1, sz2 := valueRecordSize(vf1), valueRecordSize(vf2)
		switch gp16(b, st) {
		case 1: // per-pair sets
			if ci >= gp16(b, st+8) {
				continue
			}
			ps := st + gp16(b, st+10+ci*2)
			pairCount := gp16(b, ps)
			recSize := 2 + sz1 + sz2
			lo, hi := 0, pairCount
			for lo < hi {
				mid := (lo + hi) / 2
				rec := ps + 2 + mid*recSize
				v := gp16(b, rec)
				switch {
				case v < g2:
					lo = mid + 1
				case v > g2:
					hi = mid
				default:
					return xAdvanceIn(b, rec+2, vf1)
				}
			}
		case 2: // class matrix
			c1 := classOf(b, st+gp16(b, st+8), g1)
			c2 := classOf(b, st+gp16(b, st+10), g2)
			n1, n2 := gp16(b, st+12), gp16(b, st+14)
			if c1 >= n1 || c2 >= n2 {
				continue
			}
			rec := st + 16 + (c1*n2+c2)*(sz1+sz2)
			if v := xAdvanceIn(b, rec, vf1); v != 0 {
				return v
			}
		}
	}
	return 0
}
