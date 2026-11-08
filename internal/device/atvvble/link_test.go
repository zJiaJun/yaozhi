// SPDX-License-Identifier: GPL-3.0-only

package atvvble

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/zJiaJun/yaozhi/internal/atvv"
	"github.com/zJiaJun/yaozhi/internal/device"
)

// caps16k advertises v1.0, 16 kHz, interaction mode 03 and a 4-byte frame
// (RC003 uses 120 bytes; a short frame keeps fixtures small).
var caps16k = []byte{0x0B, 0x01, 0x00, 0x02, 0x03, 0x00, 0x04}

type recorder struct {
	events []device.Event
	writes [][]byte
}

func (r *recorder) link() *link {
	return newLink(
		func(e device.Event) { r.events = append(r.events, e) },
		func(b []byte) { r.writes = append(r.writes, b) },
		func(string, ...any) {},
	)
}

// kinds summarises events, with the PCM length for audio.
func (r *recorder) kinds() string {
	var s string
	for _, e := range r.events {
		switch e := e.(type) {
		case device.VoiceStarted:
			s += "start "
		case device.VoiceAudio:
			s += fmt.Sprintf("audio%d ", len(e.PCM))
		case device.VoiceEnded:
			if e.Interrupted {
				s += "interrupted "
			} else {
				s += "end "
			}
		default:
			s += fmt.Sprintf("%T ", e)
		}
	}
	return s
}

func readyLink(t *testing.T) (*link, *recorder) {
	t.Helper()
	r := &recorder{}
	l := r.link()
	if ready, err := l.control(caps16k); !ready || err != nil || !l.ready() {
		t.Fatalf("capabilities: ready %v, err %v", ready, err)
	}
	return l, r
}

func TestLinkVoiceStream(t *testing.T) {
	l, r := readyLink(t)
	l.control([]byte{atvv.OpAudioStart, 0x03, atvv.Codec16kHz, 0x01})
	l.audio([]byte{1, 2, 3, 4})
	l.audio([]byte{5, 6})    // half a frame: nothing yet
	l.audio([]byte{7, 8, 9}) // completes it, one byte left over
	l.control([]byte{atvv.OpAudioStop})
	if got, want := r.kinds(), "start audio8 audio8 end "; got != want {
		t.Fatalf("events %q, want %q", got, want)
	}
	if len(r.writes) != 0 {
		t.Fatalf("RC003-style stream wrote % x", r.writes)
	}
}

func TestLinkOpensMicOnRequest(t *testing.T) {
	l, r := readyLink(t)
	l.control([]byte{atvv.OpMicButton})
	if len(r.writes) != 1 || !bytes.Equal(r.writes[0], atvv.MicOpen(0x0100, atvv.Codec16kHz)) {
		t.Fatalf("writes % x, want MIC_OPEN", r.writes)
	}
	if len(r.events) != 0 {
		t.Fatalf("mic button emitted %q", r.kinds())
	}
}

func TestLinkImplicitStart(t *testing.T) {
	l, r := readyLink(t)
	l.audio([]byte{1, 2, 3, 4})
	l.control([]byte{atvv.OpAudioStart, 0x03, atvv.Codec16kHz, 0x01}) // late, not reported again
	l.control([]byte{atvv.OpAudioStop})
	if got, want := r.kinds(), "start audio8 end "; got != want {
		t.Fatalf("events %q, want %q", got, want)
	}
}

func TestLinkEndInterrupts(t *testing.T) {
	l, r := readyLink(t)
	l.end(true) // nothing streaming: no event, no write
	l.control([]byte{atvv.OpAudioStart, 0x03, atvv.Codec16kHz, 0x07})
	l.end(true)
	l.end(true) // only once
	if got, want := r.kinds(), "start interrupted "; got != want {
		t.Fatalf("events %q, want %q", got, want)
	}
	if len(r.writes) != 1 || !bytes.Equal(r.writes[0], atvv.MicClose(0x0100, 0x07)) {
		t.Fatalf("writes % x, want MIC_CLOSE for session 7", r.writes)
	}

	// After a disconnect the mic cannot be closed; only the event is sent.
	l2, r2 := readyLink(t)
	l2.control([]byte{atvv.OpAudioStart, 0x03, atvv.Codec16kHz, 0x01})
	l2.end(false)
	if got := r2.kinds(); got != "start interrupted " || len(r2.writes) != 0 {
		t.Fatalf("disconnect: events %q, writes % x", got, r2.writes)
	}
}

func TestLinkRejects8kHz(t *testing.T) {
	r := &recorder{}
	l := r.link()
	only8k := []byte{0x0B, 0x01, 0x00, 0x01, 0x00, 0x00, 0x78}
	if ready, err := l.control(only8k); ready || !errors.Is(err, atvv.ErrUnsupportedRate) {
		t.Fatalf("8 kHz caps: ready %v, err %v", ready, err)
	}

	l, r = readyLink(t)
	l.control([]byte{atvv.OpAudioStart, 0x03, atvv.Codec16kHz, 0x01})
	if _, err := l.control([]byte{atvv.OpAudioStart, 0x03, atvv.Codec8kHz, 0x02}); !errors.Is(err, atvv.ErrUnsupportedRate) {
		t.Fatalf("switch to 8 kHz: err %v", err)
	}
	// The stream that was running ends; nothing is left open.
	if got := r.kinds(); got != "start interrupted " {
		t.Fatalf("events %q", got)
	}
}

func TestLinkIgnoresBeforeReady(t *testing.T) {
	r := &recorder{}
	l := r.link()
	l.audio([]byte{1, 2, 3, 4})
	if ready, err := l.control([]byte{atvv.OpAudioStart}); ready || err != nil {
		t.Fatalf("start before caps: ready %v, err %v", ready, err)
	}
	if len(r.events) != 0 || len(r.writes) != 0 {
		t.Fatalf("events %q, writes % x", r.kinds(), r.writes)
	}
}
