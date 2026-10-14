package mirrorassets

import _ "embed"

// Android's Java server and notices are identical on all desktop platforms.
//
//go:embed runtime_common.zip
var CommonArchive []byte
