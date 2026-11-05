package media

import (
	"encoding/binary"
	"testing"
)

// oggPage builds one Ogg page with a single segment.
func oggPage(seq uint32, granule uint64, payload []byte) []byte {
	page := make([]byte, 27)
	copy(page, "OggS")
	binary.LittleEndian.PutUint64(page[6:14], granule)
	binary.LittleEndian.PutUint32(page[18:22], seq)
	page[26] = 1
	page = append(page, byte(len(payload)))
	return append(page, payload...)
}

func TestAnalyzeOggOpus(t *testing.T) {
	head := []byte("OpusHead")
	head = append(head, 1, 1) // version, channels
	head = binary.LittleEndian.AppendUint16(head, 312)
	head = binary.LittleEndian.AppendUint32(head, 48000)
	head = append(head, 0, 0, 0, 0, 0)
	var data []byte
	data = append(data, oggPage(0, 0, head)...)
	data = append(data, oggPage(1, 0, []byte("OpusTags"))...)
	data = append(data, oggPage(2, 48000*7+312, make([]byte, 10))...) // 7 s of audio
	secs, wave, err := AnalyzeOggOpus(data)
	if err != nil || secs != 7 || len(wave) != 64 {
		t.Errorf("secs=%d wave=%d err=%v", secs, len(wave), err)
	}
	if !IsOggOpus(data) || IsOggOpus([]byte("ID3")) {
		t.Error("IsOggOpus")
	}
	if _, _, err := AnalyzeOggOpus([]byte("RIFF")); err == nil {
		t.Error("expected ErrNotOgg")
	}
}

func TestClassify(t *testing.T) {
	for path, want := range map[string]Kind{"a.JPG": KindImage, "b.mp4": KindVideo, "c.ogg": KindAudio, "d.pdf": KindDocument, "e": KindDocument} {
		if k, mime := Classify(path); k != want || mime == "" {
			t.Errorf("%s: %s %s", path, k, mime)
		}
	}
}
