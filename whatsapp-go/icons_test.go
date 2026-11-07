package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestServerIconsEmbedLogo(t *testing.T) {
	icons := serverIcons()
	if len(icons) < 2 {
		t.Fatalf("icons = %d", len(icons))
	}
	data, ok := strings.CutPrefix(icons[0].Source, "data:image/svg+xml;base64,")
	if !ok {
		t.Fatalf("first icon is not an inline SVG: %.40s", icons[0].Source)
	}
	svg, err := base64.StdEncoding.DecodeString(data)
	if err != nil || !strings.Contains(string(svg), "<svg") {
		t.Fatalf("inline icon does not decode to an SVG: %v", err)
	}
	if !strings.HasPrefix(icons[1].Source, "https://askmywa.com/") {
		t.Errorf("second icon: %s", icons[1].Source)
	}
}
