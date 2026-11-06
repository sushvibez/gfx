package pdf

import (
	"bytes"
	"compress/flate"
	"io"
)

// rawInflate reads a headerless deflate stream — some producers write one
// where a zlib stream belongs. Whatever decoded before an error is kept: a
// truncated content stream still draws most of its page.
func rawInflate(b []byte) ([]byte, error) {
	fr := flate.NewReader(bytes.NewReader(b))
	defer fr.Close()
	out, err := io.ReadAll(fr)
	if len(out) > 0 {
		return out, nil
	}
	return out, err
}
