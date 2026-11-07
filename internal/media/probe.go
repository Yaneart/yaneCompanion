package media

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"regexp"
	"strconv"
	"strings"
	"time"

	"yanecompanion/internal/engine"
)

var ErrUnsupported = errors.New("UNSUPPORTED_MEDIA")

type Video struct {
	Index        int     `json:"-"`
	Codec        string  `json:"codec"`
	ContentType  string  `json:"contentType"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	FPS          float64 `json:"fps"`
	Bitrate      float64 `json:"bitrate"`
	BitDepth     int     `json:"bitDepth"`
	DynamicRange string  `json:"dynamicRange"`
}
type Audio struct {
	Index       int    `json:"-"`
	Ref         string `json:"audioRef"`
	Codec       string `json:"codec"`
	ContentType string `json:"contentType,omitempty"`
	Channels    int    `json:"channels"`
	SampleRate  int    `json:"sampleRate"`
	Label       string `json:"label"`
	Language    string `json:"language,omitempty"`
	Default     bool   `json:"default"`
}
type Probe struct {
	Duration float64  `json:"durationSeconds"`
	Video    Video    `json:"video"`
	Audio    []Audio  `json:"audioTracks"`
	Text     []Text   `json:"textTracks"`
	Fonts    []Font   `json:"-"`
	Outputs  []string `json:"outputs"`
}
type Service struct {
	Runner    *Runner
	Backend   RangeBackend
	Directory string
}

func (s *Service) Probe(ctx context.Context, hash string, file engine.File) (*Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	b, err := bridge(ctx, s.Backend, hash, file, 32<<20)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	args := append([]string{"-v", "error"}, inputOptions...)
	args = append(args, "-show_streams", "-show_format", "-of", "json", b.url)
	data, err := s.Runner.Probe(ctx, args)
	if err != nil {
		return nil, err
	}
	// Attachment extradata can be megabytes. Read only the chosen video's small
	// codec header here; fonts have their own bounded, lazy resource lane.
	var raw struct {
		Streams []rawStream `json:"streams"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil, ErrUnsupported
	}
	for i := range raw.Streams {
		st := &raw.Streams[i]
		if st.Type != "video" || st.Disposition.Attached != 0 {
			continue
		}
		headerArgs := append([]string{"-v", "error"}, inputOptions...)
		headerArgs = append(headerArgs, "-select_streams", strconv.Itoa(st.Index), "-show_entries", "stream=extradata", "-show_data", "-of", "json", b.url)
		header, err := s.Runner.Probe(ctx, headerArgs)
		if err != nil {
			return nil, err
		}
		var result struct {
			Streams []rawStream `json:"streams"`
		}
		if json.Unmarshal(header, &result) != nil || len(result.Streams) != 1 {
			return nil, ErrUnsupported
		}
		st.Extra = result.Streams[0].Extra
		break
	}
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(data, &doc)
	doc["streams"], _ = json.Marshal(raw.Streams)
	data, _ = json.Marshal(doc)
	return parseProbe(data)
}

type rawStream struct {
	Index      int    `json:"index"`
	Type       string `json:"codec_type"`
	Codec      string `json:"codec_name"`
	Profile    string `json:"profile"`
	Pixel      string `json:"pix_fmt"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Level      int    `json:"level"`
	Rate       string `json:"avg_frame_rate"`
	Bitrate    string `json:"bit_rate"`
	Extra      string `json:"extradata"`
	Transfer   string `json:"color_transfer"`
	Primaries  string `json:"color_primaries"`
	Channels   int    `json:"channels"`
	SampleRate string `json:"sample_rate"`
	ExtraSize  int    `json:"extradata_size"`
	Tags       struct {
		Title    string `json:"title"`
		Language string `json:"language"`
	} `json:"tags"`
	Disposition struct {
		Attached int `json:"attached_pic"`
		Default  int `json:"default"`
		Forced   int `json:"forced"`
		SDH      int `json:"hearing_impaired"`
	} `json:"disposition"`
}

func number(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
func finite(v float64) bool   { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func parseProbe(data []byte) (*Probe, error) {
	var raw struct {
		Streams []rawStream `json:"streams"`
		Format  struct {
			Duration string `json:"duration"`
			Bitrate  string `json:"bit_rate"`
		} `json:"format"`
	}
	if json.Unmarshal(data, &raw) != nil || len(raw.Streams) == 0 || len(raw.Streams) > 64 {
		return nil, ErrUnsupported
	}
	p := &Probe{Duration: number(raw.Format.Duration), Audio: []Audio{}, Text: []Text{}, Outputs: []string{"original"}}
	if !finite(p.Duration) || p.Duration <= 0 || p.Duration > 7*24*3600 {
		return nil, ErrUnsupported
	}
	found := false
	for _, st := range raw.Streams {
		if st.Index < 0 || st.Index > 1024 {
			return nil, ErrUnsupported
		}
		p.addResource(st)
		if st.Type == "audio" {
			if st.Channels < 1 || st.Channels > 32 {
				return nil, ErrUnsupported
			}
			a := Audio{Index: st.Index, Ref: randomRef(), Codec: st.Codec, Channels: st.Channels, SampleRate: int(number(st.SampleRate))}
			a.Label, a.Language, a.Default = label(st.Tags.Title, fmt.Sprintf("Audio %d", len(p.Audio)+1)), language(st.Tags.Language), st.Disposition.Default == 1
			if a.SampleRate > 0 && a.SampleRate <= 48000 && a.Channels <= 8 {
				switch {
				case st.Codec == "aac" && (st.Profile == "LC" || st.Profile == "1"):
					a.ContentType = `audio/mp4; codecs="mp4a.40.2"`
				case st.Codec == "ac3":
					a.ContentType = `audio/mp4; codecs="ac-3"`
				case st.Codec == "eac3":
					a.ContentType = `audio/mp4; codecs="ec-3"`
				}
			}
			p.Audio = append(p.Audio, a)
		}
		if st.Type != "video" || st.Disposition.Attached != 0 || found {
			continue
		}
		found = true
		fpsParts := strings.Split(st.Rate, "/")
		fps := 0.0
		if len(fpsParts) == 2 && number(fpsParts[1]) > 0 {
			fps = number(fpsParts[0]) / number(fpsParts[1])
		}
		if st.Width < 16 || st.Height < 16 || st.Width > 4096 || st.Height > 2160 || !finite(fps) || fps <= 0 || fps > 60 {
			return nil, ErrUnsupported
		}
		depth := 8
		if st.Pixel == "yuv420p10le" {
			depth = 10
		}
		mime := videoMIME(st.Codec, st.Extra)
		if !compatibleVideo(st) || mime == "" {
			return nil, ErrUnsupported
		}
		hdr := "sdr"
		if st.Primaries == "bt2020" || st.Transfer == "smpte2084" || st.Transfer == "arib-std-b67" {
			hdr = "hdr"
		}
		bitrate := number(st.Bitrate)
		if bitrate <= 0 || !finite(bitrate) {
			bitrate = number(raw.Format.Bitrate)
		}
		if !finite(bitrate) || bitrate < 0 {
			bitrate = 0
		}
		p.Video = Video{st.Index, st.Codec, mime, st.Width, st.Height, fps, bitrate, depth, hdr}
	}
	if !found {
		return nil, ErrUnsupported
	}
	return p, nil
}

func compatibleVideo(st rawStream) bool {
	// The pinned --enable-small FFmpeg reports numeric AVProfile values.
	if st.Level <= 0 {
		return false
	}
	switch st.Codec {
	case "h264":
		if st.Pixel != "yuv420p" || st.Level > 52 {
			return false
		}
		switch st.Profile {
		case "High", "100", "Main", "77", "Baseline", "66", "Constrained Baseline", "578":
			return true
		}
	case "hevc":
		if st.Level > 153 {
			return false
		}
		return (st.Profile == "Main" || st.Profile == "1") && st.Pixel == "yuv420p" || (st.Profile == "Main 10" || st.Profile == "2") && st.Pixel == "yuv420p10le"
	}
	return false
}

var dumpLine = regexp.MustCompile(`(?m)^\s*[0-9a-fA-F]{8}: ([0-9a-fA-F ]+)  `)

func videoMIME(codec, dump string) string {
	var encoded strings.Builder
	for _, line := range dumpLine.FindAllStringSubmatch(dump, -1) {
		encoded.WriteString(strings.ReplaceAll(line[1], " ", ""))
	}
	data, err := hex.DecodeString(encoded.String())
	if err != nil || len(data) < 4 || data[0] != 1 {
		return ""
	}
	if codec == "h264" {
		return fmt.Sprintf(`video/mp4; codecs="avc1.%02x%02x%02x"`, data[1], data[2], data[3])
	}
	if codec != "hevc" || len(data) < 13 || data[1]>>6 != 0 {
		return ""
	}
	flags := uint32(data[2])<<24 | uint32(data[3])<<16 | uint32(data[4])<<8 | uint32(data[5])
	constraints := data[6:12]
	for len(constraints) > 0 && constraints[len(constraints)-1] == 0 {
		constraints = constraints[:len(constraints)-1]
	}
	suffix := ""
	for _, b := range constraints {
		suffix += fmt.Sprintf(".%X", b)
	}
	tier := "L"
	if data[1]&32 != 0 {
		tier = "H"
	}
	return fmt.Sprintf(`video/mp4; codecs="hvc1.%d.%X.%s%d%s"`, data[1]&31, bits.Reverse32(flags), tier, data[12], suffix)
}

type ClientMedia struct {
	ContentType string `json:"contentType"`
	Supported   bool   `json:"supported"`
}
type Target struct {
	FileRef  string  `json:"fileRef"`
	AudioRef string  `json:"audioRef"`
	TextRef  *string `json:"textRef"`
	Position float64 `json:"positionSeconds"`
	Output   string  `json:"output"`
}

func (p *Probe) Validate(target Target, client []ClientMedia) (*Audio, bool, error) {
	if target.Output != "original" || !finite(target.Position) || target.Position < 0 || target.Position >= p.Duration || len(client) > 16 {
		return nil, false, ErrUnsupported
	}
	if target.TextRef != nil {
		valid := false
		for _, text := range p.Text {
			valid = valid || text.Ref == *target.TextRef && text.Available
		}
		if !valid {
			return nil, false, ErrUnsupported
		}
	}
	supported := func(mime string) bool {
		for _, c := range client {
			if c.ContentType == mime && c.Supported {
				return true
			}
		}
		return false
	}
	if !supported(p.Video.ContentType) {
		return nil, false, ErrUnsupported
	}
	if target.AudioRef == "" && len(p.Audio) == 0 {
		return nil, false, nil
	}
	for i := range p.Audio {
		a := &p.Audio[i]
		if a.Ref != target.AudioRef {
			continue
		}
		if a.ContentType != "" && supported(a.ContentType) {
			return a, true, nil
		}
		if supported(`audio/mp4; codecs="mp4a.40.2"`) {
			return a, false, nil
		}
		return nil, false, ErrUnsupported
	}
	return nil, false, ErrUnsupported
}
