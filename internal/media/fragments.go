package media

import "errors"

const cacheTarget = 48 << 20
const cacheLimit = 128 << 20
const segmentLimit = 64 << 20

var errPlaylist = errors.New("INVALID_MEDIA")

type segment struct {
	Name                 string
	Duration, Start, End float64
	Sequence             int
}
