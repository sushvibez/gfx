//go:build js

package render

// The web has no SPIR-V: WebGPU takes WGSL only, so a compiled shader from
// a Qt .qsb cannot be built here. The type still exists (the batch list
// names it) and every entry point says no, which is what makes a caller
// fall back to drawing its source.

import (
	"fmt"

	"github.com/sushvibez/gfx/gpu"
)

// ShaderFX is the native custom-shader quad; on the web it never builds.
type ShaderFX struct{}

// NewShaderFX always fails on the web.
func (r *Renderer2D) NewShaderFX(label string, vert, frag *QSB, tex []*Image) (*ShaderFX, error) {
	return nil, fmt.Errorf("render: %s: a compiled (SPIR-V) shader needs the native backend", label)
}

func (fx *ShaderFX) SetUniforms(*Renderer2D, []byte)                {}
func (fx *ShaderFX) SetTextures(*Renderer2D, []*Image)              {}
func (fx *ShaderFX) Release()                                       {}
func (fx *ShaderFX) Valid(*Renderer2D) bool                         { return false }
func (r *Renderer2D) DrawShaderFX(fx *ShaderFX, x, y, w, h float32) {}
func (fx *ShaderFX) draw(*gpu.RenderPass, int)                      {}
