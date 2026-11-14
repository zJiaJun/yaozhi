// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

// Package app runs Yaozhi without its UI: the device, the voice session, the
// blocking of the voice key, and the checks of what Yaozhi needs (permissions,
// BlackHole, yaozhi-mic). The UI reads State and is told when it changes.
package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zJiaJun/yaozhi/internal/coreaudio"
	"github.com/zJiaJun/yaozhi/internal/device"
	"github.com/zJiaJun/yaozhi/internal/device/atvvble"
	"github.com/zJiaJun/yaozhi/internal/macapp"
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
	// AXRevoked: Accessibility was granted while Yaozhi ran, and then
	// turned off. Voice input is paused until it is granted again.
	AXRevoked bool
	// InputInstalled and InputRunning: the input method's app is on this
	// Mac, and running. Both are true for a preset that does not name its
	// app, which cannot be checked.
	InputInstalled bool
	InputRunning   bool
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

	Stats Stats // today's numbers, kept across launches

	// What the user chose (Settings), and what was once seen to work:
	// text arriving from the input method, and a remote connected.
	Paused     bool
	GainDB     float64
	TextSeen   bool
	RemoteSeen bool

	// NeedsStart: the user was never asked about Bluetooth, and Yaozhi
	// waits for Begin before it makes the system ask.
	NeedsStart bool

	// What the connected remote told about itself. Battery is its charge
	// in percent, meaningful while BatteryKnown.
	Model        string
	Battery      int
	BatteryKnown bool
}

// Options configures an Engine.
type Options struct {
	Devices   []device.Descriptor
	Preset    voice.Preset
	AcceptAny bool
	// StatsFile keeps Stats across launches; "" keeps them in memory only.
	StatsFile string
	// SettingsFile keeps Settings across launches, likewise.
	SettingsFile string
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
	begin   chan struct{} // closed by Begin
	blocker *keyBlocker

	paused atomic.Bool   // Settings.Paused, read on the device's I/O path
	gain   atomic.Uint64 // Settings.GainDB, a float64's bits
	open   atomic.Bool   // a voice stream is being passed on to the session

	// The loudness of the latest audio frame, for the UI's meter: a
	// float32's bits, and when it came (UnixNano).
	level   atomic.Uint32
	levelAt atomic.Int64

	// Used by checkLoop only: the latest failed attempt at making
	// yaozhi-mic, to retry slowly.
	micTried time.Time
}

// New returns an engine; Run starts it.
func New(o Options) *Engine {
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	e := &Engine{opts: o, logf: logf, recheck: make(chan struct{}, 1), begin: make(chan struct{})}
	e.st.Stats = loadStats(o.StatsFile).on(time.Now())
	set := loadSettings(o.SettingsFile)
	e.st.Paused, e.st.GainDB, e.st.TextSeen, e.st.RemoteSeen = set.Paused, set.GainDB, set.TextSeen, set.RemoteSeen

	e.paused.Store(set.Paused)
	e.gain.Store(math.Float64bits(set.GainDB))
	e.blocker = newKeyBlocker(!set.Paused, logf)
	return e
}

// SetPaused turns voice input off (true) or back on. Off, the voice key
// goes back to the system and nothing reaches the input method; a stream
// in progress ends at once.
func (e *Engine) SetPaused(paused bool) {
	if e.paused.Swap(paused) == paused {
		return
	}
	e.blocker.enable(!paused)
	if paused && e.open.Swap(false) {
		end := device.VoiceEnded{Interrupted: true}
		if s := e.session.Load(); s != nil {
			s.Handle(end)
		}
		e.deviceEvent(end)
	}
	e.update(func(s *State) { s.Paused = paused })
	e.logf("语音输入%s", map[bool]string{true: "已暂停", false: "已启用"}[paused])
	e.saveSettings()
}

// SetGain sets how much louder the voice is made, in dB from 0 to
// voice.MaxGainDB.
func (e *Engine) SetGain(db float64) {
	db = Settings{GainDB: db}.valid().GainDB
	if math.Float64frombits(e.gain.Swap(math.Float64bits(db))) == db {
		return
	}
	e.update(func(s *State) { s.GainDB = db })
	e.saveSettings()
}

// Begin lets Yaozhi use Bluetooth for the first time, which makes the
// system ask the user: call it once the user was told what for. It does
// nothing when the user was asked before.
func (e *Engine) Begin() {
	e.mu.Lock()
	select {
	case <-e.begin:
	default:
		close(e.begin)
	}
	e.mu.Unlock()
	e.update(func(s *State) { s.NeedsStart = false })
}

// NoteText records that text arrived from the input method where the UI
// could see it. The UI tells only that, not the text.
func (e *Engine) NoteText() {
	if e.State().TextSeen {
		return
	}
	e.update(func(s *State) { s.TextSeen = true })
	e.logf("第一次在遥指里看到出字")
	e.saveSettings()
}

func (e *Engine) saveSettings() {
	st := e.State()
	set := Settings{Paused: st.Paused, GainDB: st.GainDB, TextSeen: st.TextSeen, RemoteSeen: st.RemoteSeen}
	if err := saveJSON(e.opts.SettingsFile, set); err != nil {
		e.logf("保存设置失败：%v", err)
	}
}

// Diagnostics describes what Yaozhi sees, for a report: its state and the
// audio devices, never audio or text.
func (e *Engine) Diagnostics() string {
	st := e.State()
	var b strings.Builder
	yes := map[bool]string{true: "是", false: "否"}
	fmt.Fprintf(&b, "输入法预设：%s（%s，%s）\n", e.opts.Preset.Name, e.opts.Preset.Trigger.Key, e.opts.Preset.Trigger.Mode)
	fmt.Fprintf(&b, "语音输入：暂停=%s 增益=%+.0f dB\n", yes[st.Paused], st.GainDB)
	fmt.Fprintf(&b, "遥控器：%s 名称=%q 型号=%q", st.Conn, st.Device, st.Model)
	if st.BatteryKnown {
		fmt.Fprintf(&b, " 电量=%d%%", st.Battery)
	}
	if st.ConnErr != "" {
		fmt.Fprintf(&b, " 上次断开=%s", st.ConnErr)
	}
	r := st.Ready
	fmt.Fprintf(&b, "\n权限：蓝牙=%s 辅助功能=%s（被关闭过=%s）\n",
		map[Perm]string{PermUnknown: "未知", PermGranted: "已允许", PermDenied: "已拒绝"}[r.Bluetooth], yes[r.Accessibility], yes[r.AXRevoked])
	fmt.Fprintf(&b, "虚拟麦克风：BlackHole=%s %s=%s %s\n", yes[r.BlackHole], MicName, yes[r.Mic], r.MicErr)
	fmt.Fprintf(&b, "输入法 App：%s 已安装=%s 运行中=%s；在遥指里出过字=%s\n", e.opts.Preset.App.BundleID, yes[r.InputInstalled], yes[r.InputRunning], yes[st.TextSeen])
	fmt.Fprintf(&b, "统计：今天 %d 次，平均 %.1f 秒，松开后 %d ms；累计 %d 次\n",
		st.Stats.Today, st.Stats.AvgAudio().Seconds(), st.Stats.AvgLatency().Milliseconds(), st.Stats.Total)
	b.WriteString("音频设备：\n")
	devs, err := coreaudio.Devices()
	if err != nil {
		fmt.Fprintf(&b, "  读取失败：%v\n", err)
	}
	for _, d := range devs {
		fmt.Fprintf(&b, "  %s（%s）输入 %d 输出 %d 声道\n", d.Name, d.UID, d.InputChannels, d.OutputChannels)
	}
	return b.String()
}

// Level is how loud the voice is now, from 0 (silence, or no voice
// stream) to 1, for a meter. It is read often: it takes no lock.
func (e *Engine) Level() float32 {
	if time.Since(time.Unix(0, e.levelAt.Load())) > 150*time.Millisecond {
		return 0
	}
	return math.Float32frombits(e.level.Load())
}

// noteLevel measures a frame of audio: its RMS in dBFS, from -55 dB to
// -10 dB mapped onto 0 to 1. Only the loudness is kept, never the audio.
func (e *Engine) noteLevel(pcm []int16) {
	if len(pcm) == 0 {
		return
	}
	var sum float64
	for _, v := range pcm {
		f := float64(v) / 32768
		sum += f * f
	}
	db := 10 * math.Log10(sum/float64(len(pcm))+1e-12)
	l := float32(math.Max(0, math.Min(1, (db+55)/45)))
	e.level.Store(math.Float32bits(l))
	e.levelAt.Store(time.Now().UnixNano())
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
		if f != nil {
			f(s) // sees the phase before
		}
		s.Voice, s.VoiceSince = p, time.Now()
	})
}

// Run runs until ctx is cancelled. Missing permissions or devices do not
// stop it: they show in State, and it carries on once they are there.
func (e *Engine) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Go(func() { e.blocker.run(ctx) })
	wg.Go(func() { e.checkLoop(ctx) })
	wg.Go(func() { e.voiceLoop(ctx) })
	wg.Go(func() { e.idleLoop(ctx) })

	// The first use of Bluetooth makes the system ask the user. On the
	// very first launch that waits until the user was told what for.
	if !atvvble.Asked() {
		e.update(func(s *State) { s.NeedsStart = true })
		e.logf("还没问过蓝牙权限，等用户点继续")
		select {
		case <-e.begin:
		case <-ctx.Done():
			return nil
		}
	}

	a := atvvble.New(e.opts.Devices)
	a.AcceptAny = e.opts.AcceptAny
	a.Logf = e.logf
	err := a.Run(ctx, func(ev device.Event) {
		// Only streams that started while voice input could work are
		// passed on, whole: pausing ends the one in progress (SetPaused),
		// and one in progress when it resumes is left alone. Without the
		// permission to press the input method's shortcut a stream has
		// nowhere to go, and the UI already says what is missing: showing
		// it listening would promise text that cannot come.
		switch ev := ev.(type) {
		case device.VoiceStarted:
			if e.paused.Load() || e.session.Load() == nil || !e.State().Ready.Accessibility {
				return
			}
			e.open.Store(true)
		case device.VoiceAudio:
			if !e.open.Load() {
				return
			}
			voice.Gain(ev.PCM, math.Float64frombits(e.gain.Load()))
			e.noteLevel(ev.PCM)
		case device.VoiceEnded:
			if !e.open.Swap(false) {
				return
			}
		}
		if s := e.session.Load(); s != nil {
			s.Handle(ev)
		}
		e.deviceEvent(ev)
	})
	if errors.Is(err, atvvble.ErrUnauthorized) {
		e.update(func(s *State) { s.Ready.Bluetooth = PermDenied })
		e.logf("没有蓝牙权限")
		<-ctx.Done()
		return nil
	}
	return err
}

func (e *Engine) deviceEvent(ev device.Event) {
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
			if ev.State == device.StateSearching || ev.State == device.StateUnavailable {
				s.Battery, s.BatteryKnown = 0, false // as of the last connection
			}
		})
		if ev.State == device.StateReady {
			e.blocker.block(ev.Device)
			if !e.State().RemoteSeen {
				e.update(func(s *State) { s.RemoteSeen = true })
				e.saveSettings()
			}
		}
	case device.InfoChanged:
		e.update(func(s *State) {
			if ev.Model != "" {
				s.Model = ev.Model
			}
			if ev.Battery >= 0 {
				s.Battery, s.BatteryKnown = ev.Battery, true
			}
		})
	case device.VoiceStarted:
		e.setVoice(VoiceListening, func(s *State) { s.Problem = "" })
	case device.VoiceEnded:
		// Only a stream still listening goes on to end: one that already
		// failed stays failed, and no session event would ever take it
		// out of VoiceEnding.
		e.update(func(s *State) {
			if s.Voice == VoiceListening {
				s.Voice, s.VoiceSince = VoiceEnding, time.Now()
			}
		})
	}
}

// sessionEvent follows the session, which knows whether the press counted.
func (e *Engine) sessionEvent(ev voice.Event) {
	switch ev.Kind {
	case voice.Dropped:
		e.setVoice(VoiceIdle, nil)
	case voice.Finished:
		var stats Stats
		e.setVoice(VoiceDone, func(s *State) {
			var latency time.Duration
			if s.Voice == VoiceEnding {
				latency = time.Since(s.VoiceSince)
			}
			s.LastAudio = ev.Audio
			s.Count++
			s.Stats = s.Stats.add(time.Now(), ev.Audio, latency)
			stats = s.Stats
		})
		if err := saveStats(e.opts.StatsFile, stats); err != nil {
			e.logf("保存统计失败：%v", err)
		}
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
			if today := st.Stats.on(time.Now()); today != st.Stats {
				e.update(func(s *State) { s.Stats = s.Stats.on(time.Now()) }) // past midnight
			}
			// VoiceEnding ends with the session's word; should that never
			// come, it does not stay for ever.
			hold := map[VoicePhase]time.Duration{VoiceDone: 2600 * time.Millisecond, VoiceFailed: 5 * time.Second, VoiceEnding: 8 * time.Second}[st.Voice]
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
	first, wasAX := true, false
	for {
		ax, _ := macinput.CanPostEvents()
		if first || ax != wasAX {
			e.logf("辅助功能：%s", map[bool]string{true: "已允许", false: "未允许"}[ax])
			first, wasAX = false, ax
		}
		st := e.State().Ready
		bh, mic, micErr := st.BlackHole, st.Mic, st.MicErr
		if checkDevices || !(bh && mic) || time.Since(lastDevices) > time.Minute {
			bh, mic, micErr = e.checkMic()
			lastDevices = time.Now()
		}
		// The input method's app: whether it runs is asked every time,
		// whether it is installed only while it does not.
		installed, running := true, true
		if id := e.opts.Preset.App.BundleID; id != "" {
			running = macapp.Running(id)
			installed = running || macapp.Installed(id)
		}
		e.update(func(s *State) {
			switch {
			case ax:
				s.Ready.AXRevoked = false
			case s.Ready.Accessibility:
				s.Ready.AXRevoked = true // granted a moment ago
			}
			s.Ready.Accessibility, s.Ready.BlackHole, s.Ready.Mic, s.Ready.MicErr = ax, bh, mic, micErr
			s.Ready.InputInstalled, s.Ready.InputRunning = installed, running
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
