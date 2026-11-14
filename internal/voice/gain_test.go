// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"slices"
	"testing"
)

func TestGain(t *testing.T) {
	pcm := []int16{0, 100, -100, 20000, -20000}
	Gain(pcm, 0)
	if want := []int16{0, 100, -100, 20000, -20000}; !slices.Equal(pcm, want) {
		t.Fatalf("0 dB changed the audio: %v", pcm)
	}
	Gain(pcm, 6.0206) // twice as loud
	if want := []int16{0, 200, -200, 32767, -32768}; !slices.Equal(pcm, want) {
		t.Fatalf("+6 dB: %v, want %v", pcm, want)
	}
}
