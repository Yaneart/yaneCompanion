package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"yanecompanion/internal/engine"
)

type resource struct {
	data       []byte
	start, end float64
	delay      *float64
}
type captions struct {
	service      *Service
	hash         string
	file         engine.File
	probe        *Probe
	busy         chan struct{}
	cache        map[string]resource // guarded by Playback.mu, also counted in status.Bytes
	bytes        int
	index        *matroskaIndex
	indexChecked bool
	delay        *float64 // immutable value, guarded by Playback.mu; nil keeps legacy coverage
}

// Session/file resources survive a new AV producer, but retain their original
// window checks and the new producer's byte accounting and read grant.
func (p *Playback) restoreCaptionCache() {
	c := p.captions
	shared := &c.probe.captionCache
	p.mu.Lock()
	defer p.mu.Unlock()
	shared.Lock()
	defer shared.Unlock()
	if shared.hash != c.hash || shared.file != c.file || p.ctx.Err() != nil {
		return
	}
	size := 0
	if shared.value != nil {
		size = shared.value.memory()
	}
	for _, r := range shared.resources {
		size += len(r.data)
	}
	if size > resourceLimit || p.status.Bytes+int64(size) > cacheLimit {
		return
	}
	c.cache = maps.Clone(shared.resources)
	if c.cache == nil {
		c.cache = map[string]resource{}
	}
	c.index, c.indexChecked = shared.value, shared.checked
	c.delay = shared.delay
	c.bytes = size
	p.status.Bytes += int64(size)
}

func (p *Playback) resourceInfo(name string) (Text, *Font, string, bool) {
	for _, t := range p.captions.probe.Text {
		if t.Available && name == "text-"+t.Ref+"."+t.Format {
			mime := "text/vtt; charset=utf-8"
			if t.Format == "ass" {
				mime = "text/x-ssa; charset=utf-8"
			}
			return t, nil, mime, true
		}
	}
	for _, f := range p.captions.probe.Fonts {
		if name == "font-"+f.Ref+".bin" {
			return Text{}, &f, "font/ttf", true
		}
	}
	return Text{}, nil, "", false
}

func (p *Playback) serveResource(w http.ResponseWriter, r *http.Request, name string) {
	t, font, mime, ok := p.resourceInfo(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	// HEAD describes an approved route without downloading/extracting anything.
	if r.Method == "HEAD" {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	// A rapid track change can arrive while the cancelled extractor is exiting.
	// Give it Home's bounded 500ms grace period without overlapping workers.
	wait := time.NewTimer(500 * time.Millisecond)
	defer wait.Stop()
	select {
	case p.captions.busy <- struct{}{}:
		defer func() { <-p.captions.busy }()
	case <-ctx.Done():
		return
	case <-wait.C:
		http.Error(w, "RESOURCE_LIMIT", 429)
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(35 * time.Second))
	// A processing timeout must still have time to send its bounded HTTP error.
	// Only source replacement needs to interrupt an already blocked response.
	stopWrite := context.AfterFunc(p.ctx, func() { _ = controller.SetWriteDeadline(time.Now()) })
	defer stopWrite()
	p.mu.Lock()
	position := p.position
	cached, exists := p.captions.cache[name]
	delay := p.captions.delay
	p.mu.Unlock()
	lead := 90.0
	if delay != nil {
		lead = 5
	}
	sameDelay := cached.delay == nil && delay == nil || cached.delay != nil && delay != nil && *cached.delay == *delay
	if !exists || font == nil && (!sameDelay || position < cached.start || position+lead > cached.end && cached.end < p.captions.probe.Duration) {
		data, start, end, err := p.extractResource(ctx, t, font, position, delay)
		if err != nil {
			code, status := "SUBTITLE_UNAVAILABLE", 422
			if font != nil {
				code = "FONT_UNAVAILABLE"
			}
			if err.Error() == "OUTPUT_LIMIT" || err.Error() == "RESOURCE_LIMIT" {
				code, status = "RESOURCE_LIMIT", 429
			}
			http.Error(w, code, status)
			return
		}
		if ctx.Err() != nil {
			return
		}
		p.mu.Lock()
		// No late publication after cancellation/replacement. Replacement has an
		// independent cache and grant, and Close waits for these readers.
		if p.ctx.Err() != nil || ctx.Err() != nil {
			p.mu.Unlock()
			return
		}
		old := len(cached.data)
		if p.captions.bytes-old+len(data) > resourceLimit || p.status.Bytes-int64(old)+int64(len(data)) > cacheLimit {
			p.mu.Unlock()
			http.Error(w, "RESOURCE_LIMIT", 429)
			return
		}
		cached = resource{data: data, start: start, end: end, delay: delay}
		p.captions.cache[name] = cached
		p.captions.bytes += len(data) - old
		p.status.Bytes += int64(len(data) - old)
		shared := &p.captions.probe.captionCache
		shared.Lock()
		if shared.hash != p.captions.hash || shared.file != p.captions.file {
			shared.checked, shared.value = false, nil
		}
		shared.hash, shared.file = p.captions.hash, p.captions.file
		shared.resources = maps.Clone(p.captions.cache)
		shared.delay = p.captions.delay
		shared.Unlock()
		p.mu.Unlock()
	}
	if ctx.Err() != nil {
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(cached.data)))
	if font == nil {
		w.Header().Set("X-Yane-Text-End", strconv.FormatFloat(cached.end, 'f', 3, 64))
		w.Header().Set("Access-Control-Expose-Headers", "X-Yane-Text-End")
	}
	_, _ = w.Write(cached.data)
}

func (p *Playback) extractResource(ctx context.Context, t Text, font *Font, position float64, delays ...*float64) ([]byte, float64, float64, error) {
	c := p.captions
	// Same Home fast path: resolve only the selected indexed subtitle blocks.
	// Keep FFmpeg as the codec decoder and preserve the existing bounded fallback.
	// Visible cue times span +/-60s for the player's supported delay. The index
	// already includes each cue's duration (up to 120s) when finding overlaps;
	// subtracting that duration here too makes a seek fetch an extra two minutes.
	start, end := math.Max(0, position-60), math.Min(c.probe.Duration, position+120)
	cacheStart := position
	if start == 0 {
		cacheStart = 0
	}
	cacheEnd := end
	p.mu.Lock()
	delay := c.delay
	p.mu.Unlock()
	if len(delays) > 0 {
		delay = delays[0]
	}
	if delay != nil {
		// Refill a short interval for the applied delay, rather than fetching
		// every possible delay in advance. Index overlap still includes long cues.
		start = math.Min(math.Max(0, c.probe.Duration-.001), math.Max(0, position-*delay))
		end = math.Min(c.probe.Duration, math.Max(start+.001, position-*delay+30))
		cacheStart, cacheEnd = position, math.Min(c.probe.Duration, position+30)
	}
	reader := &matroskaReader{ctx: ctx, backend: c.service.Backend, hash: c.hash, file: c.file}
	if font == nil && t.Sidecar == nil {
		data, ok, err := p.indexedText(ctx, t, start, end, reader)
		if err != nil {
			return nil, 0, 0, err
		}
		if ok {
			return data, cacheStart, cacheEnd, nil
		}
	}
	file, budget := c.file, int64(128<<20)-reader.bytes
	if t.Sidecar != nil {
		file, budget = *t.Sidecar, textLimit+(256<<10)
	}
	if budget <= 0 {
		return nil, 0, 0, errors.New("RESOURCE_LIMIT")
	}
	b, err := bridge(ctx, c.service.Backend, c.hash, file, budget)
	if err != nil {
		return nil, 0, 0, err
	}
	defer b.Close()
	args := []string{"-v", "error"}
	if font != nil {
		args = append(args, inputOptions...)
		args = append(args, "-select_streams", strconv.Itoa(font.Index), "-show_entries", "stream=extradata", "-show_data", "-of", "json", b.url)
		data, err := c.service.Runner.runLimit(ctx, c.service.Runner.ffprobe, args, c.service.Runner.resources, 5<<20)
		if b.exceeded.Load() {
			return nil, 0, 0, fmt.Errorf("RESOURCE_LIMIT")
		}
		if err != nil {
			return nil, 0, 0, err
		}
		var raw struct {
			Streams []rawStream `json:"streams"`
		}
		if json.Unmarshal(data, &raw) != nil || len(raw.Streams) != 1 {
			return nil, 0, 0, ErrUnsupported
		}
		data, err = decodeDump(raw.Streams[0].Extra)
		if err != nil || len(data) != font.Size || !validateFont(data) {
			return nil, 0, 0, ErrUnsupported
		}
		return data, 0, c.probe.Duration, nil
	}
	// The player supports delay [-60,+60]. Include a full 120s cue behind
	// positive delay and leave refresh headroom beyond maximum negative delay.
	args = append(args, "-nostdin", "-copyts", "-start_at_zero")
	if t.Sidecar == nil {
		// A sequential demuxer must start early enough to discover a long cue
		// overlapping the visible interval; the indexed path does this by duration.
		readStart := math.Max(0, start-120)
		args = append(args, "-ss", fmt.Sprint(readStart), "-t", fmt.Sprint(end-readStart))
		args = append(args, inputOptions...)
	} else {
		args = append(args, "-protocol_whitelist", "http,tcp", "-format_whitelist", t.InputFormat, "-f", t.InputFormat, "-max_alloc", "8388608")
	}
	args = append(args, "-i", b.url, "-map", fmt.Sprintf("0:%d", t.Index), "-vn", "-an", "-dn", "-c:s", map[string]string{"vtt": "webvtt", "ass": "ass"}[t.Format], "-f", map[string]string{"vtt": "webvtt", "ass": "ass"}[t.Format], "pipe:1")
	data, err := c.service.Runner.runLimit(ctx, c.service.Runner.ffmpeg, args, c.service.Runner.resources, textLimit)
	if b.exceeded.Load() {
		return nil, 0, 0, fmt.Errorf("RESOURCE_LIMIT")
	}
	if err != nil {
		return nil, 0, 0, err
	}
	data, err = subtitleWindow(data, t.Format, start, end)
	return data, cacheStart, cacheEnd, err
}

func (p *Playback) indexedText(ctx context.Context, t Text, start, end float64, reader *matroskaReader) ([]byte, bool, error) {
	c := p.captions
	if reader == nil {
		reader = &matroskaReader{ctx: ctx, backend: c.service.Backend, hash: c.hash, file: c.file}
	}
	if !c.indexChecked {
		// Probe belongs to this session/file and survives audio/seek replacement.
		// Reuse validated block locations; all resource URLs/grants still rotate.
		shared := &c.probe.captionCache
		shared.Lock()
		checked, index := shared.checked && shared.hash == c.hash && shared.file == c.file, shared.value
		shared.Unlock()
		var err error
		if !checked {
			index, err = loadMatroskaIndex(reader)
		}
		if err != nil && !errors.Is(err, errMatroskaIndex) {
			return nil, false, err
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if errors.Is(err, errMatroskaIndex) {
			index = nil
		}
		p.mu.Lock()
		if p.ctx.Err() != nil || ctx.Err() != nil {
			p.mu.Unlock()
			return nil, false, context.Canceled
		}
		if index != nil {
			size := index.memory()
			if c.bytes+size > resourceLimit || p.status.Bytes+int64(size) > cacheLimit {
				p.mu.Unlock()
				return nil, false, errors.New("RESOURCE_LIMIT")
			}
			c.bytes += size
			p.status.Bytes += int64(size)
		}
		c.index = index
		c.indexChecked = true
		shared.Lock()
		if shared.hash != c.hash || shared.file != c.file {
			shared.resources = nil
		}
		shared.value, shared.checked, shared.hash, shared.file = index, true, c.hash, c.file
		shared.Unlock()
		p.mu.Unlock()
	}
	if c.index == nil {
		return nil, false, nil
	}
	input, err := c.index.window(reader, t.Index, t.Format, start+c.probe.sourceStart, end+c.probe.sourceStart)
	if errors.Is(err, errMatroskaIndex) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(input) == 0 {
		data, err := subtitleWindow([]byte("WEBVTT\n\n"), "vtt", start, end)
		return data, true, err
	}
	format := map[string]string{"vtt": "webvtt", "ass": "ass"}[t.Format]
	args := []string{"-v", "error", "-nostdin", "-copyts", "-itsoffset", strconv.FormatFloat(-c.probe.sourceStart, 'f', 6, 64), "-protocol_whitelist", "pipe", "-format_whitelist", "matroska,webm", "-max_streams", "1", "-max_alloc", "67108864", "-i", "pipe:0", "-map", "0:0", "-c:s", format, "-f", format, "pipe:1"}
	data, err := c.service.Runner.runInputLimit(ctx, c.service.Runner.ffmpeg, args, c.service.Runner.resources, textLimit, input)
	if err != nil {
		return nil, false, err
	}
	data, err = subtitleWindow(data, t.Format, start, end)
	return data, true, err
}

var cueClock = regexp.MustCompile(`^(?:(\d{1,6}):)?(\d{2}):(\d{2})[.,](\d{2,3})$`)

func cueTime(value string) float64 {
	parts := cueClock.FindStringSubmatch(strings.TrimSpace(value))
	if parts == nil {
		return math.NaN()
	}
	h, m, s := number(parts[1]), number(parts[2]), number(parts[3])+number("0."+parts[4])
	if h < 0 || m < 0 || m >= 60 || s < 0 || s >= 60 {
		return math.NaN()
	}
	return h*3600 + m*60 + s
}

// Keep authored ASS styles, but exclude embedded resource sections. All cue
// timestamps stay absolute; the player applies the measured video timeline.
func subtitleWindow(data []byte, format string, start, end float64) ([]byte, error) {
	if len(data) > textLimit || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return nil, ErrUnsupported
	}
	text := strings.ReplaceAll(string(data), "\r", "")
	count := 0
	overlaps := func(a, b string) (bool, error) {
		from, to := cueTime(a), cueTime(b)
		count++
		if !finite(from) || !finite(to) || to <= from || to-from > 120 || count > 10000 {
			return false, ErrUnsupported
		}
		return to > start && from < end, nil
	}
	var out []string
	if format == "vtt" {
		for _, block := range strings.Split(text, "\n\n") {
			lines := strings.Split(block, "\n")
			for _, line := range lines {
				if strings.Contains(line, "-->") {
					fields := strings.Fields(line)
					if len(fields) < 3 || fields[1] != "-->" {
						return nil, ErrUnsupported
					}
					keep, err := overlaps(fields[0], fields[2])
					if err != nil {
						return nil, err
					}
					if keep {
						out = append(out, block)
					}
					break
				}
			}
		}
		if len(out) == 0 {
			out = append(out, "00:00:00.000 --> 00:00:00.001\n\u200b")
		}
		return []byte("WEBVTT\n\n" + strings.Join(out, "\n\n") + "\n"), nil
	}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.TrimSpace(line))
		}
		if section != "[script info]" && section != "[v4+ styles]" && section != "[v4 styles]" && section != "[events]" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "dialogue:") {
			fields := strings.SplitN(strings.TrimPrefix(line, line[:strings.Index(line, ":")+1]), ",", 10)
			if len(fields) != 10 {
				return nil, ErrUnsupported
			}
			keep, err := overlaps(fields[1], fields[2])
			if err != nil {
				return nil, err
			}
			if !keep {
				continue
			}
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n")), nil
}
