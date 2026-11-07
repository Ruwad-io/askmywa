package main

import (
	_ "embed"
	"encoding/base64"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed assets/logo.svg
var logoSVG []byte

// serverIcons returns the logo for MCP clients: the SVG inline, so it shows
// without a network request, and a PNG on the website for clients that only
// draw raster images.
func serverIcons() []mcp.Icon {
	return []mcp.Icon{
		{Source: "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(logoSVG), MIMEType: "image/svg+xml", Sizes: []string{"any"}},
		{Source: "https://askmywa.com/assets/icon-192.png", MIMEType: "image/png", Sizes: []string{"192x192"}},
	}
}
