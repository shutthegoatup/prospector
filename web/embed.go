// Package web hands the Go side what the web build and the server share.
package web

import _ "embed"

// Locales is locales.json: the languages Prospector speaks, which the web build
// compiles catalogues for and the server chooses among.
//
//go:embed locales.json
var Locales []byte
