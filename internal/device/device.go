// SPDX-License-Identifier: GPL-3.0-only

// Package device defines what every device adapter produces and the
// descriptor files that tell adapters which devices they handle.
//
// An adapter speaks one protocol (for example ATVV over Bluetooth LE, see
// package atvvble) and turns whatever the device sends into the events below.
// Everything downstream (the voice session, key mapping, the UI) consumes only
// these events and never sees protocol details such as codecs or GATT.
//
// Differences between devices that share a protocol (names, model numbers,
// HID IDs, key usages) live in TOML descriptors, not in code; see Descriptor.
package device

import (
	"context"
	"fmt"
)

// SampleRate is the rate of every VoiceAudio frame: 16 kHz, mono, 16-bit.
const SampleRate = 16000

// Device is one adapter instance.
type Device interface {
	// Run finds a matching device, connects, and keeps it connected until
	// ctx is cancelled, reconnecting after every disconnect. It calls emit
	// for every event, one call at a time and in order; emit must not block
	// for long because it runs on the adapter's I/O path.
	//
	// Run returns nil after ctx is cancelled, or an error when the adapter
	// cannot work at all (for example no Bluetooth permission).
	Run(ctx context.Context, emit func(Event)) error
}

// Event is one of StateChanged, InfoChanged, KeyChanged, VoiceStarted,
// VoiceAudio or VoiceEnded.
type Event interface{ event() }

// State is the connection state of an adapter.
type State int

const (
	// StateSearching: no device is connected; the adapter keeps looking.
	StateSearching State = iota
	// StateConnecting: a device is connected and being set up.
	StateConnecting
	// StateReady: the device is set up and voice can be used.
	StateReady
	// StateUnavailable: the transport is off (for example Bluetooth is
	// turned off). The adapter resumes by itself when it comes back.
	StateUnavailable
)

func (s State) String() string {
	switch s {
	case StateSearching:
		return "searching"
	case StateConnecting:
		return "connecting"
	case StateReady:
		return "ready"
	case StateUnavailable:
		return "unavailable"
	}
	return fmt.Sprintf("State(%d)", int(s))
}

// StateChanged reports a new connection state.
type StateChanged struct {
	State State
	// Device is the descriptor of the device involved, nil while searching
	// for an unknown one.
	Device *Descriptor
	// Name is what the device calls itself (its Bluetooth name), if known.
	Name string
	// Err explains why the previous connection ended or why the transport
	// is unavailable; nil otherwise.
	Err error
}

// InfoChanged reports what the connected device tells about itself. It
// comes after StateChanged, once per fact learned, each time with all that
// is known so far; a device that tells nothing sends none.
type InfoChanged struct {
	// Model is the device's model number ("RC003"), "" while unknown.
	Model string
	// Battery is the charge left in percent, -1 while unknown.
	Battery int
}

// KeyChanged reports a key going down or up. Key is a name from the
// descriptor's [keys] table.
type KeyChanged struct {
	Key  string
	Down bool
}

// VoiceStarted reports that the user started talking (for RC003: the voice
// key went down). Audio may lag behind it by up to a few hundred ms.
type VoiceStarted struct{}

// VoiceAudio carries decoded audio: SampleRate Hz, mono, 16-bit. The slice is
// owned by the receiver.
type VoiceAudio struct {
	PCM []int16
}

// VoiceEnded reports the end of the voice stream started by the last
// VoiceStarted. Interrupted is true when the stream did not end normally (the
// device disconnected or the adapter stopped); no more audio follows either
// way.
type VoiceEnded struct {
	Interrupted bool
}

func (StateChanged) event() {}
func (InfoChanged) event()  {}
func (KeyChanged) event()   {}
func (VoiceStarted) event() {}
func (VoiceAudio) event()   {}
func (VoiceEnded) event()   {}
