package api

import (
	"strings"
	"testing"
)

func TestValidSegmentCountsBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		seg  string
		want bool
	}{
		{"128 bytes", strings.Repeat("a", 128), true},
		{"129 bytes", strings.Repeat("a", 129), false},
		{"64 two-byte characters", strings.Repeat("é", 64), true},
		{"65 two-byte characters", strings.Repeat("é", 65), false},
		{"empty", "", false},
	} {
		if got := ValidSegment(tc.seg); got != tc.want {
			t.Errorf("%s: ValidSegment = %v, want %v", tc.name, got, tc.want)
		}
	}
}
