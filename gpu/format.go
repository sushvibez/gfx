package gpu

// BytesPerTexel is a format's size in bytes, for an upload's row stride.
// Only the formats that get written from the CPU need an entry; anything
// else is a render target and never lands here.
func BytesPerTexel(f TextureFormat) int {
	switch f {
	case FormatR8Unorm:
		return 1
	}
	return 4
}

// VertexStepMode says what a vertex buffer's records are indexed by.
type VertexStepMode uint32

// WGPUVertexStepMode (webgpu.h): Undefined is 0, and a layout that
// leaves it there with attributes set defaults to Vertex — which is why
// every layout written before this existed still steps per vertex.
const (
	StepUndefined VertexStepMode = 0
	StepVertex    VertexStepMode = 1
	StepInstance  VertexStepMode = 2
)
