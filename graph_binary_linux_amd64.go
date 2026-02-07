//go:build linux && amd64

package platform

import _ "embed"

//go:embed actions/graph/platform-graph-linux-amd64
var graphBinaryData []byte
