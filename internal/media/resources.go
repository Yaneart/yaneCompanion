package media

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"

	"yanecompanion/internal/engine"
)

const textLimit = 2 << 20
const fontLimit = 1 << 20
const resourceLimit = 8 << 20

type Text struct {
	Ref         string       `json:"textRef"`
	Label       string       `json:"label"`
	Language    string       `json:"language,omitempty"`
	Format      string       `json:"format"`
	Default     bool         `json:"default"`
	Forced      bool         `json:"forced"`
	SDH         bool         `json:"sdh"`
	Available   bool         `json:"available"`
	Reason      string       `json:"unavailableReason,omitempty"`
	Index       int          `json:"-"`
	Sidecar     *engine.File `json:"-"`
	InputFormat string       `json:"-"`
}
type Font struct {
	Ref   string
	Index int
	Size  int
}

func label(value, fallback string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '<' || r == '>' {
			return -1
		}
		return r
	}, value))
	runes := []rune(value)
	if len(runes) > 80 {
		value = string(runes[:80])
	}
	if value == "" {
		return fallback
	}
	return value
}

var languagePattern = regexp.MustCompile(`^[a-zA-Z]{2,3}$`)

func language(value string) string {
	if languagePattern.MatchString(value) {
		return strings.ToLower(value)
	}
	return ""
}

func (p *Probe) addResource(st rawStream) {
	if st.Type == "attachment" && (st.Codec == "ttf" || st.Codec == "otf") && st.ExtraSize > 0 && st.ExtraSize <= fontLimit && len(p.Fonts) < 4 {
		p.Fonts = append(p.Fonts, Font{randomRef(), st.Index, st.ExtraSize})
	}
	if st.Type != "subtitle" || len(p.Text) >= 16 {
		return
	}
	format, reason := "", ""
	switch st.Codec {
	case "subrip", "webvtt", "mov_text", "text":
		format = "vtt"
	case "ass", "ssa":
		format = "ass"
	case "hdmv_pgs_subtitle":
		format, reason = "pgs", "BITMAP_SUBTITLES_UNSUPPORTED"
	case "dvd_subtitle":
		format, reason = "vobsub", "BITMAP_SUBTITLES_UNSUPPORTED"
	default:
		format, reason = "vtt", "SUBTITLE_CODEC_UNSUPPORTED"
	}
	p.Text = append(p.Text, Text{Ref: randomRef(), Label: label(st.Tags.Title, fmt.Sprintf("Subtitles %d", len(p.Text)+1)), Language: language(st.Tags.Language), Format: format, Default: st.Disposition.Default == 1, Forced: st.Disposition.Forced == 1, SDH: st.Disposition.SDH == 1, Available: reason == "", Reason: reason, Index: st.Index})
}

// Match an exact directory and stem, followed only by language/flag suffixes.
// File indexes and paths remain private and never become worker arguments.
func (p *Probe) AddSidecars(video engine.File, files []engine.File) {
	videoPath := strings.ReplaceAll(video.Path, "\\", "/")
	stem := strings.TrimSuffix(videoPath, path.Ext(videoPath))
	for _, f := range files {
		if len(p.Text) >= 16 {
			break
		}
		name := strings.ReplaceAll(f.Path, "\\", "/")
		ext := strings.ToLower(path.Ext(name))
		formats := map[string]string{".srt": "srt", ".vtt": "webvtt", ".ass": "ass", ".ssa": "ass"}
		input := formats[ext]
		base := strings.TrimSuffix(name, path.Ext(name))
		if input == "" || f.Length <= 0 || f.Length > textLimit || base != stem && !strings.HasPrefix(base, stem+".") {
			continue
		}
		suffix := strings.TrimPrefix(strings.TrimPrefix(base, stem), ".")
		t := Text{Ref: randomRef(), Label: label(path.Base(name), "Subtitles"), Format: "vtt", Available: true, Sidecar: &f, InputFormat: input}
		if input == "ass" {
			t.Format = "ass"
		}
		valid := true
		if suffix != "" {
			for _, part := range strings.Split(suffix, ".") {
				switch strings.ToLower(part) {
				case "forced":
					t.Forced = true
				case "sdh":
					t.SDH = true
				case "default":
					t.Default = true
				default:
					if lang := language(part); lang != "" && t.Language == "" {
						t.Language = lang
					} else {
						valid = false
					}
				}
			}
		}
		if valid {
			p.Text = append(p.Text, t)
		}
	}
}

func decodeDump(dump string) ([]byte, error) {
	var encoded strings.Builder
	for _, line := range dumpLine.FindAllStringSubmatch(dump, -1) {
		encoded.WriteString(strings.ReplaceAll(line[1], " ", ""))
	}
	return hex.DecodeString(encoded.String())
}
func validateFont(data []byte) bool {
	if len(data) < 12 || len(data) > fontLimit {
		return false
	}
	magic := binary.BigEndian.Uint32(data)
	if magic != 0x00010000 && magic != 0x4f54544f {
		return false
	}
	n := int(binary.BigEndian.Uint16(data[4:]))
	if n == 0 || n > 128 || 12+n*16 > len(data) {
		return false
	}
	for i := 0; i < n; i++ {
		entry := data[12+i*16:]
		offset, size := uint64(binary.BigEndian.Uint32(entry[8:])), uint64(binary.BigEndian.Uint32(entry[12:]))
		if offset < uint64(12+n*16) || offset+size > uint64(len(data)) {
			return false
		}
	}
	return true
}
