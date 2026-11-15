package media

// Same indexed extraction as Home's matroska-subtitle-index.ts: fetch only
// metadata and complete subtitle blocks, then let FFmpeg decode their codecs.
import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"

	"yanecompanion/internal/engine"
)

var errMatroskaIndex = errors.New("subtitle index unavailable")

type ebmlElement struct {
	id, start, data, end int64
	unknown              bool
}
type subtitleCue struct{ time, duration, cluster, relative int64 }
type indexedTrack struct {
	number int64
	stream int
	format string
	entry  []byte
	cues   []subtitleCue
	valid  bool
}
type matroskaIndex struct {
	size, segment int64
	header, info  []byte
	scale         float64
	tracks        []indexedTrack
	busy          chan struct{}
}
type matroskaReader struct {
	ctx         context.Context
	backend     RangeBackend
	hash        string
	file        engine.File
	bytes       int64
	requests    int
	scan        bool
	buffer      []byte
	bufferStart int64
	budget      *matroskaReadBudget
}

// Independent cluster walks share one job's limits, including in-flight reads.
type matroskaReadBudget struct {
	sync.Mutex
	bytes                    int64
	requests, blocks, output int
}

func (r *matroskaReader) read(offset, length int64) ([]byte, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if offset < 0 || length <= 0 || length > 2<<20 || offset > r.file.Length-length {
		return nil, errMatroskaIndex
	}
	if offset >= r.bufferStart && length > 0 && offset-r.bufferStart <= int64(len(r.buffer))-length {
		return append([]byte(nil), r.buffer[offset-r.bufferStart:offset-r.bufferStart+length]...), nil
	}
	wanted := length
	if r.scan && length > 0 && length < 16<<10 && offset >= 0 && offset < r.file.Length {
		length = min(int64(16<<10), r.file.Length-offset)
	}
	maxBytes, maxRequests := int64(8<<20), 512
	if r.scan {
		// This replaces the demux fallback for incomplete indexes. It uses that
		// path's existing 128MiB input budget, never a larger media job budget.
		maxBytes, maxRequests = 128<<20, 8192
	}
	if b := r.budget; b != nil {
		b.Lock()
		if b.requests >= maxRequests || b.bytes+length > maxBytes {
			b.Unlock()
			return nil, errMatroskaIndex
		}
		b.requests++
		b.bytes += length
		b.Unlock()
	} else if r.requests >= maxRequests || r.bytes+length > maxBytes {
		return nil, errMatroskaIndex
	}
	r.requests++
	r.bytes += length
	body, err := r.backend.OpenRange(r.ctx, r.hash, r.file, offset, offset+length-1)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, length+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != length {
		return nil, io.ErrUnexpectedEOF
	}
	r.buffer, r.bufferStart = data, offset
	return append([]byte(nil), data[:wanted]...), nil
}

func ebmlVint(data []byte, offset int64, id bool) (int64, int64, error) {
	if offset < 0 || offset >= int64(len(data)) || data[offset] == 0 {
		return 0, 0, errMatroskaIndex
	}
	first := data[offset]
	length := int64(1)
	mask := byte(0x80)
	for first&mask == 0 {
		length++
		mask >>= 1
	}
	if length > 8 || id && length > 4 || offset+length > int64(len(data)) {
		return 0, 0, errMatroskaIndex
	}
	value := int64(first)
	if !id {
		value = int64(first & (mask - 1))
	}
	for i := int64(1); i < length; i++ {
		value = value*256 + int64(data[offset+i])
	}
	if !id && value == (int64(1)<<(7*length))-1 {
		value = -1
	}
	return value, length, nil
}
func ebmlParse(data []byte, offset int64) (ebmlElement, error) {
	id, n, err := ebmlVint(data, offset, true)
	if err != nil {
		return ebmlElement{}, err
	}
	size, m, err := ebmlVint(data, offset+n, false)
	if err != nil {
		return ebmlElement{}, err
	}
	item := ebmlElement{id: id, start: offset, data: offset + n + m, end: -1, unknown: size < 0}
	if size >= 0 {
		item.end = item.data + size
	}
	return item, nil
}
func ebmlChildren(data []byte) ([]ebmlElement, error) {
	var out []ebmlElement
	for offset := int64(0); offset < int64(len(data)); {
		item, err := ebmlParse(data, offset)
		if err != nil || item.end < item.data || item.end > int64(len(data)) || len(out) >= 30000 {
			return nil, errMatroskaIndex
		}
		out = append(out, item)
		offset = item.end
	}
	return out, nil
}
func ebmlFields(data []byte) (map[int64][]byte, error) {
	items, err := ebmlChildren(data)
	if err != nil {
		return nil, err
	}
	out := map[int64][]byte{}
	for _, item := range items {
		if item.id == 0xec || item.id == 0xbf { // Void and CRC do not describe fields.
			continue
		}
		if _, exists := out[item.id]; exists {
			return nil, errMatroskaIndex
		}
		out[item.id] = data[item.data:item.end]
	}
	return out, nil
}
func ebmlUint(data []byte) (int64, error) {
	if len(data) == 0 || len(data) > 6 {
		return 0, errMatroskaIndex
	}
	n := int64(0)
	for _, b := range data {
		n = n*256 + int64(b)
	}
	return n, nil
}
func ebmlNumber(value int64) []byte {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], uint64(value))
	i := 0
	for i < 7 && data[i] == 0 {
		i++
	}
	return append([]byte(nil), data[i:]...)
}
func ebmlWrap(id int64, data []byte) []byte {
	length := 1
	for int64(len(data)) >= (int64(1)<<(7*length))-1 {
		length++
	}
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(data)))
	size[8-length] |= 1 << (8 - length)
	out := append(ebmlNumber(id), size[8-length:]...)
	return append(out, data...)
}
func (r *matroskaReader) header(offset int64) (ebmlElement, error) {
	if err := r.ctx.Err(); err != nil {
		return ebmlElement{}, err
	}
	var data []byte
	if offset >= r.bufferStart && offset-r.bufferStart < int64(len(r.buffer)) {
		cached := r.buffer[offset-r.bufferStart:]
		if _, err := ebmlParse(cached, 0); err == nil {
			data = cached
		}
	}
	if data == nil {
		var err error
		data, err = r.read(offset, min(16, r.file.Length-offset))
		if err != nil {
			return ebmlElement{}, err
		}
	}
	item, err := ebmlParse(data, 0)
	if err != nil {
		return item, err
	}
	item.start = offset
	item.data += offset
	if item.end < 0 {
		item.end = r.file.Length
	} else {
		item.end += offset
	}
	if item.end < item.data || item.end > r.file.Length {
		return item, errMatroskaIndex
	}
	return item, nil
}

func loadMatroskaIndex(r *matroskaReader) (*matroskaIndex, error) {
	header, err := r.header(0)
	if err != nil {
		return nil, err
	}
	if header.id != 0x1a45dfa3 || header.unknown || header.end > 4096 {
		return nil, errMatroskaIndex
	}
	segment, err := r.header(header.end)
	if err != nil {
		return nil, err
	}
	if segment.id != 0x18538067 {
		return nil, errMatroskaIndex
	}
	positions := map[int64]int64{}
	for offset, i := segment.data, 0; i < 32; i++ {
		item, err := r.header(offset)
		if err != nil {
			return nil, err
		}
		if item.end <= item.data || item.end > segment.end || item.unknown && item.id != 0x1f43b675 {
			return nil, errMatroskaIndex
		}
		positions[item.id] = offset
		if item.id == 0x114d9b74 {
			if item.end-item.data > 65536 {
				return nil, errMatroskaIndex
			}
			data, err := r.read(item.data, item.end-item.data)
			if err != nil {
				return nil, err
			}
			seeks, err := ebmlChildren(data)
			if err != nil {
				return nil, err
			}
			for _, seek := range seeks {
				if seek.id != 0x4dbb {
					continue
				}
				f, err := ebmlFields(data[seek.data:seek.end])
				if err != nil {
					return nil, err
				}
				id, err := ebmlUint(f[0x53ab])
				if err != nil {
					return nil, err
				}
				pos, err := ebmlUint(f[0x53ac])
				if err != nil {
					return nil, err
				}
				positions[id] = segment.data + pos
			}
		}
		if item.id == 0x1f43b675 {
			break
		}
		offset = item.end
	}
	metadataBytes := int64(0)
	metadata := func(id int64) ([]byte, error) {
		pos, ok := positions[id]
		if !ok {
			return nil, errMatroskaIndex
		}
		item, err := r.header(pos)
		if err != nil {
			return nil, err
		}
		length := item.end - item.data
		metadataBytes += length
		if item.id != id || item.unknown || item.end > segment.end || metadataBytes > 2<<20 {
			return nil, errMatroskaIndex
		}
		return r.read(item.data, length)
	}
	info, err := metadata(0x1549a966)
	if err != nil {
		return nil, err
	}
	fields, err := ebmlFields(info)
	if err != nil {
		return nil, err
	}
	scale := int64(1000000)
	if data, ok := fields[0x2ad7b1]; ok {
		scale, err = ebmlUint(data)
		if err != nil {
			return nil, err
		}
	}
	if scale <= 0 || scale > 1000000000 {
		return nil, errMatroskaIndex
	}
	entries, err := metadata(0x1654ae6b)
	if err != nil {
		return nil, err
	}
	items, err := ebmlChildren(entries)
	if err != nil {
		return nil, err
	}
	index := &matroskaIndex{size: r.file.Length, segment: segment.data, info: ebmlWrap(0x1549a966, info), scale: float64(scale) / 1e9, busy: make(chan struct{}, 1)}
	formats := map[string]string{"S_TEXT/UTF8": "vtt", "S_TEXT/ASCII": "vtt", "S_TEXT/WEBVTT": "vtt", "S_TEXT/ASS": "ass", "S_TEXT/SSA": "ass", "S_HDMV/PGS": "pgs", "S_VOBSUB": "vobsub"}
	stream := 0
	numbers := map[int64]bool{}
	for _, item := range items {
		if item.id != 0xae {
			continue
		}
		if stream >= 64 {
			return nil, errMatroskaIndex
		}
		current := stream
		stream++
		f, err := ebmlFields(entries[item.data:item.end])
		if err != nil {
			return nil, err
		}
		kind, err := ebmlUint(f[0x83])
		if err != nil {
			return nil, err
		}
		num, err := ebmlUint(f[0xd7])
		if err != nil || num <= 0 || numbers[num] {
			return nil, errMatroskaIndex
		}
		numbers[num] = true
		format := formats[string(f[0x86])]
		if kind != 17 || format == "" {
			continue
		}
		// Cue timestamps use Segment ticks. Unusual per-track timing needs the
		// normal demuxer rather than assuming that it matches the block clock.
		if raw, ok := f[0x23314f]; ok {
			value := 0.0
			if len(raw) == 4 {
				value = float64(math.Float32frombits(binary.BigEndian.Uint32(raw)))
			} else if len(raw) == 8 {
				value = math.Float64frombits(binary.BigEndian.Uint64(raw))
			}
			if value != 1 {
				return nil, errMatroskaIndex
			}
		}
		if raw, ok := f[0x56aa]; ok {
			if delay, err := ebmlUint(raw); err != nil || delay != 0 {
				return nil, errMatroskaIndex
			}
		}
		index.tracks = append(index.tracks, indexedTrack{number: num, stream: current, format: format, entry: append([]byte(nil), entries[item.start:item.end]...), valid: true})
	}
	cues, err := metadata(0x1c53bb6b)
	if err != nil {
		return nil, err
	}
	points, err := ebmlChildren(cues)
	if err != nil {
		return nil, err
	}
	for _, point := range points {
		if point.id != 0xbb {
			continue
		}
		data := cues[point.data:point.end]
		children, err := ebmlChildren(data)
		if err != nil {
			return nil, err
		}
		time := int64(-1)
		for _, child := range children {
			if child.id == 0xb3 {
				if time >= 0 {
					return nil, errMatroskaIndex
				}
				time, err = ebmlUint(data[child.data:child.end])
				if err != nil {
					return nil, err
				}
			}
		}
		if time < 0 {
			return nil, errMatroskaIndex
		}
		for _, pos := range children {
			if pos.id != 0xb7 {
				continue
			}
			f, err := ebmlFields(data[pos.data:pos.end])
			if err != nil {
				return nil, err
			}
			num, err := ebmlUint(f[0xf7])
			if err != nil {
				return nil, err
			}
			for i := range index.tracks {
				track := &index.tracks[i]
				if track.number != num {
					continue
				}
				relative, duration := int64(-1), int64(-1)
				var er, ed error
				if raw, ok := f[0xf0]; ok {
					relative, er = ebmlUint(raw)
				}
				if raw, ok := f[0xb2]; ok {
					duration, ed = ebmlUint(raw)
					if duration == 0 {
						ed = errMatroskaIndex
					}
				}
				cluster, ec := ebmlUint(f[0xf1])
				state := int64(0)
				var es error
				if raw, ok := f[0xea]; ok {
					state, es = ebmlUint(raw)
				}
				if er != nil || ed != nil || ec != nil || es != nil || state != 0 {
					track.valid = false
					continue
				}
				track.cues = append(track.cues, subtitleCue{time: time, duration: duration, cluster: cluster, relative: relative})
			}
		}
	}
	valid := false
	for _, track := range index.tracks {
		valid = valid || track.valid && len(track.cues) > 0
	}
	if !valid {
		return nil, errMatroskaIndex
	}
	index.header, err = r.read(0, header.end)
	if err != nil {
		return nil, err
	}
	return index, nil
}

func (index *matroskaIndex) memory() int {
	n := 256 + cap(index.header) + cap(index.info) + cap(index.tracks)*128
	for _, t := range index.tracks {
		n += cap(t.entry) + cap(t.cues)*32
	}
	return n
}

func (index *matroskaIndex) window(r *matroskaReader, stream int, format string, start, end float64) ([]byte, error) {
	select {
	case index.busy <- struct{}{}:
		defer func() { <-index.busy }()
	case <-r.ctx.Done():
		return nil, r.ctx.Err()
	}
	if !finite(start) || !finite(end) || end <= start || index.size != r.file.Length {
		return nil, errMatroskaIndex
	}
	var track *indexedTrack
	for i := range index.tracks {
		if index.tracks[i].stream == stream {
			track = &index.tracks[i]
			break
		}
	}
	if track == nil || track.format != format || !track.valid || len(track.cues) == 0 {
		return nil, errMatroskaIndex
	}
	for _, cue := range track.cues {
		length := float64(cue.duration) * index.scale
		if cue.duration < 0 {
			length = 120
		}
		if (cue.relative < 0 || cue.duration < 0) && float64(cue.time)*index.scale < end && float64(cue.time)*index.scale+length > start {
			return index.clusteredWindow(r, track, start, end, true)
		}
	}
	return index.clusteredWindow(r, track, start, end, false)
}

func (index *matroskaIndex) indexedClusters(r *matroskaReader, track *indexedTrack, start, end float64) ([]byte, error) {
	var clusters []byte
	count := 0
	type clusterInfo struct {
		header ebmlElement
		time   int64
	}
	times := map[int64]clusterInfo{}
	for _, cue := range track.cues {
		if float64(cue.time+cue.duration)*index.scale <= start || float64(cue.time)*index.scale >= end {
			continue
		}
		count++
		// A 300s window can exceed Home's 100 cues. The unchanged 512 range
		// request budget remains the tighter bound for ordinary individual blocks.
		if count > 256 || float64(cue.duration)*index.scale > 120 {
			return nil, errMatroskaIndex
		}
		cluster, ok := times[cue.cluster]
		if !ok {
			item, err := r.header(index.segment + cue.cluster)
			if err != nil {
				return nil, err
			}
			if item.id != 0x1f43b675 {
				return nil, errMatroskaIndex
			}
			timestamp := int64(-1)
			for offset, i := item.data, 0; i < 8; i++ {
				field, err := r.header(offset)
				if err != nil {
					return nil, err
				}
				if field.end > item.end || field.end-field.data > 65536 {
					break
				}
				if field.id == 0xe7 {
					data, err := r.read(field.data, field.end-field.data)
					if err != nil {
						return nil, err
					}
					timestamp, err = ebmlUint(data)
					if err != nil {
						return nil, err
					}
					break
				}
				offset = field.end
			}
			if timestamp < 0 {
				return nil, errMatroskaIndex
			}
			cluster = clusterInfo{item, timestamp}
			times[cue.cluster] = cluster
		}
		item, err := r.header(cluster.header.data + cue.relative)
		if err != nil {
			return nil, err
		}
		if (item.id != 0xa0 && item.id != 0xa3) || item.unknown || item.end > cluster.header.end || item.end-item.data > 65536 {
			return nil, errMatroskaIndex
		}
		body, err := r.read(item.data, item.end-item.data)
		if err != nil {
			return nil, err
		}
		block := body
		if item.id == 0xa0 {
			f, err := ebmlFields(body)
			if err != nil {
				return nil, err
			}
			block = f[0xa1]
			if _, changed := f[0xa4]; changed { // CodecState is not in the initial TrackEntry.
				return nil, errMatroskaIndex
			}
			duration, err := ebmlUint(f[0x9b])
			if err != nil || duration != cue.duration {
				return nil, errMatroskaIndex
			}
		}
		number, n, err := ebmlVint(block, 0, false)
		if err != nil || number != track.number || int64(len(block)) < n+3 {
			return nil, errMatroskaIndex
		}
		if cluster.time+int64(int16(binary.BigEndian.Uint16(block[n:n+2]))) != cue.time {
			return nil, errMatroskaIndex
		}
		if block[n+2]&0x06 != 0 { // Subtitle lacing has no independently timed cue here.
			return nil, errMatroskaIndex
		}
		if item.id == 0xa3 {
			body[n+2] &= 0x0e
			body = append(ebmlWrap(0xa1, body), ebmlWrap(0x9b, ebmlNumber(cue.duration))...)
		}
		output := ebmlWrap(0x1f43b675, append(ebmlWrap(0xe7, ebmlNumber(cluster.time)), ebmlWrap(0xa0, body)...))
		if b := r.budget; b != nil {
			b.Lock()
			b.blocks++
			b.output += len(output)
			exceeded := b.blocks > 256 || b.output > textLimit
			b.Unlock()
			if exceeded {
				return nil, errMatroskaIndex
			}
		}
		if len(clusters)+len(output) > textLimit {
			return nil, errMatroskaIndex
		}
		clusters = append(clusters, output...)
	}
	return clusters, nil
}

// Schedule independent clusters together so one missing torrent piece does not
// delay requesting all subsequent cues. Both paths share their original limits;
// only incomplete indexes need the larger header-walk budget and read-ahead.
func (index *matroskaIndex) clusteredWindow(r *matroskaReader, track *indexedTrack, start, end float64, scan bool) ([]byte, error) {
	r.scan = scan
	type clusterJob struct {
		track     indexedTrack
		originals []*subtitleCue
		data      []byte
		done      bool
	}
	var jobs []clusterJob
	clusters := map[int64]int{}
	for i := range track.cues {
		cue := &track.cues[i]
		duration := float64(cue.duration) * index.scale
		if cue.duration < 0 {
			duration = 120
		}
		if float64(cue.time)*index.scale >= end || float64(cue.time)*index.scale+duration <= start {
			continue
		}
		j, ok := clusters[cue.cluster]
		if !ok {
			j = len(jobs)
			clusters[cue.cluster] = j
			copy := *track
			copy.cues = nil
			jobs = append(jobs, clusterJob{track: copy})
		}
		jobs[j].track.cues = append(jobs[j].track.cues, *cue)
		jobs[j].originals = append(jobs[j].originals, cue)
	}
	if len(jobs) == 0 && !scan && track.format == "ass" {
		return nil, errMatroskaIndex
	}
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	budget := &matroskaReadBudget{bytes: r.bytes, requests: r.requests}
	queue := make(chan int, len(jobs))
	for i := range jobs {
		queue <- i
	}
	close(queue)
	var workers sync.WaitGroup
	var once sync.Once
	var failure error
	for range min(4, len(jobs)) {
		workers.Go(func() {
			for i := range queue {
				if ctx.Err() != nil {
					return
				}
				reader := &matroskaReader{ctx: ctx, backend: r.backend, hash: r.hash, file: r.file, scan: scan, budget: budget}
				var data []byte
				var err error
				if scan {
					data, err = index.scannedClusters(reader, &jobs[i].track, start, end)
				} else {
					data, err = index.indexedClusters(reader, &jobs[i].track, start, end)
				}
				if err != nil {
					once.Do(func() { failure = err; cancel() })
					return
				}
				jobs[i].data = data
				jobs[i].done = true
			}
		})
	}
	workers.Wait()
	r.bytes, r.requests = budget.bytes, budget.requests
	remember := func() {
		for _, job := range jobs {
			if !job.done {
				continue
			}
			for i, original := range job.originals {
				*original = job.track.cues[i]
			}
		}
	}
	if failure != nil {
		// A processing timeout must not make retry scan the same verified AV
		// headers again. Completed clusters independently validated every cue;
		// retain only their locations, never partial subtitle bytes. Explicit
		// abort/replacement and structural failures still publish nothing.
		if !errors.Is(r.ctx.Err(), context.Canceled) && !errors.Is(failure, errMatroskaIndex) {
			remember()
		}
		return nil, failure
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	header := append([]byte(nil), index.header...)
	header = append(header, 0x18, 0x53, 0x80, 0x67, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	header = append(header, index.info...)
	header = append(header, ebmlWrap(0x1654ae6b, track.entry)...)
	size := len(header)
	for _, job := range jobs {
		size += len(job.data)
	}
	if size > textLimit {
		return nil, errMatroskaIndex
	}
	output := make([]byte, 0, size)
	output = append(output, header...)
	for _, job := range jobs {
		output = append(output, job.data...)
	}
	// Publish only after every cluster and aggregate output passed validation.
	remember()
	if len(output) == len(header) && track.format != "ass" {
		return []byte{}, nil
	}
	return output, nil
}

func (index *matroskaIndex) scannedClusters(r *matroskaReader, track *indexedTrack, start, end float64) ([]byte, error) {
	seen := map[int64]bool{}
	var output []byte
	count := 0
	updates := map[*subtitleCue]subtitleCue{}
	for _, cue := range track.cues {
		duration := float64(cue.duration) * index.scale
		if cue.duration < 0 {
			duration = 120
		}
		if float64(cue.time)*index.scale >= end || float64(cue.time)*index.scale+duration <= start || seen[cue.cluster] {
			continue
		}
		seen[cue.cluster] = true
		pending := map[int64][]*subtitleCue{}
		remaining := 0
		for i := range track.cues {
			candidate := &track.cues[i]
			length := float64(candidate.duration) * index.scale
			if candidate.duration < 0 {
				length = 120
			}
			if candidate.cluster == cue.cluster && float64(candidate.time)*index.scale < end && float64(candidate.time)*index.scale+length > start {
				pending[candidate.time] = append(pending[candidate.time], candidate)
				remaining++
			}
		}
		// Once a previous complete window learned the exact block positions,
		// subsequent refills can jump straight to those blocks.
		nextKnown := func() int64 {
			next := int64(-1)
			for _, candidates := range pending {
				for _, candidate := range candidates {
					if candidate.relative < 0 {
						return -1
					}
					if next < 0 || candidate.relative < next {
						next = candidate.relative
					}
				}
			}
			return next
		}
		cluster, err := r.header(index.segment + cue.cluster)
		if err != nil {
			return nil, err
		}
		if cluster.id != 0x1f43b675 || cluster.unknown {
			return nil, errMatroskaIndex
		}
		timestamp := int64(-1)
		var blocks []byte
		for offset, elements := cluster.data, 0; offset < cluster.end && remaining > 0; elements++ {
			if elements >= 30000 {
				return nil, errMatroskaIndex
			}
			item, err := r.header(offset)
			if err != nil {
				return nil, err
			}
			if item.unknown || item.end > cluster.end {
				return nil, errMatroskaIndex
			}
			offset = item.end
			if item.id == 0xe7 {
				data, err := r.read(item.data, item.end-item.data)
				if err != nil {
					return nil, err
				}
				timestamp, err = ebmlUint(data)
				if err != nil {
					return nil, err
				}
				if relative := nextKnown(); relative >= 0 {
					offset = cluster.data + relative
				}
				continue
			}
			if item.id != 0xa0 && item.id != 0xa3 {
				continue
			}
			block := item
			if item.id == 0xa0 {
				found := false
				for pos, i := item.data, 0; pos < item.end && i < 32; i++ {
					child, err := r.header(pos)
					if err != nil {
						return nil, err
					}
					if child.unknown || child.end > item.end {
						return nil, errMatroskaIndex
					}
					if child.id == 0xa1 {
						block = child
						found = true
						break
					}
					pos = child.end
				}
				if !found {
					return nil, errMatroskaIndex
				}
			}
			prefix, err := r.read(block.data, min(11, block.end-block.data))
			if err != nil {
				return nil, err
			}
			number, n, err := ebmlVint(prefix, 0, false)
			if err != nil {
				return nil, err
			}
			if number != track.number {
				continue
			}
			if timestamp < 0 || int64(len(prefix)) < n+3 || prefix[n+2]&0x06 != 0 {
				return nil, errMatroskaIndex
			}
			clock := timestamp + int64(int16(binary.BigEndian.Uint16(prefix[n:n+2])))
			if len(pending[clock]) == 0 {
				continue
			}
			if item.id != 0xa0 || item.end-item.data > 65536 {
				return nil, errMatroskaIndex
			}
			body, err := r.read(item.data, item.end-item.data)
			if err != nil {
				return nil, err
			}
			f, err := ebmlFields(body)
			if err != nil {
				return nil, err
			}
			length, err := ebmlUint(f[0x9b])
			if err != nil {
				return nil, err
			}
			if _, changed := f[0xa4]; changed || length <= 0 || float64(length)*index.scale > 120 {
				return nil, errMatroskaIndex
			}
			expected := pending[clock][0]
			if expected.relative >= 0 && expected.relative != item.start-cluster.data || expected.duration >= 0 && expected.duration != length {
				return nil, errMatroskaIndex
			}
			pending[clock] = pending[clock][1:]
			remaining--
			resolved := *expected
			resolved.relative, resolved.duration = item.start-cluster.data, length
			updates[expected] = resolved
			if relative := nextKnown(); relative >= 0 {
				offset = cluster.data + relative
			}
			if float64(clock+length)*index.scale <= start {
				continue
			}
			count++
			if count > 256 {
				return nil, errMatroskaIndex
			}
			if b := r.budget; b != nil {
				b.Lock()
				b.blocks++
				b.output += len(body) + 32 // Includes EBML block/cluster framing.
				exceeded := b.blocks > 256 || b.output > textLimit
				b.Unlock()
				if exceeded {
					return nil, errMatroskaIndex
				}
			}
			blocks = append(blocks, ebmlWrap(0xa0, body)...)
			if len(blocks)+len(output) > textLimit {
				return nil, errMatroskaIndex
			}
		}
		if remaining > 0 {
			return nil, errMatroskaIndex
		}
		if len(blocks) > 0 {
			output = append(output, ebmlWrap(0x1f43b675, append(ebmlWrap(0xe7, ebmlNumber(timestamp)), blocks...))...)
		}
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	for original, resolved := range updates {
		*original = resolved
	}
	return output, nil
}
