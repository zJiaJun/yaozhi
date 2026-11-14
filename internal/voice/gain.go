// SPDX-License-Identifier: GPL-3.0-only

package voice

import "math"

// MaxGainDB is the most Gain is meant to add: 20 dB is ten times louder,
// beyond which a normal voice only clips.
const MaxGainDB = 20

// Gain makes pcm louder by db decibels, in place. Samples that would pass
// full scale stay at it, so a gain too high clips instead of wrapping
// around. 0 dB leaves the audio as it is.
func Gain(pcm []int16, db float64) {
	if db == 0 {
		return
	}
	k := math.Pow(10, db/20)
	for i, v := range pcm {
		pcm[i] = int16(math.Max(math.MinInt16, math.Min(math.MaxInt16, math.Round(float64(v)*k))))
	}
}
