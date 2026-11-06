// Package fbx reads Autodesk FBX (binary) into a node tree and extracts the
// meshes, materials (Phong subset + embedded diffuse textures), skeleton,
// skinning and animation clips a skinned character needs — enough of the
// format to load Mixamo-style rigs (Mixamo exports), which glTF would
// otherwise be the only supported path for. Pure Go, no cgo: the binary
// container is a documented record tree, array payloads are zlib
// (compress/zlib), embedded textures are PNG/JPEG. ASCII FBX is not
// supported (Mixamo/Maya export binary), and neither are textures stored as
// external files (Parse sees bytes only, with no directory to resolve
// relative paths against).
package fbx

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Node is one record in the FBX tree: a name, a flat property list, and
// nested child records.
type Node struct {
	Name     string
	Props    []Prop
	Children []*Node
}

// Prop is a single typed property value. Exactly one field is meaningful,
// selected by Kind (the FBX type code).
type Prop struct {
	Kind byte // Y C I F D L (scalars) f d l i b (arrays) S R (string/raw)
	I    int64
	F    float64
	S    string
	Arr  []float64 // numeric arrays (f/d/l/i/b) widened to float64
	Raw  []byte    // R
}

// Doc is a parsed FBX file: the version and the synthetic root's children.
type Doc struct {
	Version uint32
	Root    *Node
}

// magic is the 21-byte binary FBX signature.
var magic = []byte("Kaydara FBX Binary  ")

// Parse reads a binary FBX document.
func Parse(data []byte) (*Doc, error) {
	if len(data) < 27 || !bytes.HasPrefix(data, magic) {
		return nil, fmt.Errorf("fbx: not a binary FBX (bad magic)")
	}
	r := &reader{buf: data}
	r.pos = 23 // 21 magic + 2 bytes (0x1a 0x00)
	version := r.u32()
	// FBX 7500+ widens the record header offsets/counts to 64-bit.
	wide := version >= 7500

	root := &Node{Name: "<root>"}
	for {
		n, end, err := r.node(wide)
		if err != nil {
			return nil, err
		}
		if end {
			break
		}
		if n != nil {
			root.Children = append(root.Children, n)
		}
	}
	return &Doc{Version: version, Root: root}, nil
}

type reader struct {
	buf []byte
	pos int
	err error
}

func (r *reader) u8() uint8 {
	if r.pos+1 > len(r.buf) {
		r.fail()
		return 0
	}
	v := r.buf[r.pos]
	r.pos++
	return v
}

func (r *reader) u32() uint32 {
	if r.pos+4 > len(r.buf) {
		r.fail()
		return 0
	}
	v := binary.LittleEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v
}

func (r *reader) u64() uint64 {
	if r.pos+8 > len(r.buf) {
		r.fail()
		return 0
	}
	v := binary.LittleEndian.Uint64(r.buf[r.pos:])
	r.pos += 8
	return v
}

func (r *reader) bytes(n int) []byte {
	if n < 0 || r.pos+n > len(r.buf) {
		r.fail()
		return nil
	}
	b := r.buf[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *reader) fail() {
	if r.err == nil {
		r.err = fmt.Errorf("fbx: truncated at %d", r.pos)
	}
}

// hdr reads a record header (offset width depends on the FBX version).
func (r *reader) hdr(wide bool) (endOffset, numProps, propListLen uint64) {
	if wide {
		return r.u64(), r.u64(), r.u64()
	}
	return uint64(r.u32()), uint64(r.u32()), uint64(r.u32())
}

// node reads one record and its subtree. end reports the null sentinel that
// terminates a sibling list.
func (r *reader) node(wide bool) (n *Node, end bool, err error) {
	start := r.pos
	endOffset, numProps, _ := r.hdr(wide)
	nameLen := int(r.u8())
	if r.err != nil {
		return nil, false, r.err
	}
	// A zero record header is the sibling-list terminator.
	if endOffset == 0 && numProps == 0 && nameLen == 0 {
		return nil, true, nil
	}
	name := string(r.bytes(nameLen))
	nd := &Node{Name: name}
	for i := uint64(0); i < numProps; i++ {
		p, e := r.prop()
		if e != nil {
			return nil, false, e
		}
		nd.Props = append(nd.Props, p)
	}
	// Nested records fill the gap up to endOffset (minus the 13/25-byte
	// terminator FBX writes when a record has children).
	for uint64(r.pos) < endOffset {
		child, cend, e := r.node(wide)
		if e != nil {
			return nil, false, e
		}
		if cend {
			break
		}
		if child != nil {
			nd.Children = append(nd.Children, child)
		}
	}
	// Jump to the declared end so trailing padding/terminators don't desync.
	if endOffset >= uint64(start) && endOffset <= uint64(len(r.buf)) {
		r.pos = int(endOffset)
	}
	return nd, false, r.err
}

func (r *reader) prop() (Prop, error) {
	kind := r.u8()
	if r.err != nil {
		return Prop{}, r.err
	}
	switch kind {
	case 'Y': // int16
		return Prop{Kind: kind, I: int64(int16(r.u16()))}, r.err
	case 'C': // bool (1 byte)
		return Prop{Kind: kind, I: int64(r.u8())}, r.err
	case 'I': // int32
		return Prop{Kind: kind, I: int64(int32(r.u32()))}, r.err
	case 'F': // float32
		return Prop{Kind: kind, F: float64(math.Float32frombits(r.u32()))}, r.err
	case 'D': // float64
		return Prop{Kind: kind, F: math.Float64frombits(r.u64())}, r.err
	case 'L': // int64
		return Prop{Kind: kind, I: int64(r.u64())}, r.err
	case 'f', 'd', 'l', 'i', 'b': // arrays
		return r.arrayProp(kind)
	case 'S', 'R': // string / raw
		n := int(r.u32())
		b := r.bytes(n)
		if kind == 'S' {
			return Prop{Kind: kind, S: string(b)}, r.err
		}
		return Prop{Kind: kind, Raw: append([]byte(nil), b...)}, r.err
	}
	return Prop{}, fmt.Errorf("fbx: unknown property type %q at %d", kind, r.pos)
}

func (r *reader) u16() uint16 {
	if r.pos+2 > len(r.buf) {
		r.fail()
		return 0
	}
	v := binary.LittleEndian.Uint16(r.buf[r.pos:])
	r.pos += 2
	return v
}

// arrayProp reads a numeric array property (optionally zlib-deflated) and
// widens every element to float64 for uniform downstream handling.
func (r *reader) arrayProp(kind byte) (Prop, error) {
	length := int(r.u32())
	encoding := r.u32()
	compLen := int(r.u32())
	if r.err != nil {
		return Prop{}, r.err
	}
	raw := r.bytes(compLen)
	if r.err != nil {
		return Prop{}, r.err
	}
	if encoding == 1 { // zlib
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return Prop{}, fmt.Errorf("fbx: zlib: %w", err)
		}
		dec, err := io.ReadAll(zr)
		zr.Close()
		if err != nil {
			return Prop{}, fmt.Errorf("fbx: inflate: %w", err)
		}
		raw = dec
	}
	arr := make([]float64, length)
	br := &reader{buf: raw}
	for i := 0; i < length; i++ {
		switch kind {
		case 'f':
			arr[i] = float64(math.Float32frombits(br.u32()))
		case 'd':
			arr[i] = math.Float64frombits(br.u64())
		case 'i':
			arr[i] = float64(int32(br.u32()))
		case 'l':
			arr[i] = float64(int64(br.u64()))
		case 'b':
			arr[i] = float64(br.u8())
		}
	}
	if br.err != nil {
		return Prop{}, br.err
	}
	return Prop{Kind: kind, Arr: arr}, nil
}
