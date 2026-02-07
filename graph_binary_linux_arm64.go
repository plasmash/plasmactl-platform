//go:build linux && arm64

package platform

import _ "embed"

//go:embed actions/graph/platform-graph-linux-arm64
var graphBinaryData []byte
