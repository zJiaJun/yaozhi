// SPDX-License-Identifier: GPL-3.0-only

package atvv

import (
	"bytes"
	"testing"
)

func TestGetCapabilities(t *testing.T) {
	want := []byte{0x0A, 0x01, 0x00, 0x00, 0x03, 0x03}
	if got := GetCapabilities(); !bytes.Equal(got, want) {
		t.Fatalf("GetCapabilities() = % X, want % X", got, want)
	}
}

func TestParseCapabilities(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		ok   bool
		want Capabilities
	}{
		{
			name: "v1.0 with 16 kHz",
			in:   []byte{0x0B, 0x01, 0x00, 0x02, 0x03, 0x00, 0x78},
			ok:   true,
			want: Capabilities{Version: 0x0100, Codecs: 0x02, Interaction: 0x03, FrameSize: 120, SelectedCodec: Codec16kHz, SampleRate: 16000},
		},
		{
			name: "v1.0 advertising the legacy codec layout",
			in:   []byte{0x0B, 0x01, 0x00, 0x00, 0x02, 0x00, 0x78, 0x00, 0x00},
			ok:   true,
			want: Capabilities{Version: 0x0100, Codecs: 0x02, Interaction: 0x03, FrameSize: 120, SelectedCodec: Codec16kHz, SampleRate: 16000},
		},
		{
			name: "v1.0 offering only 8 kHz",
			in:   []byte{0x0B, 0x01, 0x00, 0x01, 0x00, 0x00, 0x78},
			ok:   true,
			want: Capabilities{Version: 0x0100, Codecs: 0x01, FrameSize: 120, SelectedCodec: Codec8kHz, SampleRate: 8000},
		},
		{
			name: "zero frame size falls back to the default",
			in:   []byte{0x0B, 0x01, 0x00, 0x02, 0x03, 0x00, 0x00},
			ok:   true,
			want: Capabilities{Version: 0x0100, Codecs: 0x02, Interaction: 0x03, FrameSize: DefaultFrameSize, SelectedCodec: Codec16kHz, SampleRate: 16000},
		},
		{
			name: "pre-1.0 layout",
			in:   []byte{0x0B, 0x00, 0x04, 0x00, 0x03, 0x00, 0x86, 0x00, 0x00},
			ok:   true,
			want: Capabilities{Version: 0x0004, Codecs: 0x03, FrameSize: 134, SelectedCodec: Codec16kHz, SampleRate: 16000},
		},
		{name: "pre-1.0 layout too short", in: []byte{0x0B, 0x00, 0x04, 0x00, 0x03, 0x00, 0x86}},
		{name: "empty", in: nil},
		{name: "truncated", in: []byte{0x0B, 0x01}},
		{name: "wrong opcode", in: []byte{0x00, 0x01, 0x00, 0x02, 0x03, 0x00, 0x78}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseCapabilities(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMicrophoneCommandsDependOnVersion(t *testing.T) {
	tests := []struct {
		name string
		got  []byte
		want []byte
	}{
		{"open v1.0", MicOpen(0x0100, Codec16kHz), []byte{0x0C, 0x00}},
		{"open pre-1.0 carries codec", MicOpen(0x0001, Codec16kHz), []byte{0x0C, 0x00, 0x02}},
		{"close v1.0 carries session", MicClose(0x0100, 7), []byte{0x0D, 0x07}},
		{"close pre-1.0", MicClose(0x0001, 7), []byte{0x0D}},
	}
	for _, tt := range tests {
		if !bytes.Equal(tt.got, tt.want) {
			t.Errorf("%s: got % X, want % X", tt.name, tt.got, tt.want)
		}
	}
}

func TestSupportsSampleRateOnlyAccepts16kHz(t *testing.T) {
	if !SupportsSampleRate(16000) {
		t.Error("16 kHz rejected")
	}
	if SupportsSampleRate(8000) {
		t.Error("8 kHz accepted")
	}
}
