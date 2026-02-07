//go:build darwin && arm64

package platform

import _ "embed"

//go:embed actions/graph/platform-graph-darwin-arm64
var graphBinaryData []byte
