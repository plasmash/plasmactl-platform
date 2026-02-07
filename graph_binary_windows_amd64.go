//go:build windows && amd64

package platform

import _ "embed"

//go:embed actions/graph/platform-graph-windows-amd64.exe
var graphBinaryData []byte
