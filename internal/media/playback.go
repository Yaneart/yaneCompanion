package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"yanecompanion/internal/engine"
)

type SourceTimeline struct {
	Mode           string  `json:"mode"`
	Continuous     bool    `json:"continuous"`
	AbsoluteAnchor float64 `json:"absoluteAnchorSeconds"`
	MediaAnchor    float64 `json:"mediaAnchorSeconds"`
	Duration       float64 `json:"durationSeconds"`
	WindowEnd      float64 `json:"windowEndSeconds"`
	Ended          bool    `json:"ended"`
}
type AvailableRange struct {
	Start float64 `json:"startSeconds"`
	End   float64 `json:"endSeconds"`
}
type PlaybackStatus struct {
	Timeline   SourceTimeline   `json:"timeline"`
	Ranges     []AvailableRange `json:"availableRanges"`
	Bytes      int64            `json:"cacheBytes"`
	Downloaded int64            `json:"downloadedBytes"`
	Error      string           `json:"error,omitempty"`
	Output     OutputInfo       `json:"output"`
}
type OutputAudio struct {
	Codec    string `json:"codec"`
	Channels int    `json:"channels"`
	Mode     string `json:"mode"`
}
type OutputInfo struct {
	Video OutputVideo  `json:"video"`
	Audio *OutputAudio `json:"audio,omitempty"`
}
type OutputVideo struct {
	Video
	Mode string `json:"mode"`
}
type Playback struct {
	mu               sync.Mutex
	ctx              context.Context
	cancel           context.CancelFunc
	done, ready      chan struct{}
	closeOnce        sync.Once
	root             *os.Root
	directory        string
	input            *inputBridge
	status           PlaybackStatus
	segments         []segment
	target           int
	position, window float64
	confirmed        bool
	lastChange       time.Time
	readers          chan struct{}
	openFiles        map[string]int
	captions         *captions
}

func (s *Service) Prepare(ctx context.Context, hash string, file engine.File, probe *Probe, target Target, client []ClientMedia) (*Playback, error) {
	startupDeadline := time.Now().Add(120 * time.Second)
	audio, copyAudio, err := probe.Validate(target, client)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	directory := filepath.Join(s.Directory, "media-"+randomRef())
	if err = os.MkdirAll(directory, 0700); err != nil {
		cancel()
		return nil, errors.New("RESOURCE_LIMIT")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		cancel()
		_ = os.RemoveAll(directory)
		return nil, err
	}
	p := &Playback{ctx: ctx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}), root: root, directory: directory, position: target.Position, window: WindowSeconds(probe.Video.Bitrate, 8), lastChange: time.Now(), readers: make(chan struct{}, 8), openFiles: map[string]int{}}
	p.captions = &captions{service: s, hash: hash, file: file, probe: probe, busy: make(chan struct{}, 1), cache: map[string]resource{}}
	b, err := bridge(ctx, s.Backend, hash, file, 0)
	if err != nil {
		cancel()
		root.Close()
		_ = os.RemoveAll(directory)
		return nil, err
	}
	p.input = b
	// Measure FFmpeg's actual seek/copy result. Requested -ss and ffprobe's
	// independent seek are not evidence of which keyframe FFmpeg will copy.
	seek := []string{}
	if target.Position > 0 {
		seek = []string{"-ss", strconv.FormatFloat(target.Position, 'f', 6, 64)}
	}
	args := append([]string{"-v", "error", "-nostdin", "-copyts", "-start_at_zero"}, seek...)
	args = append(args, "-noaccurate_seek")
	args = append(args, inputOptions...)
	args = append(args, "-i", b.url, "-map", fmt.Sprintf("0:%d", probe.Video.Index), "-frames:v", "1", "-c:v", "copy", "-an", "-sn", "-dn", "-f", "framehash", "pipe:1")
	measurement, err := s.Runner.Prepare(ctx, args)
	anchor := frameAnchor(measurement)
	if err != nil || !finite(anchor) || anchor < -0.1 || anchor > target.Position+1 || target.Position-math.Max(0, anchor) > p.window-2 {
		cancel()
		b.Close()
		root.Close()
		_ = os.RemoveAll(directory)
		if err != nil {
			return nil, err
		}
		return nil, ErrUnsupported
	}
	anchor = math.Max(0, anchor)
	p.status = PlaybackStatus{Timeline: SourceTimeline{Mode: "windowed-vod", Continuous: true, AbsoluteAnchor: anchor, Duration: probe.Duration, WindowEnd: anchor}, Ranges: []AvailableRange{}}
	p.status.Output.Video = OutputVideo{Video: probe.Video, Mode: "copy"}
	if audio != nil {
		out := &OutputAudio{Codec: audio.Codec, Channels: audio.Channels, Mode: "copy"}
		if !copyAudio {
			out.Codec = "aac"
			out.Channels = 2
			out.Mode = "transcode"
		}
		p.status.Output.Audio = out
	}
	args = append([]string{"-v", "error", "-nostdin", "-filter_threads", "2"}, seek...)
	args = append(args, "-noaccurate_seek")
	args = append(args, inputOptions...)
	args = append(args, "-readrate", "4", "-i", b.url, "-avoid_negative_ts", "make_zero", "-map", fmt.Sprintf("0:%d", probe.Video.Index), "-c:v", "copy")
	if probe.Video.Codec == "hevc" {
		args = append(args, "-tag:v", "hvc1")
	}
	if audio != nil {
		args = append(args, "-map", fmt.Sprintf("0:%d", audio.Index))
		if copyAudio {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-profile:a", "aac_low", "-b:a", "192k", "-ac", "2", "-ar", "48000")
		}
	} else {
		args = append(args, "-an")
	}
	// An OS pipe provides cross-platform backpressure even for tiny low-bitrate
	// inputs already buffered by the demuxer. One muxer/audio clock lives to EOF.
	args = append(args, "-sn", "-dn", "-threads", "2", "-f", "mp4", "-strict", "experimental", "-movflags", "empty_moov+default_base_moof+frag_keyframe", "-frag_duration", "2000000", "-min_frag_duration", "1000000", "pipe:1")
	go p.run(s.Runner, args)
	timer := time.NewTimer(time.Until(startupDeadline))
	defer timer.Stop()
	select {
	case <-p.ready:
		return p, nil
	case <-p.done:
		err := p.Status().Error
		p.Close()
		if err == "" {
			err = "MEDIA_FAILED"
		}
		return nil, errors.New(err)
	case <-timer.C:
		p.Close()
		return nil, errors.New("NO_PEERS")
	case <-ctx.Done():
		p.Close()
		return nil, ctx.Err()
	}
}

var frameTB = regexp.MustCompile(`(?m)^#tb 0: (\d+)/(\d+)\r?$`)
var framePTS = regexp.MustCompile(`(?m)^0,\s*-?\d+,\s*(-?\d+),`)

func frameAnchor(data []byte) float64 {
	tb, pts := frameTB.FindSubmatch(data), framePTS.FindSubmatch(data)
	if tb == nil || pts == nil || number(string(tb[2])) <= 0 {
		return math.NaN()
	}
	return number(string(pts[1])) * number(string(tb[1])) / number(string(tb[2]))
}

func (p *Playback) run(runner *Runner, args []string) {
	defer close(p.done)
	defer p.input.Close()
	work, cancel := context.WithCancel(p.ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- runner.generate(work, args, p.consume) }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			p.mu.Lock()
			if err != nil && p.ctx.Err() == nil {
				if p.status.Error == "" {
					p.status.Error = "MEDIA_FAILED"
				}
			} else if err == nil {
				if p.status.Timeline.WindowEnd >= p.status.Timeline.Duration-1 && len(p.segments) > 0 {
					p.status.Timeline.Ended = true
				} else {
					p.status.Error = "MEDIA_FAILED"
				}
			}
			if p.status.Error == "" && len(p.segments) > 0 {
				select {
				case <-p.ready:
				default:
					close(p.ready)
				}
			}
			p.mu.Unlock()
			return
		case <-ticker.C:
			p.mu.Lock()
			stalled := !p.blocked() && time.Since(p.lastChange) > 120*time.Second
			if stalled {
				p.status.Error = "NO_PEERS"
			}
			p.mu.Unlock()
			if stalled {
				cancel()
			}
		case <-p.ctx.Done():
			cancel()
			<-result
			return
		}
	}
}

func (p *Playback) consume(ctx context.Context, r io.Reader) error {
	var init []byte
	var clock mp4Clock
	var origin float64
	sequence := 0
	// Keep one fragment until the next header/EOF. A final encoder flush can then
	// join its video fragment before publication, preserving every audio sample.
	var pending []byte
	var pendingTime fragmentTime
	audioTail := false
	publish := func(fragment []byte, timing fragmentTime) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		name := fmt.Sprintf("segment-%06d.m4s", sequence)
		if err = p.root.WriteFile(name, fragment, 0600); err != nil {
			return err
		}
		p.mu.Lock()
		if sequence == 0 {
			origin = timing.start
			p.status.Timeline.MediaAnchor = timing.pts - timing.minDecode
			if !finite(p.status.Timeline.MediaAnchor) || p.status.Timeline.MediaAnchor < 0 || p.status.Timeline.MediaAnchor > p.window {
				p.mu.Unlock()
				return ErrUnsupported
			}
		}
		start := p.status.Timeline.AbsoluteAnchor + timing.start - origin
		end := p.status.Timeline.AbsoluteAnchor + timing.end - origin
		if !finite(start) || !finite(end) || math.Abs(start-p.status.Timeline.WindowEnd) > .1 {
			p.mu.Unlock()
			return errPlaylist
		}
		p.segments = append(p.segments, segment{Name: name, Duration: end - start, Start: start, End: end, Sequence: sequence})
		if end-start > 3 {
			p.mu.Unlock()
			return errPlaylist
		}
		p.target = 3
		p.status.Bytes += int64(len(fragment))
		p.status.Downloaded = p.input.bytes.Load()
		p.status.Timeline.WindowEnd = end
		p.lastChange = time.Now()
		err = p.evict()
		if err == nil && (end >= p.position+6 || p.blocked() && end > p.position+0.1) {
			select {
			case <-p.ready:
			default:
				close(p.ready)
			}
		}
		if err == nil && p.blocked() {
			select {
			case <-p.ready:
			default:
				p.status.Error = "RESOURCE_LIMIT"
				err = errors.New("RESOURCE_LIMIT")
			}
		}
		p.mu.Unlock()
		if err != nil {
			return err
		}
		sequence++
		return nil
	}
	for {
		// A pending fragment needs one header of lookahead even at byte pressure.
		// After publishing it, wait before reading another bounded mdat.
		if pending == nil {
			if err := p.wait(ctx); err != nil {
				return err
			}
		}
		data, kind, err := readBox(r)
		if err == io.EOF {
			if pending != nil {
				return publish(pending, pendingTime)
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch kind {
		case "ftyp", "moov":
			if sequence > 0 || len(init)+len(data) > 2<<20 {
				return errPlaylist
			}
			init = append(init, data...)
			if kind == "moov" {
				clock, err = initClock(init)
				if err != nil {
					return err
				}
				if err = p.root.WriteFile("init.mp4", init, 0600); err != nil {
					return err
				}
				p.mu.Lock()
				p.status.Bytes = int64(len(init))
				p.mu.Unlock()
			}

		case "moof":
			if clock.video == 0 || audioTail {
				return errPlaylist
			}
			timing, err := clock.fragment(data)
			onlyAudio := errors.Is(err, errAudioOnly)
			if err != nil && !onlyAudio {
				return err
			}
			if !onlyAudio && pending != nil {
				if err := publish(pending, pendingTime); err != nil {
					return err
				}
				pending = nil
				if err := p.wait(ctx); err != nil {
					return err
				}
			}
			mdat, k, err := readBox(r)
			if err != nil || k != "mdat" || len(data)+len(mdat) > segmentLimit {
				return errPlaylist
			}
			if onlyAudio {
				anchor := origin
				if sequence == 0 {
					anchor = pendingTime.start
				}
				p.mu.Lock()
				atEnd := p.status.Timeline.AbsoluteAnchor+pendingTime.end-anchor >= p.status.Timeline.Duration-.1
				p.mu.Unlock()
				// Only one <=100ms/64KiB encoder tail at absolute EOF may accompany the
				// pending video fragment. No invented video clock, discarded audio or
				// mutation of a fragment already served to a browser.
				if pending == nil || !atEnd || timing.minDecode < pendingTime.end-.1 || timing.minPTS < pendingTime.end-.1 || timing.allEnd > pendingTime.end+.1 || timing.allEnd-timing.minDecode > .1 || len(data)+len(mdat) > 64<<10 || len(pending)+len(data)+len(mdat) > segmentLimit {
					return ErrUnsupported
				}
				pending = append(pending, data...)
				pending = append(pending, mdat...)
				audioTail = true
			} else {
				pending = append(data, mdat...)
				pendingTime = timing
			}
			p.mu.Lock()
			overBudget := p.status.Bytes+int64(len(pending)) > cacheLimit
			p.mu.Unlock()
			if overBudget {
				return errors.New("RESOURCE_LIMIT")
			}
		case "free", "sidx", "mfra":
		default:
			return errPlaylist
		}
	}
}

func (p *Playback) evict() error {
	cutoff := p.position - 8
	if p.status.Bytes >= cacheTarget*3/4 {
		cutoff = p.position
	}
	// Requested seek is not confirmed playback. Preserve the initial keyframe
	// chain until the browser has actually decoded and reported its position.
	for p.confirmed && len(p.segments) > 1 && p.segments[0].End <= cutoff {
		item := p.segments[0]
		// Windows does not unlink an open os.File. Retain it until the bounded
		// response closes; byte backpressure still applies while it is pinned.
		if p.openFiles[item.Name] > 0 {
			break
		}
		info, err := p.root.Stat(item.Name)
		if err != nil {
			return err
		}
		if err = p.root.Remove(item.Name); err != nil {
			return err
		}
		p.status.Bytes -= info.Size()
		p.segments = p.segments[1:]
	}
	if p.status.Bytes > cacheLimit || len(p.segments) > 64 {
		return errPlaylist
	}
	if len(p.segments) > 0 {
		p.status.Ranges = []AvailableRange{{p.segments[0].Start, math.Min(p.status.Timeline.WindowEnd, p.status.Timeline.Duration)}}
	}
	return nil
}
func (p *Playback) blocked() bool {
	return p.status.Bytes >= cacheTarget || len(p.segments) >= 32 || p.status.Timeline.WindowEnd-p.position >= p.window
}
func (p *Playback) wait(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		p.mu.Lock()
		blocked := p.blocked()
		if blocked {
			select {
			case <-p.ready:
			default:
				p.status.Error = "RESOURCE_LIMIT"
				p.mu.Unlock()
				return errors.New("RESOURCE_LIMIT")
			}
			p.lastChange = time.Now()
		}
		p.mu.Unlock()
		if !blocked {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (p *Playback) Status() PlaybackStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.status
	s.Ranges = append([]AvailableRange{}, s.Ranges...)
	return s
}
func (p *Playback) Confirm(position float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !finite(position) || len(p.segments) == 0 || position < p.segments[0].Start-.1 || position > p.status.Timeline.WindowEnd+.1 || position > p.status.Timeline.Duration+.1 {
		return errors.New("INVALID_POSITION")
	}
	p.position = math.Max(p.segments[0].Start, position)
	p.confirmed = true
	return p.evict()
}
func (p *Playback) Close() {
	p.closeOnce.Do(func() {
		p.cancel()
		p.input.Close()
		<-p.done
		// Wait for already-open Windows file handles before removing this source.
		for i := 0; i < cap(p.readers); i++ {
			p.readers <- struct{}{}
		}
		p.root.Close()
		_ = os.RemoveAll(p.directory)
		for i := 0; i < cap(p.readers); i++ {
			<-p.readers
		}
	})
}

// Serve exposes only generated resources. Paths never come from media metadata.
func (p *Playback) Serve(w http.ResponseWriter, r *http.Request, name string) {
	select {
	case p.readers <- struct{}{}:
		defer func() { <-p.readers }()
	default:
		http.Error(w, "RESOURCE_LIMIT", 429)
		return
	}
	p.mu.Lock()
	if p.ctx.Err() != nil || p.status.Error != "" || len(p.segments) == 0 {
		p.mu.Unlock()
		http.Error(w, "MEDIA_UNAVAILABLE", 410)
		return
	}
	if name == "master.m3u8" || name == "media.m3u8" {
		// Generated media resources below use the same source lifetime and readers.
		var b strings.Builder
		if name == "master.m3u8" {
			b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-STREAM-INF:BANDWIDTH=100000000\nmedia.m3u8\n")
		} else {
			fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-MAP:URI=\"init.mp4\"\n", p.target, p.segments[0].Sequence)
			for _, item := range p.segments {
				fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s\n", item.Duration, item.Name)
			}
			if p.status.Timeline.Ended {
				b.WriteString("#EXT-X-ENDLIST\n")
			}
		}
		data := b.String()
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method != "HEAD" {
			_, _ = io.WriteString(w, data)
		}
		return
	}
	if p.captions != nil && (strings.HasPrefix(name, "text-") || strings.HasPrefix(name, "font-")) {
		p.mu.Unlock()
		p.serveResource(w, r, name)
		return
	}
	allowed := name == "init.mp4"
	for _, item := range p.segments {
		allowed = allowed || name == item.Name
	}
	if !allowed {
		p.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	file, err := p.root.Open(name)
	if err == nil {
		p.openFiles[name]++
	}
	p.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() {
		file.Close()
		p.mu.Lock()
		p.openFiles[name]--
		if p.openFiles[name] == 0 {
			delete(p.openFiles, name)
		}
		p.mu.Unlock()
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > segmentLimit {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	if r.Method == "HEAD" {
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
	stop := context.AfterFunc(p.ctx, func() { _ = controller.SetWriteDeadline(time.Now()) })
	defer stop()
	buf := make([]byte, 64<<10)
	for {
		if p.ctx.Err() != nil || r.Context().Err() != nil {
			return
		}
		n, err := file.Read(buf)
		if n > 0 {
			if _, e := w.Write(buf[:n]); e != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
