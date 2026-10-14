//go:build windows && amd64

package mirrorassets

import _ "embed"

//go:embed runtime_windows_amd64.zip
var Archive []byte
