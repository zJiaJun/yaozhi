// SPDX-License-Identifier: GPL-3.0-only

package atvv

import (
	"math"
	"slices"
	"testing"
)

func TestDecoderHighNibbleFirst(t *testing.T) {
	var d Decoder
	if got := d.Decode(nil, []byte{0x11}); !slices.Equal(got, []int16{1, 2}) {
		t.Fatalf("0x11 decoded to %v, want [1 2]", got)
	}
	d.Reset(0, 0)
	if got := d.Decode(nil, []byte{0x7F}); !slices.Equal(got, []int16{11, -19}) {
		t.Fatalf("0x7F decoded to %v, want [11 -19]", got)
	}
}

func TestDecoderAppendsToDst(t *testing.T) {
	var d Decoder
	got := d.Decode([]int16{42}, []byte{0x00, 0x00})
	if len(got) != 5 || got[0] != 42 {
		t.Fatalf("Decode did not append: %v", got)
	}
}

func TestDecoderResetClampsState(t *testing.T) {
	var d Decoder
	d.Reset(100_000, 1_000)
	if p, i := d.State(); p != math.MaxInt16 || i != 88 {
		t.Fatalf("State() = (%d, %d), want (32767, 88)", p, i)
	}
	d.Reset(-100_000, -5)
	if p, i := d.State(); p != math.MinInt16 || i != 0 {
		t.Fatalf("State() = (%d, %d), want (-32768, 0)", p, i)
	}
}

// TestDecoderRoundTrip encodes a tone with a reference IMA encoder and checks
// the decoder reproduces the encoder's reconstruction exactly and tracks the
// original signal closely once the step size has adapted.
func TestDecoderRoundTrip(t *testing.T) {
	const n = 1600 // 100 ms at 16 kHz
	original := make([]int16, n)
	for i := range original {
		original[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	encoded, reconstructed := imaEncode(original)

	var d Decoder
	decoded := d.Decode(nil, encoded)
	if !slices.Equal(decoded, reconstructed) {
		t.Fatal("decoder output differs from the encoder's reconstruction")
	}

	const warmup = 64
	var sum float64
	for i := warmup; i < n; i++ {
		sum += math.Abs(float64(decoded[i]) - float64(original[i]))
	}
	if mean := sum / float64(n-warmup); mean > 300 {
		t.Fatalf("mean absolute error %.1f is too large for a 440 Hz tone", mean)
	}
}

// imaEncode is a reference IMA/DVI encoder used only to build fixtures. It
// returns the packed bytes (high nibble first) and the samples a conforming
// decoder must reconstruct. len(samples) must be even.
func imaEncode(samples []int16) (packed []byte, reconstructed []int16) {
	predictor, index := 0, 0
	nibbles := make([]byte, 0, len(samples))
	for _, s := range samples {
		step := stepTable[index]
		diff := int(s) - predictor
		var code int
		if diff < 0 {
			code = 8
			diff = -diff
		}
		if diff >= step {
			code |= 4
			diff -= step
		}
		if diff >= step>>1 {
			code |= 2
			diff -= step >> 1
		}
		if diff >= step>>2 {
			code |= 1
		}

		delta := step >> 3
		if code&1 != 0 {
			delta += step >> 2
		}
		if code&2 != 0 {
			delta += step >> 1
		}
		if code&4 != 0 {
			delta += step
		}
		if code&8 != 0 {
			predictor -= delta
		} else {
			predictor += delta
		}
		predictor = clamp(predictor, math.MinInt16, math.MaxInt16)
		index = clamp(index+indexTable[code&7], 0, len(stepTable)-1)

		nibbles = append(nibbles, byte(code))
		reconstructed = append(reconstructed, int16(predictor))
	}
	for i := 0; i+1 < len(nibbles); i += 2 {
		packed = append(packed, nibbles[i]<<4|nibbles[i+1])
	}
	return packed, reconstructed
}
