package render

// Qt's compiled-shader container (.qsb) and the reflection a QML
// ShaderEffect needs to feed it.
//
// A .qsb is what a Qt app ships instead of GLSL: the qsb tool compiles the
// source once and bundles the variants (SPIR-V, GLSL, HLSL, MSL) plus
// reflection. The SPIR-V variant is the one we can use — wgpu runs SPIR-V
// through naga's frontend like any other shader — so nothing here needs a
// GLSL compiler at run time.
//
// Two readers live here: the container (zlib around a Qt data stream, from
// which the SPIR-V blob is lifted) and enough of a SPIR-V parser to answer
// "what is in the uniform block, at what offset, and which bindings are
// textures". That is all a ShaderEffect needs: Qt's convention puts
// qt_Matrix and qt_Opacity at the head of a std140 block at binding 0, and
// the effect's own properties after them by NAME.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// ShaderStage is which pipeline stage a compiled shader belongs to.
type ShaderStage int

const (
	StageVertex ShaderStage = iota
	StageFragment
	StageOther
)

// QSB is one parsed .qsb file: the SPIR-V and what it declares.
type QSB struct {
	Stage      ShaderStage
	SPIRV      []byte
	EntryPoint string
	// Uniforms is the uniform block's members in declaration order, with
	// their std140 offsets; Size is the block's size in bytes.
	Uniforms []ShaderUniform
	Size     int
	// Textures are the sampled images the shader declares, by binding.
	Textures []ShaderTexture
	Inputs   []ShaderInput // vertex inputs, by location
}

// ShaderUniform is one member of a shader's uniform block.
type ShaderUniform struct {
	Name   string
	Offset int
	Kind   UniformKind
}

// UniformKind is the shape of a uniform member — what a value has to be
// converted to before it is written.
type UniformKind int

const (
	UniformFloat UniformKind = iota
	UniformVec2
	UniformVec3
	UniformVec4
	UniformMat4
	UniformInt
	UniformBool
	UniformUnknown
)

// Size is the member's byte size (std140 rules for the shapes we model).
func (k UniformKind) Size() int {
	switch k {
	case UniformVec2:
		return 8
	case UniformVec3:
		return 12
	case UniformVec4:
		return 16
	case UniformMat4:
		return 64
	default:
		return 4
	}
}

// ShaderTexture is a sampled image the shader declares.
type ShaderTexture struct {
	Name    string
	Binding int
}

// ShaderInput is a vertex input (qt_Vertex, qt_MultiTexCoord0).
type ShaderInput struct {
	Name     string
	Location int
	Kind     UniformKind
}

// ParseQSB reads a .qsb file: it is zlib-compressed behind a 4-byte
// big-endian uncompressed size (Qt's qCompress), and the SPIR-V variant
// sits inside as a length-prefixed blob. Rather than track the container's
// versioned layout, the SPIR-V is found by its own magic word with the Qt
// byte-array length that precedes it — true for every qsb version.
func ParseQSB(data []byte) (*QSB, error) {
	raw, err := qsbPayload(data)
	if err != nil {
		return nil, err
	}
	if len(raw) < 8 {
		return nil, fmt.Errorf("render: qsb: %d bytes is too short", len(raw))
	}
	q := &QSB{EntryPoint: "main"}
	switch binary.BigEndian.Uint32(raw[4:8]) { // QShader::Stage
	case 0:
		q.Stage = StageVertex
	case 4:
		q.Stage = StageFragment
	default:
		q.Stage = StageOther
	}
	spv, err := findSPIRV(raw)
	if err != nil {
		return nil, err
	}
	q.SPIRV = spv
	reflectSPIRV(spv, q)
	nameMembers(raw, q)
	return q, nil
}

// nameMembers fills in the uniform members' NAMES. qsb strips debug info
// from the SPIR-V, so the names live in the container's own reflection
// (QShaderDescription): a record is a UTF-16BE string behind a big-endian
// byte length, followed by its type, offset and size. Rather than track
// that structure's versioned sections, each record is matched to the block
// member the SPIR-V already reported at the same offset and size — a
// vertex input's "size" field is not a size at all (0 or ~0), so those
// records cannot collide with a member.
func nameMembers(raw []byte, q *QSB) {
	if len(q.Uniforms) == 0 {
		return
	}
	type rec struct {
		offset, size int
		name         string
	}
	var recs []rec
	for i := 0; i+8 < len(raw); {
		n := int(binary.BigEndian.Uint32(raw[i:]))
		if n < 2 || n > 128 || n%2 != 0 || i+4+n+16 > len(raw) {
			i++
			continue
		}
		name, ok := utf16BEString(raw[i+4 : i+4+n])
		if !ok {
			i++
			continue
		}
		at := i + 4 + n
		recs = append(recs, rec{
			offset: int(binary.BigEndian.Uint32(raw[at+4:])),
			size:   int(binary.BigEndian.Uint32(raw[at+8:])),
			name:   name,
		})
		i = at
	}
	const none = int(^uint32(0)) // the record's "not applicable" marker
	for i := range q.Uniforms {
		u := &q.Uniforms[i]
		for _, r := range recs {
			if r.offset == u.Offset && r.size == u.Kind.Size() {
				u.Name = r.name
				break
			}
		}
	}
	// A sampler's record carries no offset and its BINDING where a member
	// carries its size; a vertex input carries its location and no size.
	for i := range q.Textures {
		t := &q.Textures[i]
		for _, r := range recs {
			if r.offset == none && r.size == t.Binding {
				t.Name = r.name
				break
			}
		}
	}
	for i := range q.Inputs {
		in := &q.Inputs[i]
		if in.Name != "" {
			continue
		}
		for _, r := range recs {
			if r.offset == in.Location && r.size == none {
				in.Name = r.name
				break
			}
		}
	}
}

// utf16BEString decodes a big-endian UTF-16 name, rejecting anything that
// is not printable ASCII — the names in a shader are identifiers, and this
// is scanning raw bytes for them.
func utf16BEString(b []byte) (string, bool) {
	out := make([]byte, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if b[i] != 0 {
			return "", false
		}
		c := b[i+1]
		if c < 0x20 || c >= 0x7f {
			return "", false
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return "", false
	}
	return string(out), true
}

// qsbPayload strips Qt's qCompress wrapper. An uncompressed file (some
// versions, and hand-built ones) is returned as it is.
func qsbPayload(data []byte) ([]byte, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("render: qsb: %d bytes is too short", len(data))
	}
	if data[4] == 0x78 { // zlib header behind the 4-byte size
		zr, err := zlib.NewReader(bytes.NewReader(data[4:]))
		if err != nil {
			return nil, fmt.Errorf("render: qsb: %v", err)
		}
		defer zr.Close()
		out, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("render: qsb: %v", err)
		}
		return out, nil
	}
	return data, nil
}

// findSPIRV locates the SPIR-V variant: its magic word, with the Qt
// byte-array length (big-endian) immediately before it.
func findSPIRV(raw []byte) ([]byte, error) {
	magic := []byte{0x03, 0x02, 0x23, 0x07} // 0x07230203, little-endian
	for at := 0; ; {
		i := bytes.Index(raw[at:], magic)
		if i < 0 {
			return nil, fmt.Errorf("render: qsb: no SPIR-V variant")
		}
		i += at
		if i >= 4 {
			n := int(binary.BigEndian.Uint32(raw[i-4 : i]))
			if n >= 20 && n%4 == 0 && i+n <= len(raw) {
				return raw[i : i+n], nil
			}
		}
		at = i + 4
	}
}

// SPIR-V opcodes and decorations this reader needs.
const (
	opName             = 5
	opMemberName       = 6
	opTypeInt          = 21
	opTypeFloat        = 22
	opTypeVector       = 23
	opTypeMatrix       = 24
	opTypeImage        = 25
	opTypeSampledImage = 27
	opTypePointer      = 32
	opTypeStruct       = 30
	opTypeBool         = 20
	opVariable         = 59
	opDecorate         = 71
	opMemberDecorate   = 72

	decBlock    = 2
	decLocation = 30
	decBinding  = 33
	decOffset   = 35

	storageUniformConstant = 0 // UniformConstant (samplers)
	storageInput           = 1
	storageUniform         = 2 // Uniform (blocks)
)

// reflectSPIRV fills in the uniform block, the sampled images and the
// vertex inputs. It walks the instruction stream once, collecting names,
// decorations and types, then resolves the variables it cares about.
func reflectSPIRV(spv []byte, q *QSB) {
	if len(spv) < 20 {
		return
	}
	words := make([]uint32, len(spv)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(spv[4*i:])
	}
	type memberDec struct{ offset int }
	names := map[uint32]string{}
	memberNames := map[uint32]map[uint32]string{}
	memberOffsets := map[uint32]map[uint32]memberDec{}
	binding := map[uint32]int{}
	location := map[uint32]int{}
	isBlock := map[uint32]bool{}
	// Types
	intW := map[uint32]bool{}
	boolW := map[uint32]bool{}
	floatW := map[uint32]bool{}
	vecOf := map[uint32][2]uint32{}   // id -> {component type, count}
	matCols := map[uint32][2]uint32{} // id -> {column type, count}
	structOf := map[uint32][]uint32{} // id -> member type ids
	ptrTo := map[uint32][2]uint32{}   // id -> {storage class, type}
	sampled := map[uint32]bool{}      // sampled-image type ids
	varOf := map[uint32][2]uint32{}   // id -> {type id, storage class}

	for i := 5; i < len(words); {
		wc := int(words[i] >> 16)
		op := words[i] & 0xffff
		if wc == 0 || i+wc > len(words) {
			break
		}
		w := words[i : i+wc]
		switch op {
		case opName:
			if wc >= 3 {
				names[w[1]] = spvString(w[2:])
			}
		case opMemberName:
			if wc >= 4 {
				if memberNames[w[1]] == nil {
					memberNames[w[1]] = map[uint32]string{}
				}
				memberNames[w[1]][w[2]] = spvString(w[3:])
			}
		case opDecorate:
			if wc >= 3 {
				switch w[2] {
				case decBlock:
					isBlock[w[1]] = true
				case decBinding:
					if wc >= 4 {
						binding[w[1]] = int(w[3])
					}
				case decLocation:
					if wc >= 4 {
						location[w[1]] = int(w[3])
					}
				}
			}
		case opMemberDecorate:
			if wc >= 5 && w[3] == decOffset {
				if memberOffsets[w[1]] == nil {
					memberOffsets[w[1]] = map[uint32]memberDec{}
				}
				memberOffsets[w[1]][w[2]] = memberDec{offset: int(w[4])}
			}
		case opTypeInt:
			intW[w[1]] = true
		case opTypeBool:
			boolW[w[1]] = true
		case opTypeFloat:
			floatW[w[1]] = true
		case opTypeVector:
			if wc >= 4 {
				vecOf[w[1]] = [2]uint32{w[2], w[3]}
			}
		case opTypeMatrix:
			if wc >= 4 {
				matCols[w[1]] = [2]uint32{w[2], w[3]}
			}
		case opTypeStruct:
			structOf[w[1]] = append([]uint32{}, w[2:]...)
		case opTypePointer:
			if wc >= 4 {
				ptrTo[w[1]] = [2]uint32{w[2], w[3]}
			}
		case opTypeImage, opTypeSampledImage:
			sampled[w[1]] = true
		case opVariable:
			if wc >= 4 {
				varOf[w[2]] = [2]uint32{w[1], w[3]}
			}
		}
		i += wc
	}

	kindOf := func(t uint32) UniformKind {
		switch {
		case floatW[t]:
			return UniformFloat
		case intW[t]:
			return UniformInt
		case boolW[t]:
			return UniformBool
		}
		if v, ok := vecOf[t]; ok {
			switch v[1] {
			case 2:
				return UniformVec2
			case 3:
				return UniformVec3
			case 4:
				return UniformVec4
			}
		}
		if m, ok := matCols[t]; ok && m[1] == 4 {
			return UniformMat4
		}
		return UniformUnknown
	}

	for id, v := range varOf {
		ptrID, storage := v[0], v[1]
		p, ok := ptrTo[ptrID]
		if !ok {
			continue
		}
		typ := p[1]
		switch storage {
		case storageUniform:
			members, ok := structOf[typ]
			if !ok || !isBlock[typ] {
				continue
			}
			size := 0
			for mi, mt := range members {
				k := kindOf(mt)
				off := 0
				if d, ok := memberOffsets[typ][uint32(mi)]; ok {
					off = d.offset
				}
				q.Uniforms = append(q.Uniforms, ShaderUniform{
					Name:   memberNames[typ][uint32(mi)],
					Offset: off,
					Kind:   k,
				})
				if end := off + k.Size(); end > size {
					size = end
				}
			}
			q.Size = (size + 15) / 16 * 16 // std140 blocks round to 16
		case storageUniformConstant:
			if sampled[typ] {
				q.Textures = append(q.Textures, ShaderTexture{
					Name: names[id], Binding: binding[id],
				})
			}
		case storageInput:
			if k := kindOf(typ); k != UniformUnknown {
				q.Inputs = append(q.Inputs, ShaderInput{
					Name: names[id], Location: location[id], Kind: k,
				})
			}
		}
	}
	return
}

// spvString reads a SPIR-V literal string (NUL-terminated, packed
// little-endian into words).
func spvString(words []uint32) string {
	var b []byte
	for _, w := range words {
		for s := 0; s < 4; s++ {
			c := byte(w >> (8 * s))
			if c == 0 {
				return string(b)
			}
			b = append(b, c)
		}
	}
	return string(b)
}

// --- combined image samplers -------------------------------------------
//
// GLSL's `sampler2D` compiles to a SPIR-V COMBINED image sampler: one
// variable of type OpTypeSampledImage. WebGPU has no such binding — a
// texture and a sampler are separate — and naga rejects the module
// outright, which is why a Qt ShaderEffect that samples anything could not
// run here at all.
//
// SplitCombinedSamplers rewrites the module the way a Vulkan-style shader
// would have been written in the first place: the variable becomes an
// IMAGE, a SAMPLER variable is added beside it, and every load of the
// combined variable becomes load-image + load-sampler + OpSampledImage,
// keeping the original result id so nothing downstream changes.

const (
	opTypeSampler        = 26
	opSampledImage       = 86
	opLoad               = 61
	opFunction           = 54
	opDecorateGroup      = 74 // last of the annotation opcodes
	decDescriptorSet     = 34
	storageUniformConst2 = 0
)

// SamplerSplit reports where the sampler for a texture binding landed.
type SamplerSplit struct {
	TextureBinding int
	SamplerBinding int
}

// SplitCombinedSamplers returns an equivalent module with separate image
// and sampler bindings, plus where each sampler was put. A module with no
// combined samplers comes back unchanged.
func SplitCombinedSamplers(spv []byte) ([]byte, []SamplerSplit, error) {
	if len(spv) < 20 || len(spv)%4 != 0 {
		return spv, nil, fmt.Errorf("render: spirv: %d bytes", len(spv))
	}
	words := make([]uint32, len(spv)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(spv[4*i:])
	}
	// One pass to learn the shape of the module.
	type instr struct{ at, wc int }
	var instrs []instr
	sampledImageOf := map[uint32]uint32{} // OpTypeSampledImage id -> image type
	ptrTo := map[uint32][2]uint32{}       // pointer id -> {storage, pointee}
	varPtr := map[uint32]uint32{}         // variable id -> pointer type
	bindingOf := map[uint32]uint32{}
	setOf := map[uint32]uint32{}
	samplerType := uint32(0)
	ptrToSampler := uint32(0)
	ptrToImage := map[uint32]uint32{} // image type -> existing pointer
	firstFunction, lastAnnotation, firstVariable := -1, -1, -1
	maxBinding := uint32(0)
	for i := 5; i < len(words); {
		wc := int(words[i] >> 16)
		op := words[i] & 0xffff
		if wc == 0 || i+wc > len(words) {
			return spv, nil, fmt.Errorf("render: spirv: truncated at word %d", i)
		}
		w := words[i : i+wc]
		instrs = append(instrs, instr{at: i, wc: wc})
		switch op {
		case opTypeSampledImage:
			if wc >= 3 {
				sampledImageOf[w[1]] = w[2]
			}
		case opTypeSampler:
			samplerType = w[1]
		case opTypePointer:
			if wc >= 4 {
				ptrTo[w[1]] = [2]uint32{w[2], w[3]}
				if w[2] == storageUniformConst2 {
					if w[3] == samplerType && samplerType != 0 {
						ptrToSampler = w[1]
					}
					ptrToImage[w[3]] = w[1]
				}
			}
		case opVariable:
			if wc >= 4 {
				varPtr[w[2]] = w[1]
			}
			if firstVariable < 0 {
				firstVariable = len(instrs) - 1
			}
		case opDecorate:
			if wc >= 4 {
				switch w[2] {
				case decBinding:
					bindingOf[w[1]] = w[3]
					if w[3] > maxBinding {
						maxBinding = w[3]
					}
				case decDescriptorSet:
					setOf[w[1]] = w[3]
				}
			}
		}
		if op >= opDecorate && op <= opDecorateGroup {
			lastAnnotation = len(instrs) - 1
		}
		if op == opFunction && firstFunction < 0 {
			firstFunction = len(instrs) - 1
		}
		i += wc
	}
	// Which variables are combined samplers?
	type combined struct {
		varID, siType, imgType     uint32
		imgPtr, smpPtr, smpVar     uint32
		samplerBinding, texBinding uint32
	}
	var subs []combined
	for v, p := range varPtr {
		pt, ok := ptrTo[p]
		if !ok || pt[0] != storageUniformConst2 {
			continue
		}
		if img, ok := sampledImageOf[pt[1]]; ok {
			subs = append(subs, combined{varID: v, siType: pt[1], imgType: img,
				texBinding: bindingOf[v]})
		}
	}
	if len(subs) == 0 {
		return spv, nil, nil
	}
	// Stable order, so the bindings a rebuild hands out do not shuffle.
	sortSlice(subs, func(a, b combined) bool { return a.texBinding < b.texBinding })
	next := words[3] // the id bound: fresh ids start here
	newID := func() uint32 { id := next; next++; return id }
	var newTypes [][]uint32
	var newAnnots [][]uint32
	var splits []SamplerSplit
	if samplerType == 0 {
		samplerType = newID()
		newTypes = append(newTypes, []uint32{uint32(2<<16) | opTypeSampler, samplerType})
	}
	if ptrToSampler == 0 {
		ptrToSampler = newID()
		newTypes = append(newTypes, []uint32{uint32(4<<16) | opTypePointer,
			ptrToSampler, storageUniformConst2, samplerType})
	}
	for i := range subs {
		s := &subs[i]
		if p, ok := ptrToImage[s.imgType]; ok && ptrTo[p][1] == s.imgType {
			s.imgPtr = p
		} else {
			s.imgPtr = newID()
			newTypes = append(newTypes, []uint32{uint32(4<<16) | opTypePointer,
				s.imgPtr, storageUniformConst2, s.imgType})
		}
		s.smpPtr = ptrToSampler
		s.smpVar = newID()
		maxBinding++
		s.samplerBinding = maxBinding
		// Both variables are re-declared in the inserted block (the old
		// declaration is dropped below): its pointer type may be defined
		// AFTER the original variable, and a type has to precede its use.
		newTypes = append(newTypes,
			[]uint32{uint32(4<<16) | opVariable, s.imgPtr, s.varID, storageUniformConst2},
			[]uint32{uint32(4<<16) | opVariable, s.smpPtr, s.smpVar, storageUniformConst2})
		newAnnots = append(newAnnots,
			[]uint32{uint32(4<<16) | opDecorate, s.smpVar, decBinding, s.samplerBinding},
			[]uint32{uint32(4<<16) | opDecorate, s.smpVar, decDescriptorSet, setOf[s.varID]})
		splits = append(splits, SamplerSplit{
			TextureBinding: int(s.texBinding), SamplerBinding: int(s.samplerBinding),
		})
	}
	find := func(v uint32) *combined {
		for i := range subs {
			if subs[i].varID == v {
				return &subs[i]
			}
		}
		return nil
	}
	// Rebuild: header, then every instruction, with the new declarations
	// spliced into their sections and each combined load expanded.
	// Where the declarations go: at the END of the globals section, just
	// before the first function, where every type they name is defined.
	typeInsertAfter := firstFunction - 1
	out := append([]uint32{}, words[:5]...)
	for idx, in := range instrs {
		w := words[in.at : in.at+in.wc]
		op := w[0] & 0xffff
		switch {
		case op == opVariable && find(w[2]) != nil:
			// Dropped here and re-declared with the image pointer type in
			// the block before the first function.
		case op == opLoad && in.wc >= 4 && find(w[3]) != nil:
			s := find(w[3])
			img, smp := newID(), newID()
			out = append(out,
				uint32(4<<16)|opLoad, s.imgType, img, s.varID,
				uint32(4<<16)|opLoad, samplerType, smp, s.smpVar,
				// The original result id, so every use downstream stands.
				uint32(5<<16)|opSampledImage, s.siType, w[2], img, smp)
		default:
			out = append(out, w...)
		}
		if idx == lastAnnotation {
			for _, a := range newAnnots {
				out = append(out, a...)
			}
		}
		// The new types have to precede the variable that uses them: the
		// rewritten image variable sits with the module's other globals,
		// so the block goes in just before the FIRST of them (a module
		// with none at all would have no combined sampler either).
		if idx == typeInsertAfter {
			for _, t := range newTypes {
				out = append(out, t...)
			}
		}
	}
	out[3] = next // the new id bound
	b := make([]byte, len(out)*4)
	for i, w := range out {
		binary.LittleEndian.PutUint32(b[4*i:], w)
	}
	return b, splits, nil
}

// sortSlice is a tiny insertion sort: the slices here are a handful of
// entries, and this keeps the dependency surface at zero.
func sortSlice[T any](s []T, less func(a, b T) bool) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && less(s[j], s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// PadUniformBlock grows a module's uniform block to at least size bytes by
// appending float members.
//
// wgpu derives a pipeline's bind group layout from BOTH stages and insists
// they agree: a uniform buffer's minimum binding size comes from the
// block's declared size, and Qt compiles each stage against only the
// members it uses — a vertex shader that just multiplies by qt_Matrix ends
// up with a shorter block than its fragment shader, and the pipeline is
// rejected as "not consistent between stages". Padding the shorter one
// makes them agree without changing what either shader reads.
func PadUniformBlock(spv []byte, size int) ([]byte, error) {
	if len(spv) < 20 || len(spv)%4 != 0 {
		return spv, fmt.Errorf("render: spirv: %d bytes", len(spv))
	}
	words := make([]uint32, len(spv)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(spv[4*i:])
	}
	// Find the uniform block: a struct decorated Block behind a Uniform
	// pointer, its current size, and a float type to pad with.
	isBlock := map[uint32]bool{}
	memberOffset := map[uint32]map[uint32]int{}
	structOf := map[uint32][]uint32{}
	ptrTo := map[uint32][2]uint32{}
	varPtr := map[uint32]uint32{}
	floatType, floatWidth := uint32(0), 0
	kindSize := map[uint32]int{}
	vecOf := map[uint32][2]uint32{}
	matOf := map[uint32][2]uint32{}
	lastAnnotation, firstType := -1, -1
	var instrs [][]uint32
	for i := 5; i < len(words); {
		wc := int(words[i] >> 16)
		op := words[i] & 0xffff
		if wc == 0 || i+wc > len(words) {
			return spv, fmt.Errorf("render: spirv: truncated at word %d", i)
		}
		w := words[i : i+wc]
		instrs = append(instrs, w)
		switch op {
		case opDecorate:
			if wc >= 3 && w[2] == decBlock {
				isBlock[w[1]] = true
			}
		case opMemberDecorate:
			if wc >= 5 && w[3] == decOffset {
				if memberOffset[w[1]] == nil {
					memberOffset[w[1]] = map[uint32]int{}
				}
				memberOffset[w[1]][w[2]] = int(w[4])
			}
		case opTypeFloat:
			if wc >= 3 && int(w[2]) >= floatWidth {
				floatType, floatWidth = w[1], int(w[2])
			}
			kindSize[w[1]] = 4
		case opTypeInt, opTypeBool:
			kindSize[w[1]] = 4
		case opTypeVector:
			if wc >= 4 {
				vecOf[w[1]] = [2]uint32{w[2], w[3]}
			}
		case opTypeMatrix:
			if wc >= 4 {
				matOf[w[1]] = [2]uint32{w[2], w[3]}
			}
		case opTypeStruct:
			structOf[w[1]] = append([]uint32{}, w[2:]...)
		case opTypePointer:
			if wc >= 4 {
				ptrTo[w[1]] = [2]uint32{w[2], w[3]}
			}
		case opVariable:
			if wc >= 4 {
				varPtr[w[2]] = w[1]
			}
		}
		if op >= opDecorate && op <= opDecorateGroup {
			lastAnnotation = len(instrs) - 1
		}
		if firstType < 0 && op >= opTypeVoid && op <= opTypeForwardPointer {
			firstType = len(instrs) - 1
		}
		i += wc
	}
	sizeOf := func(t uint32) int {
		if s, ok := kindSize[t]; ok {
			return s
		}
		if v, ok := vecOf[t]; ok {
			return int(v[1]) * 4
		}
		if m, ok := matOf[t]; ok {
			return int(m[1]) * 16
		}
		return 4
	}
	block, blockVar := uint32(0), uint32(0)
	for v, p := range varPtr {
		pt, ok := ptrTo[p]
		if !ok || pt[0] != storageUniform {
			continue
		}
		if isBlock[pt[1]] {
			block, blockVar = pt[1], v
		}
	}
	_ = blockVar
	if block == 0 || floatType == 0 {
		return spv, nil
	}
	members := structOf[block]
	end := 0
	for i, mt := range members {
		if off, ok := memberOffset[block][uint32(i)]; ok {
			if e := off + sizeOf(mt); e > end {
				end = e
			}
		}
	}
	if end >= size {
		return spv, nil
	}
	// Append one float member per 4 bytes still missing.
	var pad []uint32
	offsets := []int{}
	for off := end; off < size; off += 4 {
		pad = append(pad, floatType)
		offsets = append(offsets, off)
	}
	out := append([]uint32{}, words[:5]...)
	for idx, w := range instrs {
		op := w[0] & 0xffff
		switch {
		case op == opTypeStruct && w[1] == block:
			ext := append(append([]uint32{}, w...), pad...)
			ext[0] = uint32(len(ext)<<16) | opTypeStruct
			out = append(out, ext...)
		default:
			out = append(out, w...)
		}
		if idx == lastAnnotation {
			for i, off := range offsets {
				out = append(out, uint32(5<<16)|opMemberDecorate,
					block, uint32(len(members)+i), decOffset, uint32(off))
			}
		}
	}
	b := make([]byte, len(out)*4)
	for i, w := range out {
		binary.LittleEndian.PutUint32(b[4*i:], w)
	}
	return b, nil
}

// The type opcodes bound the type section (OpTypeVoid..
// OpTypeForwardPointer), which is how it is recognised here.
const (
	opTypeVoid           = 19
	opTypeForwardPointer = 39
)
