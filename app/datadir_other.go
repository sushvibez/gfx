//go:build !android

package app

// DataDir is the app's private, writable directory; only Android names
// one (the activity's internalDataPath). Desktop builds use the
// repository's build directories.
func DataDir() string { return "" }
