// SPDX-License-Identifier: GPL-3.0-only

package macinput

import (
	"slices"
	"testing"
)

func TestWithMapping(t *testing.T) {
	other := KeyMapping{0x7_0000_0066, 0x7_0000_006F} // power -> F20
	cur := []KeyMapping{other, {UsageF5, 0xFF_0000_0003}}

	got := withMapping(cur, UsageF5, UsageNone)
	want := []KeyMapping{other, {UsageF5, UsageNone}}
	if !slices.Equal(got, want) {
		t.Errorf("withMapping = %v, want %v", got, want)
	}
	if !slices.Equal(cur, []KeyMapping{other, {UsageF5, 0xFF_0000_0003}}) {
		t.Errorf("withMapping changed its input: %v", cur)
	}

	if got := withoutMapping(want, UsageF5); !slices.Equal(got, []KeyMapping{other}) {
		t.Errorf("withoutMapping = %v", got)
	}
	if m, ok := lookup(cur, UsageF5); !ok || m.Dst != 0xFF_0000_0003 {
		t.Errorf("lookup = %v, %v", m, ok)
	}
	if _, ok := lookup([]KeyMapping{other}, UsageF5); ok {
		t.Error("lookup found a missing key")
	}
}
