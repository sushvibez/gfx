//go:build android

package app

// OpenURL is not wired on Android yet.
func OpenURL(string) error { return ErrNoBrowser }
