package media

import (
	"errors"
	"math"
)

// Ported from yaneMedia's pure torrent-playback-window policy, with LC-002's
// stricter 45-second ceiling. The minimum never overrides the time ceiling.
func WindowSeconds(bitrate float64, minimum float64) float64 {
	const maxSeconds, targetBytes = 45.0, 48.0 * 1024 * 1024
	if math.IsNaN(minimum) || math.IsInf(minimum, 0) || minimum < 8 {
		minimum = 8
	}
	window := maxSeconds
	if !math.IsNaN(bitrate) && !math.IsInf(bitrate, 0) && bitrate > 0 {
		window = math.Floor(targetBytes * 8 / (bitrate * 1.25))
	}
	return math.Min(maxSeconds, math.Max(minimum, window))
}

type Timeline struct{ AbsoluteAnchorSeconds, MediaAnchorSeconds, DurationSeconds float64 }

func (t Timeline) Absolute(currentTime float64) (float64, error) {
	for _, n := range []float64{currentTime, t.AbsoluteAnchorSeconds, t.MediaAnchorSeconds, t.DurationSeconds} {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return 0, errors.New("INVALID_TIMELINE")
		}
	}
	position := t.AbsoluteAnchorSeconds + currentTime - t.MediaAnchorSeconds
	if t.DurationSeconds <= 0 || position < 0 || position > t.DurationSeconds {
		return 0, errors.New("INVALID_TIMELINE")
	}
	return position, nil
}
