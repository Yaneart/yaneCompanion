package media

import (
	"math"
	"testing"
)

func TestByteAwareWindow(t *testing.T) {
	for _, tc := range []struct{ bitrate, minimum, want float64 }{{0, 8, 45}, {5e6, 8, 45}, {40e6, 8, 8}, {100e6, 18, 18}, {100e6, 150, 45}, {math.NaN(), 8, 45}} {
		if got := WindowSeconds(tc.bitrate, tc.minimum); got != tc.want {
			t.Fatalf("%+v: %v", tc, got)
		}
	}
}
func TestMeasuredTimeline(t *testing.T) {
	timeline := Timeline{89.6, 2, 7200}
	if actual, err := timeline.Absolute(2.4); err != nil || math.Abs(actual-90) > 1e-6 {
		t.Fatalf("measured anchor lost: %v %v", actual, err)
	}
	if _, err := timeline.Absolute(math.NaN()); err == nil {
		t.Fatal("NaN accepted")
	}
	if _, err := (Timeline{0, 0, 0}).Absolute(1); err == nil {
		t.Fatal("unknown duration accepted")
	}
}
