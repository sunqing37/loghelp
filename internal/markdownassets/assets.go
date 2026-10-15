// Package markdownassets contains the pinned JavaScript, CSS, fonts and license
// notices used by the offline Markdown renderer.
package markdownassets

import _ "embed"

//go:embed runtime.zip
var Runtime []byte
