//go:build !linux && !windows && !js && !android

package audio

// No native backend here: the null device keeps the API working.
func openBackend() (backend, error) { return nil, errUnsupported }
