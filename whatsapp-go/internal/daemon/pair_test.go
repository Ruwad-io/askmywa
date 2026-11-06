package daemon

import (
	"strings"
	"testing"

	"whatsapp-mcp/internal/ipc"
)

func TestPairPhoneValidatesNumbers(t *testing.T) {
	d := &Daemon{}
	for in, want := range map[string]string{
		"0612345678": "country code",
		"12345":      "too short",
		"":           "too short",
	} {
		if _, err := d.pairPhone(ipc.PairParams{Phone: in}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestFormatCode(t *testing.T) {
	if formatCode("ABCD1234") != "ABCD-1234" || formatCode("ABCD-1234") != "ABCD-1234" {
		t.Error("formatCode")
	}
}
