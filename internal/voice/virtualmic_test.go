// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuiltinTypeless(t *testing.T) {
	ps := BuiltinPresets()
	if len(ps) != 1 || ps[0].ID != "typeless" {
		t.Fatalf("built-in presets: %+v", ps)
	}
	p := ps[0]
	if p.Trigger != (Trigger{Key: KeyFn, Mode: ModeToggle}) || p.Microphone != "yaozhi-mic" || p.Timing.Tap() != 120*time.Millisecond {
		t.Fatalf("typeless preset: %+v", p)
	}
}

func TestParsePresetRejects(t *testing.T) {
	const ok = `
id = "handy"
name = "Handy"
[trigger]
key = "fn"
mode = "hold"
`
	if p, err := ParsePreset([]byte(ok)); err != nil || p.Timing.Tap() != DefaultTapMS*time.Millisecond {
		t.Fatalf("minimal preset: %+v, %v", p, err)
	}
	for name, src := range map[string]string{
		"unknown field": ok + "hotkey = \"f5\"\n",
		"key":           strings.Replace(ok, `"fn"`, `"option+space"`, 1),
		"mode":          strings.Replace(ok, `"hold"`, `"press"`, 1),
		"negative":      ok + "[timing]\nready_ms = -1\n",
		"too long":      ok + "[timing]\nend_delay_ms = 5000\n",
		"id":            strings.Replace(ok, `"handy"`, `"Handy App"`, 1),
	} {
		if _, err := ParsePreset([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// fakeOut is an AudioOut whose playback finishes immediately.
type fakeOut struct {
	mu       sync.Mutex
	written  int64
	waited   []int64
	stuck    bool
	running  bool
	starts   int
	stops    int
	startErr error
}

func (o *fakeOut) Start() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.startErr != nil {
		return o.startErr
	}
	o.starts++
	o.running = true
	return nil
}

func (o *fakeOut) Stop() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.running {
		o.stops++
	}
	o.running = false
}

func (o *fakeOut) state() (running bool, starts, stops int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running, o.starts, o.stops
}

func (o *fakeOut) Write(pcm []int16) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.written += int64(len(pcm))
	return o.written
}

func (o *fakeOut) WaitPlayed(pos int64, _ time.Duration) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.waited = append(o.waited, pos)
	return !o.stuck
}

// keyLog records key events and the sleeps between them.
type keyLog struct {
	mu   sync.Mutex
	log  []string
	fail bool
}

func (k *keyLog) setKey(down bool) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.fail {
		return errors.New("not trusted")
	}
	if down {
		k.log = append(k.log, "down")
	} else {
		k.log = append(k.log, "up")
	}
	return nil
}

func (k *keyLog) sleep(d time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if d > 0 {
		k.log = append(k.log, d.String())
	}
}

func (k *keyLog) String() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return strings.Join(k.log, " ")
}

func newTestMic(mode string, timing Timing) (*VirtualMic, *keyLog, *fakeOut) {
	k, out := &keyLog{}, &fakeOut{}
	p := Preset{ID: "t", Name: "T", Trigger: Trigger{Key: KeyFn, Mode: mode}, Timing: timing}
	v := NewVirtualMic(p, out, k.setKey, nil)
	v.sleep = k.sleep
	return v, k, out
}

// wait runs f with a done callback and returns its error.
func wait(t *testing.T, f func(func(error))) error {
	t.Helper()
	ch := make(chan error, 1)
	f(func(err error) { ch <- err })
	select {
	case err := <-ch:
		return err
	case <-time.After(time.Second):
		t.Fatal("no completion")
		return nil
	}
}

func TestVirtualMicToggle(t *testing.T) {
	v, k, out := newTestMic(ModeToggle, Timing{ReadyMS: 50, EndDelayMS: 30})
	if err := wait(t, v.Begin); err != nil {
		t.Fatal(err)
	}
	v.Write(make([]int16, 100))
	v.Write(make([]int16, 20))
	if err := wait(t, v.End); err != nil {
		t.Fatal(err)
	}
	if got, want := k.String(), "down 120ms up 50ms 30ms down 120ms up"; got != want {
		t.Fatalf("keys %q, want %q", got, want)
	}
	if len(out.waited) != 1 || out.waited[0] != 120 {
		t.Fatalf("waited for %v, want position 120", out.waited)
	}
	v.Abort() // already stopped: no extra tap
	if strings.Count(k.String(), "down") != 2 {
		t.Fatalf("Abort tapped again: %s", k)
	}
}

func TestVirtualMicHold(t *testing.T) {
	v, k, _ := newTestMic(ModeHold, Timing{})
	wait(t, v.Begin)
	wait(t, v.End)
	if got := k.String(); got != "down up" {
		t.Fatalf("keys %q", got)
	}
}

func TestVirtualMicEndsEvenIfAudioStuck(t *testing.T) {
	v, k, out := newTestMic(ModeToggle, Timing{})
	out.stuck = true
	wait(t, v.Begin)
	if err := wait(t, v.End); err != nil || strings.Count(k.String(), "down") != 2 {
		t.Fatalf("err %v, keys %s", err, k)
	}
}

func TestVirtualMicAbort(t *testing.T) {
	v, k, _ := newTestMic(ModeToggle, Timing{})
	wait(t, v.Begin)
	v.Abort()
	if strings.Count(k.String(), "down") != 2 {
		t.Fatalf("Abort did not stop the input method: %s", k)
	}
	// A Begin that completes after Abort undoes itself.
	v2, k2, _ := newTestMic(ModeHold, Timing{})
	v2.Abort()
	if err := wait(t, v2.Begin); !errors.Is(err, errAborted) || k2.String() != "down up" {
		t.Fatalf("err %v, keys %s", err, k2)
	}
}

func TestVirtualMicKeyFailure(t *testing.T) {
	v, k, _ := newTestMic(ModeToggle, Timing{})
	k.fail = true
	if err := wait(t, v.Begin); err == nil {
		t.Fatal("Begin succeeded without key events")
	}
	// Still off: End and Abort do not try to stop it.
	k.fail = false
	wait(t, v.End)
	v.Abort()
	if k.String() != "" {
		t.Fatalf("keys %s", k)
	}
}

func TestVirtualMicRunsOutputOnlyWhileNeeded(t *testing.T) {
	v, _, out := newTestMic(ModeToggle, Timing{})
	v.idleStop = 30 * time.Millisecond
	if running, _, _ := out.state(); running {
		t.Fatal("output running before any stream")
	}
	wait(t, v.Begin)
	if running, starts, _ := out.state(); !running || starts != 1 {
		t.Fatalf("after Begin: running %v, starts %d", running, starts)
	}
	wait(t, v.End)
	if running, _, _ := out.state(); !running {
		t.Fatal("output stopped right after End; it should idle a while first")
	}
	time.Sleep(80 * time.Millisecond)
	if running, _, stops := out.state(); running || stops != 1 {
		t.Fatalf("after idle: running %v, stops %d", running, stops)
	}
}

func TestVirtualMicKeepsOutputForNextStream(t *testing.T) {
	v, _, out := newTestMic(ModeToggle, Timing{})
	v.idleStop = 60 * time.Millisecond
	wait(t, v.Begin)
	wait(t, v.End)
	time.Sleep(20 * time.Millisecond)
	wait(t, v.Begin) // a new stream within the idle time
	time.Sleep(100 * time.Millisecond)
	if running, _, stops := out.state(); !running || stops != 0 {
		t.Fatalf("output stopped during the second stream: running %v, stops %d", running, stops)
	}
	v.Abort()
	if running, _, _ := out.state(); running {
		t.Fatal("Abort left the output running")
	}
}

func TestVirtualMicStartFailure(t *testing.T) {
	v, k, out := newTestMic(ModeToggle, Timing{})
	out.startErr = errors.New("device gone")
	if err := wait(t, v.Begin); err == nil {
		t.Fatal("Begin succeeded without an output")
	}
	// The trigger went down and up again, so the input method is not left on.
	if got := k.String(); strings.Count(got, "down") != 2 {
		t.Fatalf("keys %q: the input method should be started and stopped", got)
	}
}
