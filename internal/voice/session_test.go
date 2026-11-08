// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zJiaJun/yaozhi/internal/device"
)

// fakeIM records calls. Completions are held until the test releases them,
// the way a real input method answers later from another goroutine.
type fakeIM struct {
	calls   []string
	written []int16
	begins  []func(error)
	ends    []func(error)
}

func (f *fakeIM) Begin(done func(error)) {
	f.calls = append(f.calls, "begin")
	f.begins = append(f.begins, done)
}
func (f *fakeIM) End(done func(error)) {
	f.calls = append(f.calls, "end")
	f.ends = append(f.ends, done)
}
func (f *fakeIM) Abort() { f.calls = append(f.calls, "abort") }
func (f *fakeIM) Write(pcm []int16) {
	f.calls = append(f.calls, fmt.Sprintf("write%d", len(pcm)))
	f.written = append(f.written, pcm...)
}

// harness drives a machine synchronously: completions sent by the fake input
// method are queued and delivered by settle.
type harness struct {
	t     *testing.T
	im    *fakeIM
	m     *machine
	inbox []any
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, im: &fakeIM{}}
	// MinAudio 150 ms = 2400 samples, MaxBuffer 1 s = 16000 samples.
	h.m = newMachine(h.im, Config{MaxBuffer: time.Second}, func(msg any) { h.inbox = append(h.inbox, msg) })
	return h
}

func (h *harness) send(events ...device.Event) {
	for _, e := range events {
		h.m.handle(e)
	}
}

// settle delivers the pending completions until none are left.
func (h *harness) settle() {
	for len(h.inbox) > 0 {
		msg := h.inbox[0]
		h.inbox = h.inbox[1:]
		h.m.handle(msg)
	}
}

// beginDone and endDone answer the oldest outstanding Begin/End.
func (h *harness) beginDone(err error) {
	h.t.Helper()
	if len(h.im.begins) == 0 {
		h.t.Fatal("no Begin to answer")
	}
	done := h.im.begins[0]
	h.im.begins = h.im.begins[1:]
	done(err)
	h.settle()
}

func (h *harness) endDone(err error) {
	h.t.Helper()
	if len(h.im.ends) == 0 {
		h.t.Fatal("no End to answer")
	}
	done := h.im.ends[0]
	h.im.ends = h.im.ends[1:]
	done(err)
	h.settle()
}

func (h *harness) want(calls string) {
	h.t.Helper()
	if got := strings.Join(h.im.calls, " "); got != calls {
		h.t.Fatalf("calls:\n got  %s\n want %s", got, calls)
	}
}

// chunk is n samples of value v (240 samples = one RC003 notification).
func chunk(n int, v int16) device.VoiceAudio {
	pcm := make([]int16, n)
	for i := range pcm {
		pcm[i] = v
	}
	return device.VoiceAudio{PCM: pcm}
}

var (
	start = device.VoiceStarted{}
	end   = device.VoiceEnded{}
)

func TestSessionNormalStream(t *testing.T) {
	h := newHarness(t)
	h.send(start)
	for range 10 { // 2400 samples: reaches MinAudio
		h.send(chunk(240, 1000))
	}
	h.want("begin")
	h.send(chunk(240, 1000), chunk(240, 1000)) // still buffered
	h.beginDone(nil)
	// The buffer holds everything but the latest chunk.
	h.want("begin write2640")
	h.send(chunk(240, 1000))
	h.want("begin write2640 write240")
	h.send(end)
	h.want("begin write2640 write240 write240 end")
	h.endDone(nil)
	if h.m.phase != phaseIdle {
		t.Fatalf("phase %d after end", h.m.phase)
	}
	if len(h.im.written) != 13*240 {
		t.Fatalf("wrote %d samples, want all %d", len(h.im.written), 13*240)
	}
}

func TestSessionFadesLastChunk(t *testing.T) {
	h := newHarness(t)
	h.send(start, chunk(2400, 1000))
	h.beginDone(nil)
	h.send(chunk(240, 8000), end)
	last := h.im.written[len(h.im.written)-240:]
	if last[0] != 8000*239/240 || last[239] != 0 {
		t.Fatalf("last chunk not faded: first %d, last %d", last[0], last[239])
	}
	for i := 1; i < len(last); i++ {
		if last[i] > last[i-1] {
			t.Fatalf("fade not monotonic at %d", i)
		}
	}
	// Earlier audio is untouched.
	if h.im.written[0] != 1000 || h.im.written[2399] != 1000 {
		t.Fatal("audio before the last chunk was changed")
	}
}

func TestSessionAccidentalPress(t *testing.T) {
	h := newHarness(t)
	h.send(start, end) // no audio at all (seen on RC003: a 134 ms press)
	h.send(start, chunk(240, 1), chunk(240, 1), end)
	h.want("")
	if h.m.phase != phaseIdle {
		t.Fatal("not idle after accidental presses")
	}
	// A real stream afterwards works.
	h.send(start, chunk(2400, 1))
	h.want("begin")
}

func TestSessionEndsWhileBeginning(t *testing.T) {
	h := newHarness(t)
	h.send(start, chunk(2400, 1), chunk(240, 1), end)
	h.want("begin")
	h.beginDone(nil)
	h.want("begin write2400 write240 end")
}

func TestSessionQueuesNextStream(t *testing.T) {
	h := newHarness(t)
	h.send(start, chunk(2400, 1))
	h.beginDone(nil)
	h.send(end)
	h.want("begin write2400 end")
	// The user presses again before the first stream has finished.
	h.send(start, chunk(2400, 2), chunk(240, 2), end)
	h.want("begin write2400 end")
	h.endDone(nil)
	h.want("begin write2400 end begin")
	h.beginDone(nil)
	h.want("begin write2400 end begin write2400 write240 end")
	if h.im.written[2400] != 2 {
		t.Fatal("second stream audio mixed up")
	}
}

func TestSessionQueuedAccidentalPress(t *testing.T) {
	h := newHarness(t)
	h.send(start, chunk(2400, 1))
	h.beginDone(nil)
	h.send(end, start, end) // a short second press while the first ends
	h.endDone(nil)
	h.want("begin write2400 end")
	if h.m.phase != phaseIdle {
		t.Fatal("not idle")
	}
}

func TestSessionInterruptedStream(t *testing.T) {
	h := newHarness(t)
	h.send(start, chunk(2400, 1))
	h.beginDone(nil)
	h.send(chunk(240, 1), device.VoiceEnded{Interrupted: true})
	// The input method is still stopped properly, so it does not keep
	// listening after a disconnect.
	h.want("begin write2400 write240 end")
}

func TestSessionBeginFails(t *testing.T) {
	h := newHarness(t)
	h.send(start, chunk(2400, 1))
	h.beginDone(errors.New("no permission"))
	h.send(chunk(240, 1), end) // rest of the failed stream: ignored
	h.want("begin")
	h.send(start, chunk(2400, 1))
	h.want("begin begin")
}

func TestSessionReportsEvents(t *testing.T) {
	h := newHarness(t)
	var got []string
	h.m.note = func(e Event) {
		got = append(got, fmt.Sprintf("%d:%v:%v", e.Kind, e.Audio, e.Err != nil))
	}
	h.send(start, end) // accidental
	h.send(start, chunk(2400, 1))
	h.beginDone(nil)
	h.send(end)
	h.endDone(nil)
	h.send(start, chunk(2400, 1))
	h.beginDone(errors.New("no permission"))
	want := []string{
		fmt.Sprintf("%d:0s:false", Dropped),
		fmt.Sprintf("%d:0s:false", Listening),
		fmt.Sprintf("%d:150ms:false", Finished),
		fmt.Sprintf("%d:0s:true", Failed),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func TestSessionIgnoresStrayEvents(t *testing.T) {
	h := newHarness(t)
	h.send(chunk(240, 1), end, device.StateChanged{State: device.StateReady})
	h.want("")
}

func TestSessionBufferLimit(t *testing.T) {
	h := newHarness(t)
	h.send(start)
	for range 100 { // 24000 samples, above the 16000 limit, before Begin answers
		h.send(chunk(240, 1))
	}
	h.beginDone(nil)
	h.send(end)
	if n := len(h.im.written); n > 16000+240 {
		t.Fatalf("wrote %d samples, buffer limit not applied", n)
	}
}

func TestSessionShutdown(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(h *harness)
		calls string
	}{
		{"idle", func(h *harness) {}, ""},
		{"collecting", func(h *harness) { h.send(start, chunk(240, 1)) }, ""},
		{"beginning", func(h *harness) { h.send(start, chunk(2400, 1)) }, "begin abort"},
		{"active", func(h *harness) { h.send(start, chunk(2400, 1)); h.beginDone(nil) }, "begin abort"},
		{"ending", func(h *harness) { h.send(start, chunk(2400, 1)); h.beginDone(nil); h.send(end) }, "begin write2400 end abort"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.setup(h)
			h.m.shutdown()
			h.want(tc.calls)
			// Late completions after shutdown change nothing.
			for _, done := range slices.Concat(h.im.begins, h.im.ends) {
				done(nil)
			}
			h.settle()
			h.want(tc.calls)
		})
	}
}
