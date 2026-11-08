// SPDX-License-Identifier: GPL-3.0-only

// Package atvv implements the host side of ATVV (Android TV Voice over BLE),
// the voice protocol used by the Xiaomi Bluetooth Remote 2 Pro (RC003) and
// other Android TV voice remotes.
//
// The package is pure Go and transport-agnostic: it consumes the raw bytes of
// the Control and Audio characteristic notifications and produces session
// events plus decoded 16 kHz mono PCM. The BLE transport lives elsewhere.
//
// Behaviour follows the real-device-verified macOS adapter in
// open-voice-bridge (GPL-3.0-only), which in turn references
// xxb26553663-star/remote-bridge-hub.
package atvv

// GATT identifiers of the ATVV voice service.
const (
	ServiceUUID  = "AB5E0001-5A21-4F05-BC7D-AF01F617B664"
	TransmitUUID = "AB5E0002-5A21-4F05-BC7D-AF01F617B664" // host -> remote commands
	AudioUUID    = "AB5E0003-5A21-4F05-BC7D-AF01F617B664" // remote -> host ADPCM stream
	ControlUUID  = "AB5E0004-5A21-4F05-BC7D-AF01F617B664" // remote -> host control events
)

// Control-channel opcodes: the first byte of every Control notification.
const (
	OpAudioStop    byte = 0x00
	OpAudioStart   byte = 0x04
	OpMicButton    byte = 0x08
	OpAudioSync    byte = 0x0A
	OpCapabilities byte = 0x0B
)

// Host -> remote command opcodes written to the Transmit characteristic.
const (
	cmdGetCapabilities byte = 0x0A
	cmdMicOpen         byte = 0x0C
	cmdMicClose        byte = 0x0D
)

// Codec identifiers advertised in the capabilities response.
const (
	Codec8kHz  byte = 0x01
	Codec16kHz byte = 0x02
)

const (
	// DefaultFrameSize is used when the remote advertises a zero frame size.
	DefaultFrameSize = 120
	// SupportedSampleRate is the only rate this bridge accepts. An 8 kHz
	// offer is rejected rather than risking a silent mis-decode.
	SupportedSampleRate = 16000
)

// version1 is the first protocol version whose commands carry the v1.0 layout.
const version1 uint16 = 0x0100

// GetCapabilities returns the v1.0 capability query sent right after the
// Control and Audio notifications are subscribed.
func GetCapabilities() []byte {
	return []byte{cmdGetCapabilities, 0x01, 0x00, 0x00, 0x03, 0x03}
}

// MicOpen returns the command that asks the remote to start streaming audio.
// Pre-1.0 remotes also expect the selected codec.
func MicOpen(version uint16, codec byte) []byte {
	if version >= version1 {
		return []byte{cmdMicOpen, 0x00}
	}
	return []byte{cmdMicOpen, 0x00, codec}
}

// MicClose returns the command that closes the microphone stream.
func MicClose(version uint16, sessionID byte) []byte {
	if version >= version1 {
		return []byte{cmdMicClose, sessionID}
	}
	return []byte{cmdMicClose}
}

// SupportsSampleRate reports whether the bridge accepts audio at rate (Hz).
func SupportsSampleRate(rate int) bool {
	return rate == SupportedSampleRate
}

// Capabilities is the parsed capabilities response (opcode 0x0B).
type Capabilities struct {
	Version       uint16
	Codecs        byte
	Interaction   byte
	FrameSize     int
	SelectedCodec byte
	SampleRate    int
}

// ParseCapabilities parses a capabilities response. It returns false for a
// payload that is too short or has the wrong opcode.
func ParseCapabilities(b []byte) (Capabilities, bool) {
	if len(b) < 7 || b[0] != OpCapabilities {
		return Capabilities{}, false
	}
	version := uint16(b[1])<<8 | uint16(b[2])

	var codecs, interaction byte
	if version >= version1 {
		codecs = b[3]
		interaction = b[4]
		// Some remotes report version 1.0 but keep the legacy codec layout:
		// codecs byte zero, codec bits in byte 4.
		if codecs == 0 && len(b) >= 9 && b[4]&0x03 != 0 {
			codecs = b[4]
			interaction = 0x03
		}
	} else {
		if len(b) < 9 {
			return Capabilities{}, false
		}
		codecs = b[4]
	}

	frameSize := int(b[5])<<8 | int(b[6])
	if frameSize == 0 {
		frameSize = DefaultFrameSize
	}
	selected := Codec8kHz
	if codecs&Codec16kHz != 0 {
		selected = Codec16kHz
	}
	return Capabilities{
		Version:       version,
		Codecs:        codecs,
		Interaction:   interaction,
		FrameSize:     frameSize,
		SelectedCodec: selected,
		SampleRate:    sampleRateFor(selected),
	}, true
}

func sampleRateFor(codec byte) int {
	if codec == Codec16kHz {
		return 16000
	}
	return 8000
}
