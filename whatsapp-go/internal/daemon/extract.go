package daemon

import (
	"fmt"
	"strings"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"

	"askmywa/internal/security"
	"askmywa/internal/store"
)

// unwrap removes the envelopes WhatsApp puts around a message (ephemeral,
// view-once, document with caption, edit) and returns the inner message.
func unwrap(msg *waE2E.Message) *waE2E.Message {
	for i := 0; msg != nil && i < 5; i++ {
		var inner *waE2E.Message
		switch {
		case msg.GetEphemeralMessage() != nil:
			inner = msg.GetEphemeralMessage().GetMessage()
		case msg.GetViewOnceMessage() != nil:
			inner = msg.GetViewOnceMessage().GetMessage()
		case msg.GetViewOnceMessageV2() != nil:
			inner = msg.GetViewOnceMessageV2().GetMessage()
		case msg.GetViewOnceMessageV2Extension() != nil:
			inner = msg.GetViewOnceMessageV2Extension().GetMessage()
		case msg.GetDocumentWithCaptionMessage() != nil:
			inner = msg.GetDocumentWithCaptionMessage().GetMessage()
		case msg.GetEditedMessage() != nil:
			inner = msg.GetEditedMessage().GetMessage()
		case msg.GetDeviceSentMessage() != nil:
			inner = msg.GetDeviceSentMessage().GetMessage()
		}
		if inner == nil {
			return msg
		}
		msg = inner
	}
	return msg
}

// extractText returns the text of a message or the caption of its media.
func extractText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	switch {
	case msg.GetConversation() != "":
		return msg.GetConversation()
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetText()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetCaption()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetCaption()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetCaption()
	case msg.GetLocationMessage() != nil:
		loc := msg.GetLocationMessage()
		return strings.TrimSpace(fmt.Sprintf("[location %.5f,%.5f] %s", loc.GetDegreesLatitude(), loc.GetDegreesLongitude(), loc.GetName()))
	case msg.GetContactMessage() != nil:
		return "[contact card] " + msg.GetContactMessage().GetDisplayName()
	case msg.GetPollCreationMessage() != nil:
		return "[poll] " + msg.GetPollCreationMessage().GetName()
	case msg.GetPollCreationMessageV2() != nil:
		return "[poll] " + msg.GetPollCreationMessageV2().GetName()
	case msg.GetPollCreationMessageV3() != nil:
		return "[poll] " + msg.GetPollCreationMessageV3().GetName()
	}
	return ""
}

// extractMedia returns the download parameters of a media message, or nil.
// File names are derived from the message timestamp and id so that a
// download is idempotent.
func extractMedia(msg *waE2E.Message, id string, ts time.Time) *store.MediaInfo {
	if msg == nil {
		return nil
	}
	stamp := ts.UTC().Format("20060102_150405")
	short := id
	if len(short) > 8 {
		short = short[len(short)-8:]
	}
	switch {
	case msg.GetImageMessage() != nil:
		m := msg.GetImageMessage()
		return &store.MediaInfo{MediaType: "image", Filename: fmt.Sprintf("image_%s_%s%s", stamp, short, extFor(m.GetMimetype(), ".jpg")), MimeType: m.GetMimetype(),
			URL: m.GetURL(), DirectPath: m.GetDirectPath(), MediaKey: m.GetMediaKey(), FileSHA256: m.GetFileSHA256(), FileEncSHA256: m.GetFileEncSHA256(), FileLength: m.GetFileLength()}
	case msg.GetVideoMessage() != nil:
		m := msg.GetVideoMessage()
		return &store.MediaInfo{MediaType: "video", Filename: fmt.Sprintf("video_%s_%s%s", stamp, short, extFor(m.GetMimetype(), ".mp4")), MimeType: m.GetMimetype(),
			URL: m.GetURL(), DirectPath: m.GetDirectPath(), MediaKey: m.GetMediaKey(), FileSHA256: m.GetFileSHA256(), FileEncSHA256: m.GetFileEncSHA256(), FileLength: m.GetFileLength()}
	case msg.GetAudioMessage() != nil:
		m := msg.GetAudioMessage()
		return &store.MediaInfo{MediaType: "audio", Filename: fmt.Sprintf("audio_%s_%s%s", stamp, short, extFor(m.GetMimetype(), ".ogg")), MimeType: m.GetMimetype(),
			URL: m.GetURL(), DirectPath: m.GetDirectPath(), MediaKey: m.GetMediaKey(), FileSHA256: m.GetFileSHA256(), FileEncSHA256: m.GetFileEncSHA256(), FileLength: m.GetFileLength()}
	case msg.GetDocumentMessage() != nil:
		m := msg.GetDocumentMessage()
		name := m.GetFileName()
		if name == "" {
			name = m.GetTitle()
		}
		if name == "" {
			name = fmt.Sprintf("document_%s_%s", stamp, short)
		}
		return &store.MediaInfo{MediaType: "document", Filename: security.SafeFilename(name), MimeType: m.GetMimetype(),
			URL: m.GetURL(), DirectPath: m.GetDirectPath(), MediaKey: m.GetMediaKey(), FileSHA256: m.GetFileSHA256(), FileEncSHA256: m.GetFileEncSHA256(), FileLength: m.GetFileLength()}
	case msg.GetStickerMessage() != nil:
		m := msg.GetStickerMessage()
		return &store.MediaInfo{MediaType: "sticker", Filename: fmt.Sprintf("sticker_%s_%s.webp", stamp, short), MimeType: m.GetMimetype(),
			URL: m.GetURL(), DirectPath: m.GetDirectPath(), MediaKey: m.GetMediaKey(), FileSHA256: m.GetFileSHA256(), FileEncSHA256: m.GetFileEncSHA256(), FileLength: m.GetFileLength()}
	}
	return nil
}

func extFor(mime, fallback string) string {
	mime = strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0]))
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4", "audio/aac":
		return ".m4a"
	case "audio/wav":
		return ".wav"
	}
	return fallback
}
