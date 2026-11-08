// SPDX-License-Identifier: GPL-3.0-only

package coreaudio

import (
	"slices"
	"testing"
)

func TestDuplicateToStereo(t *testing.T) {
	dst := make([]int16, 6)
	if n := duplicateToStereo(dst, []int16{1, -2, 3, 4}); n != 3 {
		t.Fatalf("n = %d, want 3 (dst holds 3 frames)", n)
	}
	if want := []int16{1, 1, -2, -2, 3, 3}; !slices.Equal(dst, want) {
		t.Errorf("dst = %v, want %v", dst, want)
	}
	if n := duplicateToStereo(dst, nil); n != 0 {
		t.Errorf("empty src: n = %d", n)
	}
}

func TestFourCC(t *testing.T) {
	if got := fourCC("aqcd"); got != propCurrentDevice {
		t.Errorf("fourCC(aqcd) = %#x, want %#x", got, propCurrentDevice)
	}
	if got := fourCCString(fourCC("dev#")); got != "dev#" {
		t.Errorf("round trip = %q", got)
	}
}
