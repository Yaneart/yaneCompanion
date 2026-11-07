package media

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// Read only the box fields needed for measured fragment timestamps. Counts and
// lengths are checked before indexing; untrusted media never supplies paths.
type box struct {
	kind string
	data []byte
}

func boxes(data []byte) ([]box, error) {
	result := []box{}
	for len(data) > 0 {
		if len(data) < 8 {
			return nil, errPlaylist
		}
		n := uint64(binary.BigEndian.Uint32(data))
		header := 8
		if n == 1 {
			if len(data) < 16 {
				return nil, errPlaylist
			}
			n = binary.BigEndian.Uint64(data[8:])
			header = 16
		}
		if n < uint64(header) || n > uint64(len(data)) || len(result) > 1024 {
			return nil, errPlaylist
		}
		result = append(result, box{string(data[4:8]), data[header:int(n)]})
		data = data[int(n):]
	}
	return result, nil
}
func readBox(r io.Reader) ([]byte, string, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, "", err
	}
	n := int64(binary.BigEndian.Uint32(header))
	limit := int64(2 << 20)
	if string(header[4:8]) == "mdat" {
		limit = segmentLimit
	}
	if n < 8 || n > limit {
		return nil, "", errPlaylist
	}
	data := make([]byte, int(n))
	copy(data, header)
	_, err := io.ReadFull(r, data[8:])
	return data, string(header[4:8]), err
}
func child(data []byte, kind string) ([]byte, error) {
	list, err := boxes(data)
	if err != nil {
		return nil, err
	}
	for _, b := range list {
		if b.kind == kind {
			return b.data, nil
		}
	}
	return nil, errPlaylist
}
func u32(data []byte, offset int) (uint32, error) {
	if offset < 0 || offset > len(data)-4 {
		return 0, errPlaylist
	}
	return binary.BigEndian.Uint32(data[offset:]), nil
}

type mp4Clock struct {
	video      uint32
	timescales map[uint32]uint32
	defaults   map[uint32]uint32
}

func initClock(data []byte) (mp4Clock, error) {
	c := mp4Clock{timescales: map[uint32]uint32{}, defaults: map[uint32]uint32{}}
	moov, err := child(data, "moov")
	if err != nil {
		return c, err
	}
	list, err := boxes(moov)
	if err != nil {
		return c, err
	}
	for _, b := range list {
		if b.kind == "mvex" {
			entries, e := boxes(b.data)
			if e != nil {
				return c, e
			}
			for _, e := range entries {
				if e.kind == "trex" {
					id, x := u32(e.data, 4)
					duration, y := u32(e.data, 12)
					if x != nil || y != nil {
						return c, errPlaylist
					}
					c.defaults[id] = duration
				}
			}
			continue
		}
		if b.kind != "trak" {
			continue
		}
		tkhd, e := child(b.data, "tkhd")
		if e != nil || len(tkhd) < 4 {
			return c, errPlaylist
		}
		offset := 12
		if tkhd[0] == 1 {
			offset = 20
		}
		id, e := u32(tkhd, offset)
		if e != nil {
			return c, e
		}
		mdia, e := child(b.data, "mdia")
		if e != nil {
			return c, e
		}
		mdhd, e := child(mdia, "mdhd")
		if e != nil || len(mdhd) < 4 {
			return c, errPlaylist
		}
		offset = 12
		if mdhd[0] == 1 {
			offset = 20
		}
		scale, e := u32(mdhd, offset)
		if e != nil || scale == 0 {
			return c, errPlaylist
		}
		c.timescales[id] = scale
		hdlr, e := child(mdia, "hdlr")
		if e != nil || len(hdlr) < 12 {
			return c, errPlaylist
		}
		if string(hdlr[8:12]) == "vide" {
			c.video = id
		}
	}
	if c.video == 0 {
		return c, errPlaylist
	}
	return c, nil
}

type fragmentTime struct{ start, end, pts, minDecode, minPTS, allEnd float64 }

var errAudioOnly = errors.New("AUDIO_ONLY_FRAGMENT")

func (c mp4Clock) fragment(data []byte) (fragmentTime, error) {
	result := fragmentTime{minDecode: math.Inf(1), minPTS: math.Inf(1)}
	moof, err := child(data, "moof")
	if err != nil {
		return result, err
	}
	list, err := boxes(moof)
	if err != nil {
		return result, err
	}
	found := false
	for _, b := range list {
		if b.kind != "traf" {
			continue
		}
		h, err := child(b.data, "tfhd")
		if err != nil {
			return result, err
		}
		flags, err := u32(h, 0)
		if err != nil {
			return result, err
		}
		flags &= 0xffffff
		id, err := u32(h, 4)
		if err != nil || c.timescales[id] == 0 {
			return result, errPlaylist
		}
		offset := 8
		if flags&1 != 0 {
			offset += 8
		}
		if flags&2 != 0 {
			offset += 4
		}
		defaultDuration := c.defaults[id]
		if flags&8 != 0 {
			defaultDuration, err = u32(h, offset)
			if err != nil {
				return result, err
			}
			offset += 4
		}
		t, err := child(b.data, "tfdt")
		if err != nil || len(t) < 8 {
			return result, errPlaylist
		}
		decode := uint64(binary.BigEndian.Uint32(t[4:]))
		if t[0] == 1 {
			if len(t) < 12 {
				return result, errPlaylist
			}
			decode = binary.BigEndian.Uint64(t[4:])
		}
		scale := float64(c.timescales[id])
		start := float64(decode) / scale
		result.minDecode = math.Min(result.minDecode, start)
		track := fragmentTime{start: start, pts: math.Inf(1)}
		entries, err := boxes(b.data)
		if err != nil {
			return result, err
		}
		for _, e := range entries {
			if e.kind != "trun" {
				continue
			}
			f, err := u32(e.data, 0)
			if err != nil {
				return result, err
			}
			f &= 0xffffff
			count, err := u32(e.data, 4)
			if err != nil || count > 100000 {
				return result, errPlaylist
			}
			off := 8
			if f&1 != 0 {
				off += 4
			}
			if f&4 != 0 {
				off += 4
			}
			for i := uint32(0); i < count; i++ {
				duration := defaultDuration
				if f&0x100 != 0 {
					duration, err = u32(e.data, off)
					if err != nil {
						return result, err
					}
					off += 4
				}
				if duration == 0 {
					return result, errPlaylist
				}
				if f&0x200 != 0 {
					off += 4
				}
				if f&0x400 != 0 {
					off += 4
				}
				composition := int64(0)
				if f&0x800 != 0 {
					v, e2 := u32(e.data, off)
					if e2 != nil {
						return result, e2
					}
					composition = int64(v)
					if e.data[0] == 1 {
						composition = int64(int32(v))
					}
					off += 4
				}
				if off > len(e.data) {
					return result, errPlaylist
				}
				presentation := (float64(decode) + float64(composition)) / scale
				track.pts = math.Min(track.pts, presentation)
				track.allEnd = math.Max(track.allEnd, presentation+float64(duration)/scale)
				decode += uint64(duration)
			}
		}
		track.end = float64(decode) / scale
		if !finite(track.pts) || track.end <= track.start || track.end-track.start > 45 {
			return result, ErrUnsupported
		}
		result.allEnd = math.Max(result.allEnd, math.Max(track.end, track.allEnd))
		result.minPTS = math.Min(result.minPTS, track.pts)
		if id == c.video {
			found = true
			result.start, result.end, result.pts = track.start, track.end, track.pts
		}
	}
	if !found {
		if finite(result.minDecode) && result.allEnd > result.minDecode {
			return result, errAudioOnly
		}
		return result, ErrUnsupported
	}
	return result, nil
}
