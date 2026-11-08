// SPDX-License-Identifier: GPL-3.0-only

package atvv

import (
	"bytes"
	"math"
	"slices"
	"testing"
)

func TestPostprocess(t *testing.T) {
	tests := []struct {
		name   string
		in     []int16
		gainDB float64
		want   []int16
	}{
		{"smooths interior samples", []int16{0, 1000, 0}, 0, []int16{0, 500, 0}},
		{"smoothing floors negatives like an arithmetic shift", []int16{-1, -1, 0}, 0, []int16{-1, -1, 0}},
		{"clips amplified samples", []int16{20_000}, 24, []int16{math.MaxInt16}},
		{"attenuates and rounds", []int16{10_000}, -6, []int16{5012}},
		{"clamps gain to +24 dB", []int16{1000}, 100, []int16{15849}},
		{"treats +Inf as 0 dB", []int16{20_000}, math.Inf(1), []int16{20_000}},
		{"treats NaN as 0 dB", []int16{20_000}, math.NaN(), []int16{20_000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Postprocess(tt.in, tt.gainDB); !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPostprocessEmptyAndDoesNotMutateInput(t *testing.T) {
	if got := Postprocess(nil, 0); len(got) != 0 {
		t.Fatalf("empty input produced %v", got)
	}
	in := []int16{0, 1000, 0}
	Postprocess(in, 12)
	if !slices.Equal(in, []int16{0, 1000, 0}) {
		t.Fatalf("input mutated to %v", in)
	}
}

func TestFrameAccumulatorKeepsPartialData(t *testing.T) {
	var a FrameAccumulator
	if frames := a.Append([]byte{1, 2}, 3); len(frames) != 0 {
		t.Fatalf("unexpected frames %v", frames)
	}
	if !bytes.Equal(a.Pending(), []byte{1, 2}) {
		t.Fatalf("pending = %v", a.Pending())
	}

	frames := a.Append([]byte{3, 4, 5, 6, 7}, 3)
	want := [][]byte{{1, 2, 3}, {4, 5, 6}}
	if !slices.EqualFunc(frames, want, bytes.Equal) {
		t.Fatalf("frames = %v, want %v", frames, want)
	}
	if !bytes.Equal(a.Pending(), []byte{7}) {
		t.Fatalf("pending = %v, want [7]", a.Pending())
	}

	// Returned frames must not alias the internal buffer.
	frames[0][0] = 99
	if next := a.Append([]byte{8, 9}, 3); !bytes.Equal(next[0], []byte{7, 8, 9}) {
		t.Fatalf("next frame = %v", next[0])
	}
}

func TestFrameAccumulatorResetAndZeroFrameSize(t *testing.T) {
	var a FrameAccumulator
	if frames := a.Append([]byte{1, 2, 3}, 0); frames != nil {
		t.Fatalf("zero frame size produced %v", frames)
	}
	a.Append([]byte{1, 2}, 3)
	a.Reset()
	if len(a.Pending()) != 0 {
		t.Fatalf("pending after reset = %v", a.Pending())
	}
}
