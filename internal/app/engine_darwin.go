// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

// Package app runs Yaozhi without its UI: the device, the voice session, the
// blocking of the voice key, and the checks of what Yaozhi needs (permissions,
// BlackHole, yaozhi-mic). The UI reads State and is told when it changes.
package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zJiaJun/yaozhi/internal/coreaudio"
	"github.com/zJiaJun/yaozhi/internal/device"
	"github.com/zJiaJun/yaozhi/internal/device/atvvble"
	"github.com/zJiaJun/yaozhi/internal/macinput"
	"github.com/zJiaJun/yaozhi/internal/voice"
)

const (
	// BlackHoleUID is the UID of the unmodified BlackHole 2ch driver.
	BlackHoleUID = "BlackHole2ch_UID"
	// MicName is the aggregate device input methods are set to.
	MicName = "yaozhi-mic"
)

// Perm is what is known of a permission.
type Perm int

const (
	PermUnknown Perm = iota
	PermGranted
	PermDenied
)

// VoicePhase is where the latest press of the voice key is.
type VoicePhase int

const (
	VoiceIdle      VoicePhase = iota
	VoiceListening            // the key is held
	VoiceEnding               // released; waiting for the audio to play out
	VoiceDone                 // the input method was told to finish
	VoiceFailed               // the input method could not be started
)

// Readiness is what Yaozhi needs before voice input works.
type Readiness struct {
	Bluetooth     Perm
	Accessibility bool
	BlackHole     bool   // the user installed BlackHole 2ch
	Mic           bool   // yaozhi-mic is there (Yaozhi makes it)
	MicErr        string // why Yaozhi could not make yaozhi-mic
}

// Steps counts the three steps done: Bluetooth, Accessibility, and the
// virtual microphone (BlackHole with yaozhi-mic on top).
func (r Readiness) Steps() int {
	n := 0
	for _, ok := range []bool{r.Bluetooth == PermGranted, r.Accessibility, r.BlackHole && r.Mic} {
		if ok {
			n++
		}
	}
	return n
}

// TotalSteps is what Steps counts to.
const TotalSteps = 3

// Ready reports whether voice input can work.
func (r Readiness) Ready() bool { return r.Steps() == TotalSteps }

// State is a snapshot for the UI.
type State struct {
	Conn    device.State
	Device  string // the device's name, once known
	ConnErr string // why the last connection ended

	Ready Readiness

	Voice      VoicePhase
	VoiceSince time.Time     // when Voice last changed
	LastAudio  time.Duration // speech in the latest finished stream
	Count      int           // streams finished since launch
	Problem    string        // why the latest stream failed
}

// Options configures an Engine.
type Options struct {
	Devices   []device.Descriptor
	Preset    voice.Preset
	AcceptAny bool
	// Logf receives diagnostics, never audio or text. nil discards them.
	Logf func(format string, args ...any)
}

// Engine runs everything but the UI.
type Engine struct {
	opts Options
	logf func(string, ...any)

	mu       sync.Mutex
	st       State
	onChange []func()

	session atomic.Pointer[voice.Session] // nil until BlackHole is there
	recheck chan struct{}

	// Used by checkLoop only, and RemoveMic.
	micMu      sync.Mutex
	micRemoved bool      // the user removed yaozhi-mic: do not make it again
	micTried   time.Time // the latest failed attempt, to retry slowly
}

// New returns an engine; Run starts it.
func New(o Options) *Engine {
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Engine{opts: o, logf: logf, recheck: make(chan struct{}, 1)}
}

// State returns the current state.
func (e *Engine) State() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.st
}

// OnChange registers f, called from any goroutine after the state changes.
// Register before Run.
func (e *Engine) OnChange(f func()) { e.onChange = append(e.onChange, f) }

// Recheck asks for the readiness checks to run now, as after the user
// granted a permission.
func (e *Engine) Recheck() {
	select {
	case e.recheck <- struct{}{}:
	default:
	}
}

func (e *Engine) update(f func(s *State)) {
	e.mu.Lock()
	before := e.st
	f(&e.st)
	changed := e.st != before
	e.mu.Unlock()
	if changed {
		for _, fn := range e.onChange {
			fn()
		}
	}
}

func (e *Engine) setVoice(p VoicePhase, f func(s *State)) {
	e.update(func(s *State) {
		s.Voice, s.VoiceSince = p, time.Now()
		if f != nil {
			f(s)
		}
	})
}

// Run runs until ctx is cancelled. Missing permissions or devices do not
// stop it: they show in State, and it carries on once they are there.
func (e *Engine) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	blocker := newKeyBlocker(e.logf)
	wg.Go(func() { blocker.run(ctx) })
	wg.Go(func() { e.checkLoop(ctx) })
	wg.Go(func() { e.voiceLoop(ctx) })
	wg.Go(func() { e.idleLoop(ctx) })

	a := atvvble.New(e.opts.Devices)
	a.AcceptAny = e.opts.AcceptAny
	a.Logf = e.logf
	err := a.Run(ctx, func(ev device.Event) {
		if s := e.session.Load(); s != nil {
			s.Handle(ev)
		}
		e.deviceEvent(ev, blocker)
	})
	if errors.Is(err, atvvble.ErrUnauthorized) {
		e.update(func(s *State) { s.Ready.Bluetooth = PermDenied })
		e.logf("没有蓝牙权限")
		<-ctx.Done()
		return nil
	}
	return err
}

func (e *Engine) deviceEvent(ev device.Event, blocker *keyBlocker) {
	switch ev := ev.(type) {
	case device.StateChanged:
		name := ev.Name
		if ev.Device != nil {
			name = ev.Device.Name
		}
		e.update(func(s *State) {
			s.Conn = ev.State
			if name != "" {
				s.Device = name
			}
			s.ConnErr = ""
			if ev.Err != nil {
				s.ConnErr = ev.Err.Error()
			}
			if ev.State != device.StateUnavailable {
				// CoreBluetooth only reports states once it may be used.
				s.Ready.Bluetooth = PermGranted
			}
			if ev.State != device.StateReady && (s.Voice == VoiceListening) {
				s.Voice, s.VoiceSince = VoiceIdle, time.Now()
			}
		})
		if ev.State == device.StateReady {
			blocker.block(ev.Device)
		}
	case device.VoiceStarted:
		e.setVoice(VoiceListening, func(s *State) { s.Problem = "" })
	case device.VoiceEnded:
		e.setVoice(VoiceEnding, nil)
	}
}

// sessionEvent follows the session, which knows whether the press counted.
func (e *Engine) sessionEvent(ev voice.Event) {
	switch ev.Kind {
	case voice.Dropped:
		e.setVoice(VoiceIdle, nil)
	case voice.Finished:
		e.setVoice(VoiceDone, func(s *State) { s.LastAudio = ev.Audio; s.Count++ })
	case voice.Failed:
		msg := ev.Err.Error()
		if errors.Is(ev.Err, macinput.ErrNotTrusted) {
			msg = "没有辅助功能权限，没法替你点 Fn"
		}
		e.setVoice(VoiceFailed, func(s *State) { s.Problem = msg })
	}
}

// idleLoop returns Done and Failed to Idle after a while, for the UI.
func (e *Engine) idleLoop(ctx context.Context) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			st := e.State()
			hold := map[VoicePhase]time.Duration{VoiceDone: 2600 * time.Millisecond, VoiceFailed: 5 * time.Second}[st.Voice]
			if hold > 0 && time.Since(st.VoiceSince) > hold {
				e.update(func(s *State) {
					if s.Voice == st.Voice && s.VoiceSince == st.VoiceSince {
						s.Voice, s.VoiceSince = VoiceIdle, time.Now()
					}
				})
			}
		}
	}
}

// checkLoop keeps Readiness current: permissions can be granted or taken
// away and devices come and go while Yaozhi runs. It also makes yaozhi-mic
// whenever it is missing.
func (e *Engine) checkLoop(ctx context.Context) {
	// Accessibility has no change notification, but checking it is cheap:
	// every 2 s. Listing the audio devices is not, so it runs when Core
	// Audio says the list changed, when asked to, while yaozhi-mic still
	// needs making, and once a minute in case a notification was missed.
	devicesChanged := make(chan struct{}, 1)
	if stop, err := coreaudio.WatchDevices(func() {
		select {
		case devicesChanged <- struct{}{}:
		default:
		}
	}); err != nil {
		e.logf("监听音频设备变化失败，改为定时检查：%v", err)
	} else {
		defer stop()
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	var lastDevices time.Time
	checkDevices := true
	for {
		ax, _ := macinput.CanPostEvents()
		st := e.State().Ready
		bh, mic, micErr := st.BlackHole, st.Mic, st.MicErr
		if checkDevices || !(bh && mic) || time.Since(lastDevices) > time.Minute {
			bh, mic, micErr = e.checkMic()
			lastDevices = time.Now()
		}
		e.update(func(s *State) {
			s.Ready.Accessibility, s.Ready.BlackHole, s.Ready.Mic, s.Ready.MicErr = ax, bh, mic, micErr
		})
		checkDevices = false
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-devicesChanged:
			checkDevices = true
		case <-e.recheck:
			checkDevices = true
		}
	}
}

// checkMic finds yaozhi-mic, making it when BlackHole is there and it is not.
func (e *Engine) checkMic() (blackHole, mic bool, micErr string) {
	devs, err := coreaudio.Devices()
	if err != nil {
		return false, false, err.Error()
	}
	choice, _ := chooseMic(devs)
	switch choice {
	case micNoBlackHole:
		return false, false, ""
	case micOurs, micTheirs:
		return true, true, ""
	}
	e.micMu.Lock()
	defer e.micMu.Unlock()
	if e.micRemoved {
		return true, false, "已移除，重新打开遥指后会再创建"
	}
	if !e.micTried.IsZero() && time.Since(e.micTried) < 30*time.Second {
		return true, false, "创建失败，稍后重试"
	}
	d, err := coreaudio.CreateAggregate(MicName, MicUID, []string{BlackHoleUID})
	if err != nil {
		e.micTried = time.Now()
		e.logf("创建 %s 失败：%v", MicName, err)
		return true, false, "创建失败，稍后重试"
	}
	e.micTried = time.Time{}
	e.logf("已创建 %s（%s，输入 %d 声道）", d.Name, d.UID, d.InputChannels)
	return true, true, ""
}

// RemoveMic removes the yaozhi-mic Yaozhi made (not one the user made) and
// stops making it again until the next launch. For leaving Yaozhi behind.
func (e *Engine) RemoveMic() error {
	e.micMu.Lock()
	e.micRemoved = true
	e.micMu.Unlock()
	err := coreaudio.DestroyAggregate(MicUID)
	if err == nil {
		e.logf("已移除 %s", MicName)
	}
	e.Recheck()
	return err
}

// voiceLoop starts the voice session once BlackHole is installed.
func (e *Engine) voiceLoop(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		if _, err := coreaudio.DeviceByUID(BlackHoleUID); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
	player, err := coreaudio.NewPlayer(BlackHoleUID, device.SampleRate)
	if err != nil {
		e.logf("打开 BlackHole 失败：%v", err)
		return
	}
	defer player.Close()
	defer func() {
		if err := macinput.ReleaseFnIfPressed(); err != nil {
			e.logf("松开 Fn 失败：%v", err)
		}
	}()
	mic := voice.NewVirtualMic(e.opts.Preset, player, macinput.SetFn, e.logf)
	s := voice.NewSession(mic, voice.Config{Logf: e.logf, OnEvent: e.sessionEvent})
	e.session.Store(s)
	e.logf("语音输入已就绪：%s", e.opts.Preset.Name)
	s.Run(ctx)
	e.session.Store(nil)
}
