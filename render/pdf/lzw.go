package pdf

// LZWDecode: the TIFF/PDF variant of LZW — 8-bit data, codes growing from
// 9 to 12 bits, 256 = clear, 257 = end of data. Go's compress/lzw reads
// the GIF and TIFF orderings but not PDF's optional "early change" of the
// code width, so this is written out.

// lzwDecode expands an LZW stream. earlyChange 1 (the default) grows the
// code width one code sooner, which is what almost every producer emits.
func lzwDecode(b []byte, earlyChange int) []byte {
	const (
		clearCode = 256
		eodCode   = 257
	)
	table := make([][]byte, 258, 4096)
	reset := func() {
		table = table[:258]
		for i := 0; i < 256; i++ {
			table[i] = []byte{byte(i)}
		}
	}
	reset()

	var out []byte
	width := 9
	var prev []byte
	bitPos := 0
	read := func() (int, bool) {
		if (bitPos+width+7)/8 > len(b) {
			return 0, false
		}
		v := 0
		for i := 0; i < width; i++ {
			byteIdx := (bitPos + i) / 8
			bitIdx := 7 - uint((bitPos+i)%8)
			v = v<<1 | int(b[byteIdx]>>bitIdx&1)
		}
		bitPos += width
		return v, true
	}
	for {
		code, ok := read()
		if !ok || code == eodCode {
			return out
		}
		if code == clearCode {
			reset()
			width, prev = 9, nil
			continue
		}
		var entry []byte
		switch {
		case code < len(table):
			entry = table[code]
		case prev != nil: // the KwKwK case: the entry being defined right now
			entry = append(append([]byte{}, prev...), prev[0])
		default:
			return out
		}
		out = append(out, entry...)
		if prev != nil && len(table) < 4096 {
			table = append(table, append(append([]byte{}, prev...), entry[0]))
		}
		prev = entry
		// The width grows as the table fills; earlyChange makes it one code early.
		switch len(table) + earlyChange {
		case 512:
			width = 10
		case 1024:
			width = 11
		case 2048:
			width = 12
		}
	}
}
