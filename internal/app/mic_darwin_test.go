// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package app

import (
	"testing"

	"github.com/zJiaJun/yaozhi/internal/coreaudio"
)

func TestChooseMic(t *testing.T) {
	bh := coreaudio.Device{UID: BlackHoleUID, Name: "BlackHole 2ch"}
	builtIn := coreaudio.Device{UID: "BuiltInMicrophoneDevice", Name: "MacBook Pro 麦克风"}
	ours := coreaudio.Device{UID: MicUID, Name: MicName, Aggregate: true, SubDevices: []string{BlackHoleUID}}
	// As Audio MIDI Setup names it (seen on the development Mac).
	theirs := coreaudio.Device{UID: "~:AMS2_Aggregate:0", Name: "yaozhi-mic", Aggregate: true, SubDevices: []string{BlackHoleUID}}
	otherAgg := coreaudio.Device{UID: "~:AMS2_Aggregate:1", Name: "yaozhi-mic", Aggregate: true, SubDevices: []string{"BuiltInMicrophoneDevice"}}
	notAgg := coreaudio.Device{UID: "x", Name: "yaozhi-mic"}

	cases := []struct {
		name string
		devs []coreaudio.Device
		want micChoice
		uid  string
	}{
		{"no BlackHole", []coreaudio.Device{builtIn, theirs}, micNoBlackHole, ""},
		{"nothing yet", []coreaudio.Device{bh, builtIn}, micCreate, ""},
		{"ours", []coreaudio.Device{bh, ours}, micOurs, MicUID},
		{"ours wins over theirs", []coreaudio.Device{bh, theirs, ours}, micOurs, MicUID},
		{"theirs", []coreaudio.Device{bh, theirs}, micTheirs, "~:AMS2_Aggregate:0"},
		{"same name, other contents", []coreaudio.Device{bh, otherAgg}, micCreate, ""},
		{"same name, not an aggregate", []coreaudio.Device{bh, notAgg}, micCreate, ""},
	}
	for _, tc := range cases {
		got, d := chooseMic(tc.devs)
		if got != tc.want || d.UID != tc.uid {
			t.Errorf("%s: got %v %q, want %v %q", tc.name, got, d.UID, tc.want, tc.uid)
		}
	}
}
