// SPDX-License-Identifier: GPL-3.0-only

package voice

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// AudioOut is the virtual microphone: audio written to it is what the input
// method hears. coreaudio.Player playing into BlackHole implements it.
type AudioOut interface {
	// Start starts playing; a started output plays silence between writes.
	Start() error
	// Stop stops playing, dropping what has not played yet.
	Stop()
	// Write queues audio and returns the position after it.
	Write(pcm []int16) int64
	// WaitPlayed waits until everything up to pos has played, or timeout.
	WaitPlayed(pos int64, timeout time.Duration) bool
}

// IdleStop is how long the output keeps running after a stream, so that
// streams in quick succession do not stop and start it each time. Stopped,
// it costs nothing; started, it plays silence 50 times a second.
const IdleStop = 3 * time.Second

// DrainTimeout bounds the wait for the last audio to play. Well above the
// measured 73–95 ms, so it only fires when the output is stuck.
const DrainTimeout = 2 * time.Second

// VirtualMic is the InputMethod for input methods that listen to a
// microphone and are started and stopped by a shortcut, as described by a
// Preset.
type VirtualMic struct {
	preset Preset
	out    AudioOut
	setKey func(down bool) error
	sleep  func(time.Duration)
	logf   func(string, ...any)

	keyMu sync.Mutex // serialises trigger, so taps never interleave

	idleStop time.Duration

	mu       sync.Mutex
	pos      int64 // position after the latest Write
	on       bool  // the input method was started and not yet stopped
	aborted  bool
	stopIdle *time.Timer // stops the output once streams are over
}

var _ InputMethod = (*VirtualMic)(nil)

// NewVirtualMic returns a VirtualMic. setKey presses (true) or releases
// (false) the preset's trigger key. logf may be nil.
func NewVirtualMic(p Preset, out AudioOut, setKey func(down bool) error, logf func(string, ...any)) *VirtualMic {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &VirtualMic{preset: p, out: out, setKey: setKey, sleep: time.Sleep, logf: logf, idleStop: IdleStop}
}

// errAborted reports a Begin or End cut short by Abort.
var errAborted = errors.New("voice: aborted")

// Begin implements InputMethod: tap or press the trigger, then wait the
// preset's ready time.
func (v *VirtualMic) Begin(done func(error)) {
	v.mu.Lock()
	if v.stopIdle != nil {
		v.stopIdle.Stop()
		v.stopIdle = nil
	}
	v.mu.Unlock()
	go func() {
		// The output starts while the trigger is pressed; the session holds
		// the audio until Begin is done, so its start-up costs no speech.
		started := make(chan error, 1)
		go func() {
			t0 := time.Now()
			err := v.out.Start()
			if d := time.Since(t0); d > 100*time.Millisecond {
				v.logf("虚拟麦克风启动用了 %d ms", d.Milliseconds())
			}
			started <- err
		}()
		err := v.trigger(true)
		if serr := <-started; serr != nil && err == nil {
			_ = v.trigger(false)
			err = fmt.Errorf("虚拟麦克风：%w", serr)
		}
		if err == nil {
			v.sleep(v.preset.Timing.Ready())
		}
		v.mu.Lock()
		aborted := v.aborted
		v.mu.Unlock()
		if err == nil && aborted {
			// Abort ran while the trigger was in flight; undo it.
			_ = v.trigger(false)
			err = errAborted
		}
		done(err)
	}()
}

// Write implements InputMethod.
func (v *VirtualMic) Write(pcm []int16) {
	pos := v.out.Write(pcm)
	v.mu.Lock()
	v.pos = pos
	v.mu.Unlock()
}

// End implements InputMethod: wait for the audio to play out, then tap or
// release the trigger.
func (v *VirtualMic) End(done func(error)) {
	v.mu.Lock()
	pos := v.pos
	v.mu.Unlock()
	go func() {
		start := time.Now()
		if !v.out.WaitPlayed(pos, DrainTimeout) {
			v.logf("语音 %v 内没播完，直接结束", DrainTimeout)
		} else {
			v.logf("剩余语音 %d ms 后播完", time.Since(start).Milliseconds())
		}
		v.sleep(v.preset.Timing.EndDelay())
		err := v.trigger(false)
		v.scheduleStop()
		done(err)
	}()
}

// Abort implements InputMethod: if the input method may be listening, stop it
// now, without waiting for the audio.
func (v *VirtualMic) Abort() {
	v.mu.Lock()
	v.aborted = true
	v.mu.Unlock()
	if err := v.trigger(false); err != nil {
		v.logf("停止输入法失败：%v", err)
	}
	v.mu.Lock()
	if v.stopIdle != nil {
		v.stopIdle.Stop()
		v.stopIdle = nil
	}
	v.mu.Unlock()
	v.out.Stop()
}

// scheduleStop stops the output after idleStop, unless Begin comes first.
func (v *VirtualMic) scheduleStop() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.stopIdle != nil {
		v.stopIdle.Stop()
	}
	var t *time.Timer
	t = time.AfterFunc(v.idleStop, func() {
		// Stop under v.mu: a Begin that comes now waits for it, then
		// starts the output again, instead of being stopped after it.
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.stopIdle == t {
			v.stopIdle = nil
			v.out.Stop()
		}
	})
	v.stopIdle = t
}

// trigger starts (start=true) or stops the input method. It records whether
// the input method is on, so Abort and repeated calls do not toggle it the
// wrong way; a call that would not change it does nothing.
func (v *VirtualMic) trigger(start bool) error {
	v.keyMu.Lock()
	defer v.keyMu.Unlock()
	v.mu.Lock()
	on := v.on
	v.mu.Unlock()
	if on == start {
		return nil
	}

	var err error
	switch v.preset.Trigger.Mode {
	case ModeHold:
		err = v.setKey(start)
	default: // ModeToggle
		if err = v.setKey(true); err == nil {
			v.sleep(v.preset.Timing.Tap())
			err = v.setKey(false)
		}
	}
	if err != nil {
		// Keep the old state: the key event did not get through.
		return fmt.Errorf("%s 快捷键：%w", v.preset.Name, err)
	}
	v.mu.Lock()
	v.on = start
	v.mu.Unlock()
	return nil
}
