package daemon

import (
	"testing"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestUnwrapAndExtract(t *testing.T) {
	inner := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("une photo"), Mimetype: proto.String("image/png"), DirectPath: proto.String("/v/x")}}
	wrapped := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: inner}}}}
	got := unwrap(wrapped)
	if got != inner {
		t.Fatal("unwrap did not reach the inner message")
	}
	if extractText(got) != "une photo" {
		t.Errorf("caption: %q", extractText(got))
	}
	mi := extractMedia(got, "3EB0ABCDEF12", time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC))
	if mi == nil || mi.MediaType != "image" || mi.Filename != "image_20261105_100000_ABCDEF12.png" || mi.DirectPath != "/v/x" {
		t.Errorf("media: %+v", mi)
	}
	doc := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("../../facture.pdf")}}
	if mi := extractMedia(doc, "id", time.Now()); mi.Filename != "facture.pdf" {
		t.Errorf("document name not sanitised: %q", mi.Filename)
	}
	if extractText(&waE2E.Message{Conversation: proto.String("salut")}) != "salut" || extractMedia(&waE2E.Message{}, "x", time.Now()) != nil {
		t.Error("plain text")
	}
}
