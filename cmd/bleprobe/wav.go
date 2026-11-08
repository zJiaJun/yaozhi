// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"bytes"
	"encoding/binary"
	"os"
)

// encodeWAV returns a 16-bit mono PCM WAV file.
func encodeWAV(samples []int16, rate int) []byte {
	dataLen := 2 * len(samples)
	var b bytes.Buffer
	b.Grow(44 + dataLen)
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + dataLen))
	b.WriteString("WAVEfmt ")
	w(uint32(16))       // fmt chunk size
	w(uint16(1))        // PCM
	w(uint16(1))        // mono
	w(uint32(rate))     // sample rate
	w(uint32(rate * 2)) // byte rate
	w(uint16(2))        // block align
	w(uint16(16))       // bits per sample
	b.WriteString("data")
	w(uint32(dataLen))
	w(samples)
	return b.Bytes()
}

func writeWAV(path string, samples []int16, rate int) error {
	return os.WriteFile(path, encodeWAV(samples, rate), 0o644)
}
