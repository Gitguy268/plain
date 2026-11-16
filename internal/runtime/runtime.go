// Package runtime embeds the JavaScript every compiled Plain program starts with.
package runtime

import _ "embed"

// Prelude is the runtime: Option/Result constructors, equality, printing, panics and the standard library.
//
//go:embed prelude.js
var Prelude string
