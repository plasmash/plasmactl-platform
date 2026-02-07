//go:build darwin && amd64

package platform

import _ "embed"

//go:embed actions/graph/platform-graph-darwin-amd64
var graphBinaryData []byte
