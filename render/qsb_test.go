package render

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// A .qsb is what a Qt app ships instead of GLSL: zlib around a Qt data
// stream, with the SPIR-V variant inside and the reflection that names the
// uniform block's members. Both readers are exercised on a real file when
// one is available (the corpus' compiled shaders); the test is skipped
// where it is not, since nothing in the repo ships one.
func TestParseQSB(t *testing.T) {
	var path string
	for _, c := range []string{
		"../build/qml-compare/examples-ref/portal/shaders/portal.frag.qsb",
		"testdata/portal.frag.qsb",
	} {
		if _, err := os.Stat(c); err == nil {
			path = c
			break
		}
	}
	if path == "" {
		t.Skip("no .qsb available (run scripts/qml-compare.sh to compile the corpus')")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseQSB(b)
	if err != nil {
		t.Fatalf("ParseQSB(%s): %v", filepath.Base(path), err)
	}
	if q.Stage != StageFragment {
		t.Errorf("stage = %v, want fragment", q.Stage)
	}
	if len(q.SPIRV) < 100 || q.SPIRV[0] != 0x03 {
		t.Fatalf("SPIR-V is %d bytes starting %#x", len(q.SPIRV), q.SPIRV[:4])
	}
	// Qt's convention: qt_Matrix at 0, qt_Opacity right after it, then the
	// effect's own properties.
	if len(q.Uniforms) < 3 {
		t.Fatalf("uniforms = %+v", q.Uniforms)
	}
	if u := q.Uniforms[0]; u.Name != "qt_Matrix" || u.Offset != 0 || u.Kind != UniformMat4 {
		t.Errorf("first uniform = %+v, want qt_Matrix mat4 at 0", u)
	}
	if u := q.Uniforms[1]; u.Name != "qt_Opacity" || u.Offset != 64 || u.Kind != UniformFloat {
		t.Errorf("second uniform = %+v, want qt_Opacity float at 64", u)
	}
	if q.Size < 80 {
		t.Errorf("block size = %d, want at least 80", q.Size)
	}
	named := 0
	for _, u := range q.Uniforms {
		if u.Name != "" {
			named++
		}
	}
	if named != len(q.Uniforms) {
		t.Errorf("%d of %d uniforms have names", named, len(q.Uniforms))
	}
}

// GLSL's sampler2D compiles to a SPIR-V COMBINED image sampler, which
// WebGPU has no binding for — naga rejects the module outright. The
// rewrite says the same thing the Vulkan way: the variable becomes an
// image, a sampler variable is added beside it, and every load of the old
// variable becomes load+load+OpSampledImage keeping the original result
// id. This checks the rewrite's shape; that a GPU accepts the result is
// what the lens and portal pages show.
func TestSplitCombinedSamplers(t *testing.T) {
	var path string
	for _, c := range []string{
		"../build/qml-compare/examples-ref/lenstabs/shaders/lens_circle.frag.qsb",
		"testdata/lens_circle.frag.qsb",
	} {
		if _, err := os.Stat(c); err == nil {
			path = c
			break
		}
	}
	if path == "" {
		t.Skip("no texture-sampling .qsb available")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseQSB(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Textures) != 1 {
		t.Fatalf("textures = %+v, want one", q.Textures)
	}
	out, splits, err := SplitCombinedSamplers(q.SPIRV)
	if err != nil {
		t.Fatal(err)
	}
	if len(splits) != 1 || splits[0].TextureBinding != q.Textures[0].Binding {
		t.Fatalf("splits = %+v for texture %+v", splits, q.Textures[0])
	}
	if splits[0].SamplerBinding == splits[0].TextureBinding {
		t.Errorf("the sampler landed on the texture's own binding %d", splits[0].SamplerBinding)
	}
	if len(out) <= len(q.SPIRV) {
		t.Errorf("rewrite is %d bytes against the original %d", len(out), len(q.SPIRV))
	}
	// The result must still parse as SPIR-V, and its declared id bound has
	// to cover the ids the rewrite added.
	if len(out)%4 != 0 {
		t.Fatalf("rewrite is %d bytes", len(out))
	}
	words := make([]uint32, len(out)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(out[4*i:])
	}
	if words[0] != 0x07230203 {
		t.Fatalf("bad magic %#x", words[0])
	}
	maxID := uint32(0)
	for i := 5; i < len(words); {
		wc := int(words[i] >> 16)
		if wc == 0 || i+wc > len(words) {
			t.Fatalf("the rewrite does not walk: bad word count at %d", i)
		}
		for _, w := range words[i+1 : i+wc] {
			if w > maxID && w < 1<<20 {
				maxID = w
			}
		}
		i += wc
	}
	if words[3] <= maxID {
		t.Errorf("id bound %d does not cover the ids used (%d)", words[3], maxID)
	}
	// And the padding pass leaves a big enough block.
	padded, err := PadUniformBlock(out, q.Size+32)
	if err != nil {
		t.Fatal(err)
	}
	if len(padded) <= len(out) {
		t.Errorf("padding added nothing (%d bytes)", len(padded))
	}
}
