// SPDX-License-Identifier: GPL-3.0-only

package atvv

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"
)

// caps16k advertises v1.0, 16 kHz and a 4-byte frame to keep fixtures small.
var caps16k = []byte{0x0B, 0x01, 0x00, 0x02, 0x03, 0x00, 0x04}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func readySession(t *testing.T) (*Session, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	s := NewSession(clock.now)
	ev, err := s.HandleControl(caps16k)
	if err != nil || ev.Kind != EventCapabilities || !s.Ready() {
		t.Fatalf("capabilities: event %v, err %v, ready %v", ev.Kind, err, s.Ready())
	}
	return s, clock
}

func mustControl(t *testing.T, s *Session, b []byte, want EventKind) Event {
	t.Helper()
	ev, err := s.HandleControl(b)
	if err != nil {
		t.Fatalf("HandleControl(% X): %v", b, err)
	}
	if ev.Kind != want {
		t.Fatalf("HandleControl(% X) = %v, want %v", b, ev.Kind, want)
	}
	return ev
}

// expectedPCM decodes frames with a fresh decoder in the given state, the way
// a session should right after AUDIO_START or AUDIO_SYNC.
func expectedPCM(predictor, stepIndex int, gainDB float64, frames ...[]byte) []int16 {
	var d Decoder
	d.Reset(predictor, stepIndex)
	var out []int16
	for _, f := range frames {
		out = append(out, Postprocess(d.Decode(nil, f), gainDB)...)
	}
	return out
}

func TestSessionRequiresCapabilitiesFirst(t *testing.T) {
	s := NewSession(nil)
	if _, err := s.HandleControl([]byte{OpMicButton}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("mic button before caps: err = %v", err)
	}
	if _, _, err := s.HandleAudio([]byte{1, 2, 3, 4}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("audio before caps: err = %v", err)
	}
}

func TestSessionRejectsBadCapabilities(t *testing.T) {
	s := NewSession(nil)
	if _, err := s.HandleControl(nil); !errors.Is(err, ErrMalformed) {
		t.Fatalf("empty payload: err = %v", err)
	}
	if _, err := s.HandleControl([]byte{0x0B, 0x01}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("truncated caps: err = %v", err)
	}
	only8k := []byte{0x0B, 0x01, 0x00, 0x01, 0x00, 0x00, 0x78}
	if _, err := s.HandleControl(only8k); !errors.Is(err, ErrUnsupportedRate) {
		t.Fatalf("8 kHz caps: err = %v", err)
	}
	if s.Ready() {
		t.Fatal("session ready after 8 kHz caps")
	}
}

func TestSessionVoiceFlow(t *testing.T) {
	s, clock := readySession(t)

	mustControl(t, s, []byte{OpMicButton}, EventMicButton)
	if got := s.MicOpenCommand(); !bytes.Equal(got, []byte{0x0C, 0x00}) {
		t.Fatalf("MicOpenCommand = % X", got)
	}

	ev := mustControl(t, s, []byte{OpAudioStart, 0x03, Codec16kHz, 0x05}, EventAudioStart)
	if ev.SessionID != 5 || !s.Streaming() {
		t.Fatalf("start: session %d, streaming %v", ev.SessionID, s.Streaming())
	}
	if got := s.MicCloseCommand(); !bytes.Equal(got, []byte{0x0D, 0x05}) {
		t.Fatalf("MicCloseCommand = % X", got)
	}

	// Notifications split mid-frame are reassembled.
	f1, f2 := []byte{0x12, 0x34, 0x56, 0x78}, []byte{0x9A, 0xBC, 0xDE, 0xF0}
	pcm1, started, err := s.HandleAudio(append(slices.Clone(f1), f2[:2]...))
	if err != nil || started {
		t.Fatalf("audio 1: started %v, err %v", started, err)
	}
	pcm2, _, err := s.HandleAudio(f2[2:])
	if err != nil {
		t.Fatalf("audio 2: %v", err)
	}
	if got, want := append(pcm1, pcm2...), expectedPCM(0, 0, 0, f1, f2); !slices.Equal(got, want) {
		t.Fatalf("pcm = %v, want %v", got, want)
	}

	mustControl(t, s, []byte{OpAudioStop}, EventAudioStop)
	if s.Streaming() {
		t.Fatal("still streaming after stop")
	}
	// A second stop is not reported again.
	mustControl(t, s, []byte{OpAudioStop}, EventNone)

	// Leftover audio right after the stop is dropped...
	clock.advance(LateAudioGuard - time.Millisecond)
	if pcm, started, err := s.HandleAudio(f1); err != nil || started || pcm != nil || s.Streaming() {
		t.Fatalf("late audio: pcm %v, started %v, err %v", pcm, started, err)
	}
	// ...but audio after the guard implicitly starts a fresh stream.
	clock.advance(2 * time.Millisecond)
	pcm, started, err := s.HandleAudio(f1)
	if err != nil || !started || !s.Streaming() {
		t.Fatalf("implicit start: started %v, err %v", started, err)
	}
	if want := expectedPCM(0, 0, 0, f1); !slices.Equal(pcm, want) {
		t.Fatalf("implicit stream pcm = %v, want %v", pcm, want)
	}
}

func TestSessionReportsStartOncePerStream(t *testing.T) {
	s, _ := readySession(t)
	mustControl(t, s, []byte{OpAudioStart, 0x03, Codec16kHz, 0x01}, EventAudioStart)
	mustControl(t, s, []byte{OpAudioStart, 0x03, Codec16kHz, 0x01}, EventNone)
}

func TestSessionSyncResetsDecoderOnNextFrame(t *testing.T) {
	s, _ := readySession(t)
	mustControl(t, s, []byte{OpAudioStart, 0x03, Codec16kHz, 0x01}, EventAudioStart)

	// A partial frame before the sync is discarded with the accumulator.
	if pcm, _, _ := s.HandleAudio([]byte{0xAA, 0xBB}); pcm != nil {
		t.Fatalf("partial frame produced %v", pcm)
	}
	// predictor 0xFF38 = -200, step index 10.
	mustControl(t, s, []byte{OpAudioSync, 0x00, 0x00, 0x00, 0xFF, 0x38, 10}, EventAudioSync)

	frame := []byte{0x13, 0x57, 0x9B, 0xDF}
	pcm, _, err := s.HandleAudio(frame)
	if err != nil {
		t.Fatal(err)
	}
	if want := expectedPCM(-200, 10, 0, frame); !slices.Equal(pcm, want) {
		t.Fatalf("pcm after sync = %v, want %v", pcm, want)
	}

	// A sync payload that is too short is ignored.
	mustControl(t, s, []byte{OpAudioSync, 0x00}, EventNone)
}

func TestSessionAppliesGain(t *testing.T) {
	s, _ := readySession(t)
	s.GainDB = 6
	mustControl(t, s, []byte{OpAudioStart, 0x03, Codec16kHz, 0x01}, EventAudioStart)
	frame := []byte{0x77, 0x77, 0x77, 0x77}
	pcm, _, _ := s.HandleAudio(frame)
	if want := expectedPCM(0, 0, 6, frame); !slices.Equal(pcm, want) {
		t.Fatalf("pcm = %v, want %v", pcm, want)
	}
}

func TestSessionFailsWhenStreamSwitchesTo8kHz(t *testing.T) {
	s, _ := readySession(t)
	if _, err := s.HandleControl([]byte{OpAudioStart, 0x03, Codec8kHz, 0x01}); !errors.Is(err, ErrUnsupportedRate) {
		t.Fatalf("err = %v, want ErrUnsupportedRate", err)
	}
	if s.Streaming() {
		t.Fatal("streaming after rejected start")
	}
}

func TestSessionUnknownOpcode(t *testing.T) {
	s, _ := readySession(t)
	ev := mustControl(t, s, []byte{0x42}, EventUnknown)
	if ev.Opcode != 0x42 {
		t.Fatalf("opcode = %#x", ev.Opcode)
	}
}
