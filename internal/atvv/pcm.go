// SPDX-License-Identifier: GPL-3.0-only

package atvv

import "math"

// MaxGainDB bounds the post-processing gain in both directions.
const MaxGainDB = 24.0

// Postprocess applies a 3-tap [1 2 1]/4 smoothing filter to the interior
// samples, then scales by gainDB (clamped to ±MaxGainDB; NaN and ±Inf count
// as 0 dB) and clips to int16. It returns a new slice.
func Postprocess(in []int16, gainDB float64) []int16 {
	if len(in) == 0 {
		return nil
	}
	out := make([]int16, len(in))
	copy(out, in)
	for i := 1; i < len(in)-1; i++ {
		out[i] = int16((int(in[i-1]) + 2*int(in[i]) + int(in[i+1])) >> 2)
	}

	if math.IsNaN(gainDB) || math.IsInf(gainDB, 0) {
		gainDB = 0
	}
	gainDB = math.Min(MaxGainDB, math.Max(-MaxGainDB, gainDB))
	if gainDB == 0 {
		return out
	}
	gain := math.Pow(10, gainDB/20)
	for i, v := range out {
		scaled := math.Round(float64(v) * gain)
		out[i] = int16(math.Min(math.MaxInt16, math.Max(math.MinInt16, scaled)))
	}
	return out
}

// FrameAccumulator re-chunks arbitrarily fragmented notification payloads
// into fixed-size ADPCM frames. The zero value is ready to use.
type FrameAccumulator struct {
	pending []byte
}

// Append adds b and returns every complete frame of frameSize bytes. Bytes
// that do not yet fill a frame stay pending. Returned frames do not alias the
// accumulator's buffer.
func (a *FrameAccumulator) Append(b []byte, frameSize int) [][]byte {
	if frameSize <= 0 {
		return nil
	}
	a.pending = append(a.pending, b...)
	var frames [][]byte
	for len(a.pending) >= frameSize {
		frame := make([]byte, frameSize)
		copy(frame, a.pending[:frameSize])
		frames = append(frames, frame)
		a.pending = a.pending[frameSize:]
	}
	if len(a.pending) == 0 {
		a.pending = a.pending[:0:0]
	}
	return frames
}

// Pending returns a copy of the bytes waiting for a complete frame.
func (a *FrameAccumulator) Pending() []byte {
	return append([]byte(nil), a.pending...)
}

// Reset drops any pending bytes.
func (a *FrameAccumulator) Reset() {
	a.pending = nil
}
