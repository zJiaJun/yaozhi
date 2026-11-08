// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"encoding/binary"
	"testing"
)

func TestEncodeWAV(t *testing.T) {
	b := encodeWAV([]int16{1, -2, 0x7fff}, 16000)
	if len(b) != 44+6 {
		t.Fatalf("len = %d, want 50", len(b))
	}
	le := binary.LittleEndian
	checks := []struct {
		name      string
		got, want uint32
	}{
		{"RIFF size", le.Uint32(b[4:]), 36 + 6},
		{"channels", uint32(le.Uint16(b[22:])), 1},
		{"rate", le.Uint32(b[24:]), 16000},
		{"byte rate", le.Uint32(b[28:]), 32000},
		{"bits", uint32(le.Uint16(b[34:])), 16},
		{"data size", le.Uint32(b[40:]), 6},
		{"sample 1", uint32(le.Uint16(b[46:])), 0xfffe},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if string(b[0:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || string(b[36:40]) != "data" {
		t.Errorf("bad chunk ids: %q", b[:40])
	}
}

func TestIsRC003Name(t *testing.T) {
	for name, want := range map[string]bool{
		"MI RC":                         true,
		" mi rc\n":                      true,
		"Xiaomi Bluetooth Remote 2 Pro": true,
		"小米蓝牙语音遥控器":                     true,
		"MI RC 2":                       false,
		"":                              false,
		"Chromecast Voice Remote":       false,
	} {
		if got := isRC003Name(name); got != want {
			t.Errorf("isRC003Name(%q) = %v, want %v", name, got, want)
		}
	}
}
