//go:build !android

package app

import "fmt"

// Asset is Android-only (an APK's packed assets); elsewhere data lives
// in the repository's build directories.
func Asset(name string) (string, error) {
	return "", fmt.Errorf("app: no packed assets on this platform (%s)", name)
}
