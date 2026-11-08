// SPDX-License-Identifier: GPL-3.0-only

package atvv

import "math"

var stepTable = [89]int{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31,
	34, 37, 41, 45, 50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130,
	143, 157, 173, 190, 209, 230, 253, 279, 307, 337, 371, 408, 449,
	494, 544, 598, 658, 724, 796, 876, 963, 1060, 1166, 1282, 1411,
	1552, 1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327, 3660, 4026,
	4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442,
	11487, 12635, 13899, 15289, 16818, 18500, 20350, 22385, 24623,
	27086, 29794, 32767,
}

var indexTable = [8]int{-1, -1, -1, -1, 2, 4, 6, 8}

// Decoder is a standard IMA/DVI 4-bit ADPCM decoder. Each input byte yields
// two samples, high nibble first. The zero value is ready to use.
type Decoder struct {
	predictor int
	stepIndex int
}

// Reset sets the decoder state, clamping both values into range.
func (d *Decoder) Reset(predictor, stepIndex int) {
	d.predictor = clamp(predictor, math.MinInt16, math.MaxInt16)
	d.stepIndex = clamp(stepIndex, 0, len(stepTable)-1)
}

// State returns the current predictor and step index.
func (d *Decoder) State() (predictor, stepIndex int) {
	return d.predictor, d.stepIndex
}

// Decode appends the samples decoded from b to dst and returns the result.
func (d *Decoder) Decode(dst []int16, b []byte) []int16 {
	for _, v := range b {
		dst = append(dst, d.nibble(int(v>>4)), d.nibble(int(v&0x0F)))
	}
	return dst
}

func (d *Decoder) nibble(n int) int16 {
	step := stepTable[d.stepIndex]
	diff := step >> 3
	if n&1 != 0 {
		diff += step >> 2
	}
	if n&2 != 0 {
		diff += step >> 1
	}
	if n&4 != 0 {
		diff += step
	}
	if n&8 != 0 {
		d.predictor -= diff
	} else {
		d.predictor += diff
	}
	d.predictor = clamp(d.predictor, math.MinInt16, math.MaxInt16)
	d.stepIndex = clamp(d.stepIndex+indexTable[n&7], 0, len(stepTable)-1)
	return int16(d.predictor)
}

func clamp(v, lo, hi int) int {
	return min(hi, max(lo, v))
}
