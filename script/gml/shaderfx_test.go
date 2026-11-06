package gml

import "testing"

// An IMAGE source hands a shader its OWN texture, not a snapshot of the
// item: in Qt an Image is a texture provider, so `ShaderEffect { source:
// someImage }` samples the decoded image whole and the item's fillMode
// never enters into it. Measured on the corpus' lens page — Qt's shader
// sees the entire photo stretched across the quad where a rendered
// snapshot would show it cover-cropped, a tenth of the picture's scale.
// The value may also arrive through a wrapping component's declared
// property, which is how that page passes it.
func TestShaderSourceImageUsesTheImagesOwnTexture(t *testing.T) {
	env := NewEnv()
	d, err := LoadString(`
import QtQuick
Item {
    width: 400; height: 300
    Image {
        id: photo
        anchors.fill: parent
        source: "photo.png"
        fillMode: Image.PreserveAspectCrop
        visible: false
    }
    ShaderEffect {
        id: fx
        anchors.fill: parent
        source: photo
        fragmentShader: "shaders/none.frag.qsb"
    }
}`, env)
	if err != nil {
		t.Fatalf("LoadString: %v", err)
	}
	// The build degrades (there is no compiled shader here), but the source
	// resolution is what this test is about.
	d.View()()
	fxNode := d.doc.nodesByID["fx"]
	if fxNode == nil {
		t.Fatal("the ShaderEffect is not in the id registry")
	}
	cell := d.doc.shaderSourceImage(fxNode, env, "source")
	if cell == nil {
		t.Fatal("the shader's Image source did not resolve to an image cell")
	}
	if cell != d.doc.imageCell("photo.png") {
		t.Errorf("resolved a different cell than the image's own source")
	}
}
