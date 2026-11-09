package app

import "errors"

// ErrNoBrowser is returned where the platform cannot hand a URL to a
// browser (headless, Android).
var ErrNoBrowser = errors.New("app: no browser to open the URL in")
