package pdf

// Stream filters. FlateDecode carries almost everything in a real file;
// LZW, ASCIIHex, ASCII85 and RunLength show up in older documents and in
// inline images. DCTDecode (JPEG) and JPX are left encoded — an image
// stream hands those bytes to the image decoder instead.

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

// Decode returns the stream's bytes with its /Filter chain applied. Image
// codecs (DCTDecode, JPXDecode) are not decoded here: Decode stops at one
// and reports which it was, so an image can hand the bytes to a decoder
// that knows the format.
func (s *Stream) Decode() ([]byte, error) {
	data, _, err := s.decode()
	return data, err
}

// DecodeImage is Decode for image streams: it also reports the image codec
// the chain ended at ("DCTDecode", "JPXDecode") or "" when the bytes are
// fully decoded raw samples.
func (s *Stream) DecodeImage() ([]byte, string, error) { return s.decode() }

func (s *Stream) decode() ([]byte, string, error) {
	data := s.Raw
	filters := s.filterNames()
	parms := s.decodeParms(len(filters))
	for i, f := range filters {
		switch f {
		case "FlateDecode", "Fl":
			out, err := inflate(data)
			if err != nil {
				return nil, "", err
			}
			data = applyPredictor(out, parms[i], s.doc)
		case "LZWDecode", "LZW":
			early := 1
			if p := parms[i]; p != nil {
				if v, ok := numOf(s.doc.Resolve(p["EarlyChange"])); ok {
					early = int(v)
				}
			}
			data = applyPredictor(lzwDecode(data, early), parms[i], s.doc)
		case "ASCIIHexDecode", "AHx":
			data = asciiHexDecode(data)
		case "ASCII85Decode", "A85":
			data = ascii85Decode(data)
		case "RunLengthDecode", "RL":
			data = runLengthDecode(data)
		case "DCTDecode", "DCT", "JPXDecode":
			return data, string(f), nil
		case "Crypt":
			// Identity crypt filters are a no-op; anything else means the
			// file is encrypted, which Load refuses up front.
		default:
			return nil, "", fmt.Errorf("pdf: unsupported filter %s", f)
		}
	}
	return data, "", nil
}

// filterNames is the stream's filter chain, as names.
func (s *Stream) filterNames() []Name {
	var out []Name
	switch f := s.doc.Resolve(s.Dict["Filter"]).(type) {
	case Name:
		out = append(out, f)
	case Array:
		for _, o := range f {
			if n, ok := s.doc.Resolve(o).(Name); ok {
				out = append(out, n)
			}
		}
	}
	return out
}

// decodeParms is the per-filter parameter dictionary list, padded to n.
func (s *Stream) decodeParms(n int) []Dict {
	out := make([]Dict, n)
	var raw Object = s.Dict["DecodeParms"]
	if raw == nil {
		raw = s.Dict["DP"]
	}
	switch p := s.doc.Resolve(raw).(type) {
	case Dict:
		if n > 0 {
			out[0] = p
		}
	case Array:
		for i := 0; i < n && i < len(p); i++ {
			if d, ok := s.doc.Resolve(p[i]).(Dict); ok {
				out[i] = d
			}
		}
	}
	return out
}

// flate inflates zlib data, tolerating the missing header some producers
// emit and returning whatever was decoded before a truncation error (a
// clipped stream still draws most of its page).
func inflate(b []byte) ([]byte, error) {
	for _, body := range [][]byte{b, skipToZlib(b)} {
		if len(body) == 0 {
			continue
		}
		zr, err := zlib.NewReader(bytes.NewReader(body))
		if err != nil {
			continue
		}
		out, err := io.ReadAll(zr)
		zr.Close()
		if len(out) > 0 || err == nil {
			return out, nil
		}
	}
	// No zlib header at all: try a bare deflate stream.
	if out, err := rawInflate(b); err == nil || len(out) > 0 {
		return out, nil
	}
	return nil, fmt.Errorf("pdf: FlateDecode: not a zlib stream")
}

// skipToZlib finds a zlib header a few bytes in — some writers leave
// whitespace or a stray byte between "stream" and the data.
func skipToZlib(b []byte) []byte {
	for i := 0; i < len(b) && i < 4; i++ {
		if b[i] == 0x78 { // CMF for deflate with a 32K window
			return b[i:]
		}
	}
	return nil
}

// asciiHexDecode reads hex digit pairs up to '>'.
func asciiHexDecode(b []byte) []byte {
	var out []byte
	var hi byte
	half := false
	for _, c := range b {
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		case c == '>':
			if half {
				out = append(out, hi<<4)
			}
			return out
		default:
			continue
		}
		if half {
			out = append(out, hi<<4|v)
			half = false
		} else {
			hi, half = v, true
		}
	}
	if half {
		out = append(out, hi<<4)
	}
	return out
}

// ascii85Decode reads base-85 groups up to "~>".
func ascii85Decode(b []byte) []byte {
	var out []byte
	var group [5]byte
	n := 0
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case isWhite(c):
			continue
		case c == '~':
			goto flush
		case c == 'z' && n == 0:
			out = append(out, 0, 0, 0, 0)
			continue
		case c < '!' || c > 'u':
			continue
		}
		group[n] = c - '!'
		n++
		if n == 5 {
			v := uint32(0)
			for _, g := range group {
				v = v*85 + uint32(g)
			}
			out = append(out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
			n = 0
		}
	}
flush:
	if n > 1 {
		// A partial group encodes n-1 bytes; pad with 'u' (84) as the spec says.
		for i := n; i < 5; i++ {
			group[i] = 84
		}
		v := uint32(0)
		for _, g := range group {
			v = v*85 + uint32(g)
		}
		full := []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
		out = append(out, full[:n-1]...)
	}
	return out
}

// runLengthDecode expands the byte-run encoding: a length < 128 copies the
// next length+1 bytes, > 128 repeats the next byte 257-length times, and
// 128 ends the data.
func runLengthDecode(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); {
		n := int(b[i])
		i++
		switch {
		case n == 128:
			return out
		case n < 128:
			end := min(i+n+1, len(b))
			out = append(out, b[i:end]...)
			i = end
		default:
			if i >= len(b) {
				return out
			}
			for j := 0; j < 257-n; j++ {
				out = append(out, b[i])
			}
			i++
		}
	}
	return out
}

// applyPredictor undoes the PNG/TIFF predictors a Flate or LZW stream may
// have been filtered through (xref streams and image data both use them).
func applyPredictor(data []byte, parms Dict, doc *Document) []byte {
	if parms == nil {
		return data
	}
	pred, ok := numOf(doc.Resolve(parms["Predictor"]))
	if !ok || pred < 2 {
		return data
	}
	colors := intOr(doc, parms["Colors"], 1)
	bpc := intOr(doc, parms["BitsPerComponent"], 8)
	columns := intOr(doc, parms["Columns"], 1)
	bpp := max((colors*bpc+7)/8, 1)        // bytes per pixel
	rowLen := (columns*colors*bpc + 7) / 8 // bytes per row
	if int(pred) == 2 {
		return tiffPredictor(data, rowLen, bpp, bpc)
	}
	// PNG predictors: each row is prefixed by its filter type.
	var out []byte
	prev := make([]byte, rowLen)
	for pos := 0; pos+1 <= len(data); pos += rowLen + 1 {
		ft := data[pos]
		end := min(pos+1+rowLen, len(data))
		row := make([]byte, rowLen)
		copy(row, data[pos+1:end])
		switch ft {
		case 1: // Sub
			for i := bpp; i < rowLen; i++ {
				row[i] += row[i-bpp]
			}
		case 2: // Up
			for i := 0; i < rowLen; i++ {
				row[i] += prev[i]
			}
		case 3: // Average
			for i := 0; i < rowLen; i++ {
				left := 0
				if i >= bpp {
					left = int(row[i-bpp])
				}
				row[i] += byte((left + int(prev[i])) / 2)
			}
		case 4: // Paeth
			for i := 0; i < rowLen; i++ {
				var a, c int
				if i >= bpp {
					a, c = int(row[i-bpp]), int(prev[i-bpp])
				}
				b := int(prev[i])
				row[i] += byte(paeth(a, b, c))
			}
		}
		out = append(out, row...)
		prev = row
		if end == len(data) {
			break
		}
	}
	return out
}

// tiffPredictor undoes TIFF predictor 2 (horizontal differencing).
func tiffPredictor(data []byte, rowLen, bpp, bpc int) []byte {
	if bpc != 8 { // sub-byte components: left as-is rather than guessed at
		return data
	}
	for r := 0; r+rowLen <= len(data); r += rowLen {
		row := data[r : r+rowLen]
		for i := bpp; i < rowLen; i++ {
			row[i] += row[i-bpp]
		}
	}
	return data
}

func paeth(a, b, c int) int {
	p := a + b - c
	pa, pb, pc := abs(p-a), abs(p-b), abs(p-c)
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	}
	return c
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
