package torr

import (
	"strconv"
	"strings"
)

// Private media workers use exact single ranges. Keep the engine's piece
// priorities and its buffered reader within that range, instead of treating
// a subtitle header as the start of an unbounded movie stream.
func companionRange(value string, size int64) []int64 {
	if !strings.HasPrefix(value, "bytes=") {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 {
		return nil
	}
	start, a := strconv.ParseInt(parts[0], 10, 64)
	end, b := strconv.ParseInt(parts[1], 10, 64)
	if a != nil || b != nil || start < 0 || end < start || end >= size {
		return nil
	}
	return []int64{start, end + 1}
}
