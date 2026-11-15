package torr

import (
	"reflect"
	"testing"
)

func TestCompanionExactRange(t *testing.T) {
	for _, tc := range []struct {
		value string
		size  int64
		want  []int64
	}{
		{"bytes=4-7", 16, []int64{4, 8}}, {"bytes=0-0", 1, []int64{0, 1}},
		{"bytes=15-15", 16, []int64{15, 16}}, {"bytes=0-9223372036854775806", 9223372036854775807, []int64{0, 9223372036854775807}},
		{"bytes=4-16", 16, nil}, {"bytes=8-4", 16, nil}, {"bytes=-1-4", 16, nil},
		{"bytes=4-", 16, nil}, {"bytes=-4", 16, nil}, {"bytes=0-3,4-7", 16, nil},
		{"bytes=0-9223372036854775807", 16, nil}, {"", 16, nil},
	} {
		if got := companionRange(tc.value, tc.size); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: got %v want %v", tc.value, got, tc.want)
		}
	}
}
