// SPDX-License-Identifier: GPL-3.0-only

package atvv

import (
	"errors"
	"fmt"
	"time"
)

// LateAudioGuard is how long after an AUDIO_STOP stray audio notifications
// are treated as leftovers of the finished session and dropped, instead of
// implicitly starting a new one.
const LateAudioGuard = 300 * time.Millisecond

var (
	// ErrMalformed reports a control payload that cannot be parsed.
	ErrMalformed = errors.New("atvv: malformed control payload")
	// ErrUnsupportedRate reports a remote that offers or switches to a codec
	// other than 16 kHz. The caller should drop the connection.
	ErrUnsupportedRate = errors.New("atvv: remote does not offer 16 kHz audio")
	// ErrNotReady reports protocol data received before capabilities were
	// confirmed.
	ErrNotReady = errors.New("atvv: capabilities not confirmed")
)

// EventKind classifies what a control notification meant.
type EventKind int

const (
	EventNone EventKind = iota
	// EventCapabilities: capabilities accepted; the session is ready.
	EventCapabilities
	// EventMicButton: the voice key was pressed. The caller should write
	// Session.MicOpenCommand to the Transmit characteristic.
	EventMicButton
	// EventAudioStart: a voice stream started.
	EventAudioStart
	// EventAudioStop: the voice stream ended (voice key released).
	EventAudioStop
	// EventAudioSync: decoder state will be resynchronised on the next frame.
	EventAudioSync
	// EventUnknown: an opcode this package does not handle.
	EventUnknown
)

func (k EventKind) String() string {
	switch k {
	case EventNone:
		return "none"
	case EventCapabilities:
		return "capabilities"
	case EventMicButton:
		return "mic_button"
	case EventAudioStart:
		return "audio_start"
	case EventAudioStop:
		return "audio_stop"
	case EventAudioSync:
		return "audio_sync"
	case EventUnknown:
		return "unknown"
	}
	return fmt.Sprintf("EventKind(%d)", int(k))
}

// Event is the outcome of one control notification.
type Event struct {
	Kind         EventKind
	Opcode       byte
	SessionID    byte
	Capabilities Capabilities
}

type syncState struct {
	predictor int
	stepIndex int
}

// Session holds the ATVV protocol state of one BLE connection. Create a new
// Session for every connection attempt so no state leaks across reconnects.
// A Session is not safe for concurrent use.
type Session struct {
	// GainDB is applied by Postprocess to every decoded frame.
	GainDB float64

	now        func() time.Time
	caps       Capabilities
	ready      bool
	streaming  bool
	sessionID  byte
	stoppedAt  time.Time
	hasStopped bool
	sync       *syncState
	dec        Decoder
	acc        FrameAccumulator
}

// NewSession returns a Session. now may be nil to use time.Now.
func NewSession(now func() time.Time) *Session {
	if now == nil {
		now = time.Now
	}
	return &Session{now: now}
}

// Ready reports whether 16 kHz capabilities have been confirmed.
func (s *Session) Ready() bool { return s.ready }

// Streaming reports whether a voice stream is in progress.
func (s *Session) Streaming() bool { return s.streaming }

// Capabilities returns the negotiated capabilities.
func (s *Session) Capabilities() Capabilities { return s.caps }

// MicOpenCommand returns the command to send after EventMicButton.
func (s *Session) MicOpenCommand() []byte {
	return MicOpen(s.caps.Version, s.caps.SelectedCodec)
}

// MicCloseCommand returns the command that closes the current stream.
func (s *Session) MicCloseCommand() []byte {
	return MicClose(s.caps.Version, s.sessionID)
}

// HandleControl processes one Control notification.
func (s *Session) HandleControl(b []byte) (Event, error) {
	if len(b) == 0 {
		return Event{}, ErrMalformed
	}
	op := b[0]
	if op != OpCapabilities && !s.ready {
		return Event{Opcode: op}, ErrNotReady
	}

	switch op {
	case OpCapabilities:
		caps, ok := ParseCapabilities(b)
		if !ok {
			return Event{Opcode: op}, ErrMalformed
		}
		s.caps = caps
		if !SupportsSampleRate(caps.SampleRate) {
			s.ready = false
			return Event{Opcode: op, Capabilities: caps}, ErrUnsupportedRate
		}
		s.ready = true
		return Event{Kind: EventCapabilities, Opcode: op, Capabilities: caps}, nil

	case OpMicButton:
		return Event{Kind: EventMicButton, Opcode: op}, nil

	case OpAudioStart:
		if len(b) >= 3 {
			s.caps.Interaction = b[1]
			s.caps.SelectedCodec = b[2]
			s.caps.SampleRate = sampleRateFor(b[2])
		}
		if !SupportsSampleRate(s.caps.SampleRate) {
			s.stopStreaming()
			return Event{Opcode: op, Capabilities: s.caps}, ErrUnsupportedRate
		}
		s.sessionID = 0
		if len(b) >= 4 {
			s.sessionID = b[3]
		}
		// A repeated AUDIO_START (or one after audio already started the
		// stream implicitly) still resets the decoder but is not reported
		// again, so consumers see exactly one start per stream.
		if !s.startStreaming() {
			return Event{Opcode: op, SessionID: s.sessionID}, nil
		}
		return Event{Kind: EventAudioStart, Opcode: op, SessionID: s.sessionID}, nil

	case OpAudioStop:
		if !s.stopStreaming() {
			return Event{Opcode: op}, nil
		}
		return Event{Kind: EventAudioStop, Opcode: op, SessionID: s.sessionID}, nil

	case OpAudioSync:
		if len(b) < 7 {
			return Event{Opcode: op}, nil
		}
		predictor := int(int16(uint16(b[4])<<8 | uint16(b[5])))
		s.sync = &syncState{predictor: predictor, stepIndex: int(b[6])}
		s.acc.Reset()
		return Event{Kind: EventAudioSync, Opcode: op}, nil
	}
	return Event{Kind: EventUnknown, Opcode: op}, nil
}

// HandleAudio processes one Audio notification and returns the decoded,
// post-processed PCM (16 kHz mono). started is true when the audio implicitly
// began a stream because it raced ahead of AUDIO_START.
func (s *Session) HandleAudio(b []byte) (pcm []int16, started bool, err error) {
	if !s.ready {
		return nil, false, ErrNotReady
	}
	if !s.streaming {
		if s.hasStopped && s.now().Sub(s.stoppedAt) < LateAudioGuard {
			return nil, false, nil
		}
		s.startStreaming()
		started = true
	}
	for _, frame := range s.acc.Append(b, s.caps.FrameSize) {
		if s.sync != nil {
			s.dec.Reset(s.sync.predictor, s.sync.stepIndex)
			s.sync = nil
		}
		decoded := s.dec.Decode(make([]int16, 0, 2*len(frame)), frame)
		pcm = append(pcm, Postprocess(decoded, s.GainDB)...)
	}
	return pcm, started, nil
}

// startStreaming resets the decoding state and reports whether this began a
// new stream (false if one was already in progress).
func (s *Session) startStreaming() bool {
	s.acc.Reset()
	s.sync = nil
	s.dec.Reset(0, 0)
	s.hasStopped = false
	wasStreaming := s.streaming
	s.streaming = true
	return !wasStreaming
}

// stopStreaming ends the stream and reports whether one was in progress.
func (s *Session) stopStreaming() bool {
	if !s.streaming {
		return false
	}
	s.streaming = false
	s.acc.Reset()
	s.sync = nil
	s.stoppedAt = s.now()
	s.hasStopped = true
	return true
}
