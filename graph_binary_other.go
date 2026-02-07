//go:build !(linux && amd64) && !(linux && arm64) && !(darwin && amd64) && !(darwin && arm64) && !(windows && amd64)

package platform

// graphBinaryData is empty on unsupported platforms.
var graphBinaryData []byte
