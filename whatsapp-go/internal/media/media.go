// Package media classifies files for sending, analyses Ogg Opus voice notes
// (duration and waveform, ported from the previous bridge) and converts
// audio with ffmpeg.
package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Kind is the WhatsApp message kind a file is sent as.
type Kind string

// Kinds.
const (
	KindImage    Kind = "image"
	KindVideo    Kind = "video"
	KindAudio    Kind = "audio"
	KindDocument Kind = "document"
)

// Classify picks the kind and MIME type from the file extension.
func Classify(path string) (Kind, string) {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")) {
	case "jpg", "jpeg":
		return KindImage, "image/jpeg"
	case "png":
		return KindImage, "image/png"
	case "gif":
		return KindImage, "image/gif"
	case "webp":
		return KindImage, "image/webp"
	case "ogg", "opus":
		return KindAudio, "audio/ogg; codecs=opus"
	case "mp3":
		return KindAudio, "audio/mpeg"
	case "m4a", "aac":
		return KindAudio, "audio/mp4"
	case "wav":
		return KindAudio, "audio/wav"
	case "mp4":
		return KindVideo, "video/mp4"
	case "mov":
		return KindVideo, "video/quicktime"
	case "avi":
		return KindVideo, "video/avi"
	case "pdf":
		return KindDocument, "application/pdf"
	case "txt":
		return KindDocument, "text/plain"
	case "csv":
		return KindDocument, "text/csv"
	case "zip":
		return KindDocument, "application/zip"
	case "doc":
		return KindDocument, "application/msword"
	case "docx":
		return KindDocument, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "xlsx":
		return KindDocument, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	return KindDocument, "application/octet-stream"
}

// ErrNotOgg is returned when the data does not start with an Ogg page.
var ErrNotOgg = errors.New("not an Ogg file (missing OggS signature)")

// AnalyzeOggOpus reads the Ogg pages of an Opus file to compute its
// duration (from the last granule position) and returns a synthetic
// 64-byte waveform WhatsApp displays for voice notes.
func AnalyzeOggOpus(data []byte) (seconds uint32, waveform []byte, err error) {
	if len(data) < 4 || string(data[0:4]) != "OggS" {
		return 0, nil, ErrNotOgg
	}
	var lastGranule uint64
	var sampleRate uint32 = 48000
	var preSkip uint16
	foundHead := false
	for i := 0; i+27 < len(data); {
		if string(data[i:i+4]) != "OggS" {
			i++
			continue
		}
		granule := binary.LittleEndian.Uint64(data[i+6 : i+14])
		seq := binary.LittleEndian.Uint32(data[i+18 : i+22])
		nSeg := int(data[i+26])
		if i+27+nSeg >= len(data) {
			break
		}
		pageSize := 27 + nSeg
		for _, l := range data[i+27 : i+27+nSeg] {
			pageSize += int(l)
		}
		if pageSize > len(data)-i {
			pageSize = len(data) - i
		}
		if !foundHead && seq <= 1 {
			page := data[i : i+pageSize]
			if pos := bytes.Index(page, []byte("OpusHead")); pos >= 0 && pos+8+12 <= len(page) {
				// OpusHead: magic(8) version(1) channels(1) preSkip(2) sampleRate(4) ...
				preSkip = binary.LittleEndian.Uint16(page[pos+10 : pos+12])
				sampleRate = binary.LittleEndian.Uint32(page[pos+12 : pos+16])
				foundHead = true
			}
		}
		if granule != 0 && granule != math.MaxUint64 {
			lastGranule = granule
		}
		i += pageSize
	}
	if sampleRate == 0 {
		sampleRate = 48000
	}
	if lastGranule > uint64(preSkip) {
		// Opus granule positions are always at 48 kHz regardless of the input rate.
		seconds = uint32(math.Ceil(float64(lastGranule-uint64(preSkip)) / 48000))
	} else {
		seconds = uint32(len(data) / 2000) // rough fallback
	}
	if seconds < 1 {
		seconds = 1
	} else if seconds > 300 {
		seconds = 300
	}
	return seconds, PlaceholderWaveform(seconds), nil
}

// PlaceholderWaveform generates a plausible 64-value waveform, deterministic
// for a given duration.
func PlaceholderWaveform(seconds uint32) []byte {
	const n = 64
	wave := make([]byte, n)
	rng := rand.New(rand.NewSource(int64(seconds)))
	freq := float64(min(int(seconds), 120)) / 30.0
	for i := range wave {
		pos := float64(i) / n
		v := 35*math.Sin(pos*math.Pi*freq*8) + 17.5*math.Sin(pos*math.Pi*freq*16)
		v += (rng.Float64() - 0.5) * 15
		v = v*(0.7+0.3*math.Sin(pos*math.Pi)) + 50
		wave[i] = byte(math.Max(0, math.Min(100, v)))
	}
	return wave
}

// IsOggOpus reports whether the data looks like an Ogg Opus stream.
func IsOggOpus(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == "OggS" && bytes.Contains(data[:min(len(data), 4096)], []byte("OpusHead"))
}

// ConvertToOpusOgg transcodes input to a 24 kHz Opus voice note in outDir
// with ffmpeg. The returned path is a new file; callers remove it when done.
func ConvertToOpusOgg(ctx context.Context, input, outDir string) (string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg is not installed (brew install ffmpeg), so %s cannot be converted to a voice note; use send_file to send it as a plain audio file", filepath.Base(input))
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", err
	}
	out, err := os.CreateTemp(outDir, "voice-*.ogg")
	if err != nil {
		return "", err
	}
	outPath := out.Name()
	out.Close()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-i", input,
		"-vn", "-c:a", "libopus", "-b:a", "32k", "-ar", "24000", "-ac", "1", "-application", "voip", "-vbr", "on",
		"-compression_level", "10", "-frame_duration", "60", "-y", outPath)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.Remove(outPath)
		return "", fmt.Errorf("ffmpeg failed to convert %s: %v: %s", filepath.Base(input), err, strings.TrimSpace(stderr.String()))
	}
	return outPath, nil
}
