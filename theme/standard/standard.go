// Package standard holds the manifest of the standard modules and the default
// theme built on it. The screen editor opens with them.
package standard

import _ "embed"

//go:embed manifest.json
var Manifest []byte

//go:embed theme.json
var Theme []byte
