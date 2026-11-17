package media

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"yanecompanion/internal/engine"
)

// Renditions is a source-scoped video producer and one lazily selected audio
// producer. Session/API adapters must admit selection before exposing its URL.
type Renditions struct {
	mu                            sync.Mutex
	ctx                           context.Context
	cancel                        context.CancelFunc
	selection                     chan struct{}
	service                       *Service
	hash                          string
	file                          engine.File
	probe                         *Probe
	client                        []ClientMedia
	video, audio, retired         *Playback
	audioRef, audioGeneration     string
	audioPosition                 *float64
	retiredRef, retiredGeneration string
	closed                        bool
	readers                       chan struct{}
	closeDone                     chan struct{}
}

type AudioRendition struct {
	AudioRef string `json:"audioRef"`
	// Never reuse init/fragment names when a previous selection is regenerated.
	Generation string   `json:"generation"`
	Duration   float64  `json:"durationSeconds"`
	Position   *float64 `json:"positionSeconds,omitempty"`
}

func (s *Service) PrepareRenditions(ctx context.Context, hash string, file engine.File, probe *Probe, target Target, client []ClientMedia) (*Renditions, error) {
	audio, _, err := probe.Validate(target, client)
	if err != nil {
		return nil, err
	}
	// Unknown per-track ends cannot establish selected duration/EOF safely.
	if !probe.supportsRenditions() {
		return nil, ErrUnsupported
	}
	ctx, cancel := context.WithCancel(ctx)
	r := &Renditions{ctx: ctx, cancel: cancel, selection: make(chan struct{}, 1), readers: make(chan struct{}, 8), closeDone: make(chan struct{}), service: s, hash: hash, file: file, probe: probe, client: append([]ClientMedia(nil), client...)}
	// The original startup budget covers both producers together. Once ready,
	// their lifetime belongs to the session and confirmed playback progress.
	startup := time.AfterFunc(120*time.Second, cancel)
	defer startup.Stop()
	position := math.Min(target.Position, math.Max(0, probe.videoEnd-.01))
	r.video, err = r.prepareTrack(ctx, nil, position, 0)
	if err == nil && audio != nil {
		_, err = r.SelectAudio(ctx, audio.Ref, position)
	}
	if err != nil {
		r.Close()
		return nil, err
	}
	r.video.captions = &captions{service: s, hash: hash, file: file, probe: probe, busy: make(chan struct{}, 1), cache: map[string]resource{}}
	r.video.restoreCaptionCache()
	return r, nil
}

func (p *Probe) supportsRenditions() bool {
	if p.videoEnd <= 0 {
		return false
	}
	for _, track := range p.Audio {
		if track.end <= 0 {
			return false
		}
	}
	return true
}

// Selection is serialized. A cancelled or superseded producer cannot publish
// after Close. The video worker, its directory and its clock are retained.
func (r *Renditions) SelectAudio(ctx context.Context, ref string, position float64) (AudioRendition, error) {
	if err := ctx.Err(); err != nil {
		return AudioRendition{}, err
	}
	select {
	case r.selection <- struct{}{}:
		defer func() { <-r.selection }()
	case <-ctx.Done():
		return AudioRendition{}, ctx.Err()
	case <-r.ctx.Done():
		return AudioRendition{}, r.ctx.Err()
	}
	r.mu.Lock()
	if r.closed || ctx.Err() != nil {
		r.mu.Unlock()
		return AudioRendition{}, context.Canceled
	}
	old := r.audio
	if old != nil && r.audioRef == ref {
		status := old.Status()
		if status.Error == "" && len(status.Ranges) > 0 && position >= status.Ranges[0].Start && position < status.Timeline.WindowEnd {
			value := AudioRendition{ref, r.audioGeneration, r.probe.playbackDuration(r.findAudio(ref)), r.audioPosition}
			r.mu.Unlock()
			return value, ctx.Err()
		}
	}
	audio := r.findAudio(ref)
	video := r.video.Status()
	var corrected *float64
	if audio != nil && finite(position) && position >= r.probe.playbackDuration(audio) {
		position = math.Max(0, math.Min(r.probe.videoEnd, r.probe.playbackDuration(audio))-.01)
		corrected = &position
	}
	if audio == nil || audio.end <= 0 || !finite(position) || len(video.Ranges) == 0 || position < video.Ranges[0].Start || position >= r.probe.playbackDuration(audio) || !video.Timeline.Ended && position > video.Timeline.WindowEnd+.1 {
		r.mu.Unlock()
		return AudioRendition{}, ErrUnsupported
	}
	// Keep the audible producer until its replacement is ready. The two audio
	// slots have 8 MiB each; together with video they still total 128 MiB.
	retired := r.retired
	r.retired = nil
	r.retiredRef, r.retiredGeneration = "", ""
	lane := 0
	if old != nil {
		lane = 1 - old.audioLane
	}
	r.mu.Unlock()
	if retired != nil {
		retired.Close()
	}
	work, cancel := context.WithCancel(r.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	p, err := r.prepareTrack(work, audio, math.Max(0, math.Min(position-2, audio.end-.01)), lane)
	if err != nil {
		cancel()
		return AudioRendition{}, err
	}
	// From the publication point the producer belongs to the source. A waiter
	// cancelled after commit reconciles its receipt; it cannot kill ready media.
	stop()
	r.mu.Lock()
	if r.closed || ctx.Err() != nil || r.ctx.Err() != nil {
		r.mu.Unlock()
		cancel()
		p.Close()
		return AudioRendition{}, context.Canceled
	}
	r.retired, r.retiredRef, r.retiredGeneration = old, r.audioRef, r.audioGeneration
	r.audio, r.audioRef, r.audioGeneration = p, ref, randomRef()
	r.audioPosition = corrected
	value := AudioRendition{ref, r.audioGeneration, r.probe.playbackDuration(audio), corrected}
	r.mu.Unlock()
	if old != nil {
		old.stopProducer()
	}
	return value, nil
}

func (r *Renditions) findAudio(ref string) *Audio {
	for i := range r.probe.Audio {
		if r.probe.Audio[i].Ref == ref {
			return &r.probe.Audio[i]
		}
	}
	return nil
}

func (r *Renditions) prepareTrack(ctx context.Context, audio *Audio, position float64, lane int) (*Playback, error) {
	ctx, cancel := context.WithCancel(ctx)
	directory, err := os.MkdirTemp(r.service.Directory, "rendition-")
	if err != nil {
		cancel()
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		cancel()
		_ = os.RemoveAll(directory)
		return nil, err
	}
	p := &Playback{ctx: ctx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}), root: root, directory: directory, position: position, window: WindowSeconds(r.probe.Video.Bitrate, 8), lastChange: time.Now(), readers: make(chan struct{}, 8), openFiles: map[string]int{}, trackHandler: "vide", byteLimit: 112 << 20, byteTarget: cacheTarget}
	index, end := r.probe.Video.Index, r.probe.videoEnd
	codec := []string{"-c:v", "copy", "-an"}
	p.status.Output.Video = OutputVideo{Video: r.probe.Video, Mode: "copy"}
	if r.probe.Video.Codec == "hevc" {
		codec = append(codec, "-tag:v", "hvc1")
	}
	if audio != nil {
		index, end, p.trackHandler = audio.Index, audio.end, "soun"
		p.byteLimit, p.byteTarget, p.window, p.audioLane = 8<<20, 2<<20, 45, lane
		_, copyAudio, validateErr := r.probe.Validate(Target{AudioRef: audio.Ref, Position: position, Output: "original"}, r.client)
		if validateErr != nil {
			cancel()
			root.Close()
			_ = os.RemoveAll(directory)
			return nil, validateErr
		}
		codec = []string{"-vn", "-c:a", "copy"}
		p.status.Output.Audio = &OutputAudio{Codec: audio.Codec, Channels: audio.Channels, Mode: "copy"}
		if !copyAudio {
			codec = []string{"-vn", "-c:a", "aac", "-profile:a", "aac_low", "-b:a", "192k", "-ac", "2", "-ar", "48000"}
			p.status.Output.Audio = &OutputAudio{Codec: "aac", Channels: 2, Mode: "transcode"}
		}
	}
	p.status.Timeline = SourceTimeline{Mode: "windowed-vod", Continuous: true, Duration: end, VideoEnd: r.probe.videoEnd}
	p.input, err = bridge(ctx, r.service.Backend, r.hash, r.file, 0)
	if err != nil {
		cancel()
		root.Close()
		_ = os.RemoveAll(directory)
		return nil, err
	}
	args := []string{"-v", "error", "-nostdin", "-copyts", "-start_at_zero", "-filter_threads", "2"}
	if position > 0 {
		args = append(args, "-ss", strconv.FormatFloat(position, 'f', 6, 64))
	}
	args = append(args, "-noaccurate_seek")
	args = append(args, inputOptions...)
	args = append(args, "-readrate", "4", "-i", p.input.url, "-map", fmt.Sprintf("0:%d", index))
	args = append(args, codec...)
	// frag_discont retains absolute DTS in tfdt, as FFmpeg's HLS muxer does.
	// Otherwise standalone MP4 output silently rebases each worker to zero.
	args = append(args, "-sn", "-dn", "-map_chapters", "-1", "-avoid_negative_ts", "disabled", "-output_ts_offset", "1", "-threads", "2", "-f", "mp4", "-use_editlist", "0", "-movflags", "empty_moov+default_base_moof+frag_keyframe+frag_discont", "-frag_duration", "2000000", "-min_frag_duration", "1000000", "pipe:1")
	go p.run(r.service.Runner, args)
	timer := time.NewTimer(120 * time.Second)
	defer timer.Stop()
	select {
	case <-p.ready:
		if ctx.Err() != nil {
			p.Close()
			return nil, ctx.Err()
		}
		return p, nil
	case <-p.done:
		status := p.Status()
		if status.Error == "" && status.Timeline.Ended && len(status.Ranges) > 0 {
			return p, nil
		}
		p.Close()
		if p.failure != nil {
			return nil, p.failure
		}
		return nil, errors.New("MEDIA_FAILED")
	case <-ctx.Done():
		p.Close()
		return nil, ctx.Err()
	case <-timer.C:
		p.Close()
		return nil, errors.New("NO_PEERS")
	}
}

func (r *Renditions) Status() PlaybackStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.video == nil {
		return PlaybackStatus{Error: "MEDIA_UNAVAILABLE"}
	}
	status := r.video.Status()
	if r.retired != nil {
		status.Bytes += r.retired.Status().Bytes
	}
	if r.audio == nil {
		return status
	}
	audio := r.audio.Status()
	status.Bytes += audio.Bytes
	status.Downloaded += audio.Downloaded
	status.Output.Audio = audio.Output.Audio
	status.Timeline.Duration = r.probe.playbackDuration(r.findAudio(r.audioRef))
	start, end := status.Timeline.AbsoluteAnchor, status.Timeline.WindowEnd
	if len(status.Ranges) > 0 {
		start = status.Ranges[0].Start
	}
	if len(audio.Ranges) > 0 {
		start = math.Max(start, audio.Ranges[0].Start)
	}
	if status.Timeline.Ended && audio.Timeline.Ended {
		end = math.Max(end, audio.Timeline.WindowEnd)
	} else if status.Timeline.Ended {
		end = audio.Timeline.WindowEnd
	} else if !audio.Timeline.Ended {
		end = math.Min(end, audio.Timeline.WindowEnd)
	}
	status.Timeline.WindowEnd = math.Min(end, status.Timeline.Duration)
	status.Ranges = nil
	if status.Timeline.WindowEnd > start {
		status.Ranges = []AvailableRange{{start, status.Timeline.WindowEnd}}
	}
	status.Timeline.Ended = status.Timeline.Ended && audio.Timeline.Ended
	if audio.Error != "" {
		status.Error = audio.Error
	}
	return status
}

func (r *Renditions) Confirm(position float64, delay ...float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.video == nil || !finite(position) {
		return errors.New("INVALID_POSITION")
	}
	v := r.video.Status()
	if len(v.Ranges) == 0 || position < v.Ranges[0].Start-.1 {
		return errors.New("INVALID_POSITION")
	}
	if r.audio != nil {
		a := r.audio.Status()
		if !v.Timeline.Ended && position > v.Timeline.WindowEnd+.1 || !a.Timeline.Ended && position > a.Timeline.WindowEnd+.1 || position > r.probe.playbackDuration(r.findAudio(r.audioRef))+.1 {
			return errors.New("INVALID_POSITION")
		}
		if err := r.audio.Confirm(math.Min(position, a.Timeline.WindowEnd), delay...); err != nil {
			return err
		}
	}
	if err := r.video.Confirm(math.Min(position, v.Timeline.WindowEnd), delay...); err != nil {
		return err
	}
	r.video.mu.Lock()
	r.video.captionPosition = &position
	r.video.mu.Unlock()
	return nil
}

func (r *Renditions) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		<-r.closeDone
		return
	}
	r.closed = true
	r.cancel()
	v, a, retired := r.video, r.audio, r.retired
	r.mu.Unlock()
	if v != nil {
		v.Close()
	}
	if a != nil {
		a.Close()
	}
	if retired != nil {
		retired.Close()
	}
	// Wait for an admitted selection to clean up its unpublished producer.
	r.selection <- struct{}{}
	<-r.selection
	close(r.closeDone)
}

func (p *Playback) SeparateAudio() bool { return p.renditions != nil }

func (p *Playback) AudioRendition() *AudioRendition {
	if p.renditions == nil {
		return nil
	}
	r := p.renditions
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.audio == nil {
		return nil
	}
	return &AudioRendition{r.audioRef, r.audioGeneration, r.probe.playbackDuration(r.findAudio(r.audioRef)), r.audioPosition}
}

func (p *Playback) SelectAudio(ctx context.Context, ref string, position float64) (AudioRendition, error) {
	if p.renditions == nil {
		return AudioRendition{}, ErrUnsupported
	}
	return p.renditions.SelectAudio(ctx, ref, position)
}

// Session ownership, Origin and read-grant validation remain in the HTTP layer.
// Every resource name is source-scoped; regenerated audio gets new init/segment
// names so a delayed read can never receive another producer's bytes.
func (r *Renditions) Serve(w http.ResponseWriter, request *http.Request, name string) {
	select {
	case r.readers <- struct{}{}:
		defer func() { <-r.readers }()
	default:
		http.Error(w, "RESOURCE_LIMIT", 429)
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		http.NotFound(w, request)
		return
	}
	var p *Playback
	prefix, resource := "", ""
	if name == "master.m3u8" {
		var b strings.Builder
		b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
		for index, a := range r.probe.Audio {
			if a.end <= 0 {
				continue
			}
			selected := "NO"
			if a.Ref == r.audioRef {
				selected = "YES"
			}
			label := strings.NewReplacer("\"", "'", "\r", " ", "\n", " ").Replace(a.Label)
			fmt.Fprintf(&b, "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"%s (%d)\",DEFAULT=%s,AUTOSELECT=%s,URI=\"audio-%s.m3u8\"\n", label, index+1, selected, selected, a.Ref)
		}
		if len(r.probe.Audio) > 0 {
			b.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=100000000,AUDIO=\"audio\"\n")
		} else {
			b.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=100000000\n")
		}
		b.WriteString("video-media.m3u8\n")
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if request.Method != "HEAD" {
			_, _ = fmt.Fprint(w, b.String())
		}
		return
	}
	if strings.HasPrefix(name, "video-") {
		p, prefix, resource = r.video, "video-", strings.TrimPrefix(name, "video-")
	} else if r.audio != nil {
		prefix = "audio-" + r.audioRef + "-" + r.audioGeneration + "-"
		if name == "audio-"+r.audioRef+".m3u8" {
			p, resource = r.audio, "media.m3u8"
		} else if strings.HasPrefix(name, prefix) {
			p, resource = r.audio, strings.TrimPrefix(name, prefix)
		}
	}
	if p == nil && r.retired != nil {
		previous := "audio-" + r.retiredRef + "-" + r.retiredGeneration + "-"
		if strings.HasPrefix(name, previous) {
			p, prefix, resource = r.retired, previous, strings.TrimPrefix(name, previous)
		}
	}
	if strings.HasPrefix(name, "text-") || strings.HasPrefix(name, "font-") {
		p, resource = r.video, name
	}
	if p == nil {
		r.mu.Unlock()
		http.NotFound(w, request)
		return
	}
	if resource == "media.m3u8" {
		p.mu.Lock()
		if p.ctx.Err() != nil || len(p.segments) == 0 || p.status.Error != "" {
			p.mu.Unlock()
			r.mu.Unlock()
			http.NotFound(w, request)
			return
		}
		var b strings.Builder
		fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-MAP:URI=\"%sinit.mp4\"\n", p.target, p.segments[0].Sequence, prefix)
		// Use the same file clock for independently prepared renditions. Sequence
		// numbers restart on selection, so they cannot align late audio to video.
		clock := p.programOrigin + p.segments[0].Start - p.status.Timeline.AbsoluteAnchor
		date := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(math.Round(clock*1000)) * time.Millisecond)
		fmt.Fprintf(&b, "#EXT-X-PROGRAM-DATE-TIME:%s\n", date.Format("2006-01-02T15:04:05.000Z"))
		for _, item := range p.segments {
			fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s%s\n", item.Duration, prefix, item.Name)
		}
		if p.status.Timeline.Ended {
			b.WriteString("#EXT-X-ENDLIST\n")
		}
		p.mu.Unlock()
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if request.Method != "HEAD" {
			_, _ = fmt.Fprint(w, b.String())
		}
		return
	}
	r.mu.Unlock()
	p.Serve(w, request, resource)
}
