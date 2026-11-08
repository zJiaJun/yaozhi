// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package app

import (
	"slices"
	"strings"

	"github.com/zJiaJun/yaozhi/internal/coreaudio"
)

// MicUID is the UID of the yaozhi-mic Yaozhi creates.
const MicUID = "com.github.zjiajun.yaozhi.mic"

// micChoice is what to do about yaozhi-mic, given the devices there are.
type micChoice int

const (
	micNoBlackHole micChoice = iota // nothing to build it from
	micOurs                         // the one Yaozhi made is there
	micTheirs                       // the user made one (Audio MIDI Setup): use it
	micCreate                       // make one
)

// chooseMic looks for yaozhi-mic. A device the user made counts when it has
// the name and holds BlackHole 2ch, so that input methods never list two
// microphones called yaozhi-mic.
func chooseMic(devs []coreaudio.Device) (micChoice, coreaudio.Device) {
	if !slices.ContainsFunc(devs, func(d coreaudio.Device) bool { return d.UID == BlackHoleUID }) {
		return micNoBlackHole, coreaudio.Device{}
	}
	if i := slices.IndexFunc(devs, func(d coreaudio.Device) bool { return d.UID == MicUID }); i >= 0 {
		return micOurs, devs[i]
	}
	if i := slices.IndexFunc(devs, func(d coreaudio.Device) bool {
		return d.Aggregate && strings.EqualFold(strings.TrimSpace(d.Name), MicName) && slices.Contains(d.SubDevices, BlackHoleUID)
	}); i >= 0 {
		return micTheirs, devs[i]
	}
	return micCreate, coreaudio.Device{}
}
