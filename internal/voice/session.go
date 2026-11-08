// SPDX-License-Identifier: GPL-3.0-only

// Package voice turns the voice events of a device into input for a speech
// input method.
//
// Session holds the logic every input method shares: buffering the start of
// the speech until the input method listens, ignoring accidental presses,
// fading out the last frame (which may carry the key's click), finishing a
// stream before starting the next one, and ending cleanly when the device
// disconnects mid-stream. What differs between input methods sits behind the
// InputMethod interface; VirtualMic implements it for input methods that
// listen to a microphone and are started by a shortcut (Typeless, Handy...),
// configured by a Preset.
//
// The flow follows remote-mic-app's VoiceFnTapSessionController.swift
// (GPL-3.0-only): buffer pre-roll, tap, release the pre-roll, and tap again
// only after the audio has drained.
package voice

import (
	"context"
	"time"

	"github.com/zJiaJun/yaozhi/internal/device"
)

// InputMethod is where a voice stream goes. Session calls it from a single
// goroutine, in the order Begin, Write..., End, never overlapping streams.
type InputMethod interface {
	// Begin starts the input method. It must not block: it calls done (from
	// any goroutine) once the input method listens, or with the reason it
	// could not start.
	Begin(done func(error))
	// Write passes audio (device.SampleRate, mono), in order and in real
	// time. Called only between a successful Begin and End.
	Write(pcm []int16)
	// End is called after the last Write. It must not block: it calls done
	// once the input method has been told to finish, after all audio was
	// consumed.
	End(done func(error))
	// Abort stops at once, whatever is in progress. Called on shutdown; it
	// may block briefly.
	Abort()
}

// Config tunes a Session. Zero fields take the defaults.
type Config struct {
	// MinAudio is how much audio a stream needs before the input method is
	// started; a press that ends with less is taken as accidental and
	// dropped. Default 150 ms.
	MinAudio time.Duration
	// MaxBuffer bounds the audio held while waiting for the input method
	// or for the previous stream to finish. Default 30 s.
	MaxBuffer time.Duration
	// Logf receives diagnostics (never audio). nil discards them.
	Logf func(format string, args ...any)
	// OnEvent, if set, hears what happened to each stream, for the UI. It
	// is called on the session's goroutine and must not block.
	OnEvent func(Event)
}

// EventKind says what happened to a stream.
type EventKind int

const (
	// Listening: the input method started and gets the audio.
	Listening EventKind = iota + 1
	// Finished: the input method was told to finish; the text is on its way.
	Finished
	// Dropped: the press was too short and was ignored.
	Dropped
	// Failed: the input method could not be started (Err says why).
	Failed
)

// Event reports a step of a stream.
type Event struct {
	Kind EventKind
	// Audio is how much speech the stream had (Finished, Dropped).
	Audio time.Duration
	Err   error
}

// Session feeds voice events from a device into an InputMethod.
type Session struct {
	m  *machine
	in chan any
}

// NewSession returns a Session that drives im.
func NewSession(im InputMethod, cfg Config) *Session {
	s := &Session{in: make(chan any, 4096)}
	s.m = newMachine(im, cfg, func(msg any) { s.in <- msg })
	return s
}

// Handle passes a device event to the session. It is safe to call from any
// goroutine; events must arrive in device order.
func (s *Session) Handle(e device.Event) {
	switch e.(type) {
	case device.VoiceStarted, device.VoiceAudio, device.VoiceEnded:
		s.in <- e
	}
}

// Run processes events until ctx is cancelled, then aborts whatever stream
// is in progress so the input method is not left listening.
func (s *Session) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.m.shutdown()
			return
		case msg := <-s.in:
			s.m.handle(msg)
		}
	}
}

type phase int

const (
	phaseIdle       phase = iota
	phaseCollecting       // stream started, not enough audio yet
	phaseBeginning        // InputMethod.Begin in progress; audio is buffered
	phaseActive           // audio goes straight to the input method
	phaseEnding           // InputMethod.End in progress
)

// begun and finished are the completions of Begin and End, tagged with the
// stream they belong to.
type begun struct {
	gen int
	err error
}

type finished struct {
	gen int
	err error
}

// machine is the session state. It runs on one goroutine; completions from
// the input method come back through send, never by direct calls.
type machine struct {
	im   InputMethod
	send func(any)
	logf func(string, ...any)
	note func(Event)
	now  func() time.Time

	minSamples int
	maxSamples int

	phase     phase
	gen       int
	ended     bool    // the device ended the current stream
	held      []int16 // latest chunk, written last with a fade-out
	buf       []int16 // audio waiting for Begin to finish
	received  int     // samples of the current stream
	dropped   int     // samples dropped because buf was full
	startedAt time.Time
	endedAt   time.Time

	// backlog holds the events of the next stream(s) while the current one
	// is still finishing, replayed when it is done.
	backlog        []device.Event
	backlogSamples int
}

func newMachine(im InputMethod, cfg Config, send func(any)) *machine {
	if cfg.MinAudio == 0 {
		cfg.MinAudio = 150 * time.Millisecond
	}
	if cfg.MaxBuffer == 0 {
		cfg.MaxBuffer = 30 * time.Second
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.OnEvent == nil {
		cfg.OnEvent = func(Event) {}
	}
	return &machine{
		im:         im,
		send:       send,
		logf:       cfg.Logf,
		note:       cfg.OnEvent,
		now:        time.Now,
		minSamples: samples(cfg.MinAudio),
		maxSamples: samples(cfg.MaxBuffer),
	}
}

func samples(d time.Duration) int { return int(d * device.SampleRate / time.Second) }

func ms(n int) int { return n * 1000 / device.SampleRate }

func dur(n int) time.Duration { return time.Duration(n) * time.Second / device.SampleRate }

func (m *machine) handle(msg any) {
	switch msg := msg.(type) {
	case device.Event:
		m.event(msg)
	case begun:
		m.begun(msg)
	case finished:
		m.finished(msg)
	}
}

func (m *machine) event(e device.Event) {
	// Once the device has ended this stream, everything belongs to the next
	// one and waits until this one is done.
	if m.phase != phaseIdle && m.ended {
		m.queue(e)
		return
	}
	switch e := e.(type) {
	case device.VoiceStarted:
		if m.phase == phaseIdle {
			m.start()
		}
	case device.VoiceAudio:
		if m.phase != phaseIdle {
			m.audio(e.PCM)
		}
	case device.VoiceEnded:
		if m.phase != phaseIdle {
			m.end(e.Interrupted)
		}
	}
}

// queue queues an event of the next stream. Audio beyond MaxBuffer is
// dropped; events that do not belong to a started stream are ignored.
func (m *machine) queue(e device.Event) {
	switch e := e.(type) {
	case device.VoiceStarted:
	case device.VoiceAudio:
		if len(m.backlog) == 0 {
			return
		}
		if m.backlogSamples+len(e.PCM) > m.maxSamples {
			m.dropped += len(e.PCM)
			return
		}
		m.backlogSamples += len(e.PCM)
	case device.VoiceEnded:
		if len(m.backlog) == 0 {
			return
		}
	default:
		return
	}
	m.backlog = append(m.backlog, e)
}

func (m *machine) start() {
	m.gen++
	m.phase = phaseCollecting
	m.ended = false
	m.held, m.buf = nil, nil
	m.received, m.dropped = 0, 0
	m.startedAt = m.now()
}

func (m *machine) audio(pcm []int16) {
	m.received += len(pcm)
	if m.held != nil {
		m.forward(m.held)
	}
	m.held = pcm
	if m.phase == phaseCollecting && m.received >= m.minSamples {
		m.begin()
	}
}

// forward passes audio on, or buffers it until the input method listens.
func (m *machine) forward(pcm []int16) {
	if m.phase == phaseActive {
		m.im.Write(pcm)
		return
	}
	if len(m.buf)+len(pcm) > m.maxSamples {
		m.dropped += len(pcm)
		return
	}
	m.buf = append(m.buf, pcm...)
}

func (m *machine) begin() {
	m.phase = phaseBeginning
	gen := m.gen
	m.logf("语音开始，启动输入法")
	m.im.Begin(func(err error) { m.send(begun{gen, err}) })
}

func (m *machine) begun(b begun) {
	if b.gen != m.gen || m.phase != phaseBeginning {
		return
	}
	if b.err != nil {
		m.logf("启动输入法失败，丢弃这段语音：%v", b.err)
		m.note(Event{Kind: Failed, Err: b.err})
		m.idle()
		return
	}
	m.phase = phaseActive
	m.note(Event{Kind: Listening})
	m.logf("输入法已开始，放出缓存的 %d ms 语音（按下后 %d ms）", ms(len(m.buf)), m.now().Sub(m.startedAt).Milliseconds())
	if len(m.buf) > 0 {
		m.im.Write(m.buf)
		m.buf = nil
	}
	if m.ended {
		m.finish()
	}
}

func (m *machine) end(interrupted bool) {
	m.ended = true
	m.endedAt = m.now()
	if interrupted {
		m.logf("设备断开，提前结束这段语音")
	}
	switch m.phase {
	case phaseCollecting:
		m.logf("按键太短（%d ms 语音），当作误触", ms(m.received))
		m.note(Event{Kind: Dropped, Audio: dur(m.received)})
		m.idle()
	case phaseActive:
		m.finish()
	}
	// phaseBeginning: begun finishes once the input method listens.
}

// finish writes the held chunk with a fade-out and ends the input method.
func (m *machine) finish() {
	if m.held != nil {
		m.im.Write(fadeOut(m.held))
		m.held = nil
	}
	if m.dropped > 0 {
		m.logf("缓冲区满，丢弃了 %d ms 语音", ms(m.dropped))
	}
	m.phase = phaseEnding
	gen := m.gen
	m.im.End(func(err error) { m.send(finished{gen, err}) })
}

func (m *machine) finished(f finished) {
	if f.gen != m.gen || m.phase != phaseEnding {
		return
	}
	if f.err != nil {
		m.logf("结束输入法出错：%v", f.err)
	}
	m.logf("语音结束：%d ms 语音，松键 %d ms 后输入法结束", ms(m.received), m.now().Sub(m.endedAt).Milliseconds())
	m.note(Event{Kind: Finished, Audio: dur(m.received)})
	m.idle()
}

// idle forgets the current stream and replays queued events of the next.
func (m *machine) idle() {
	m.phase = phaseIdle
	m.ended = false
	m.held, m.buf = nil, nil
	backlog := m.backlog
	m.backlog, m.backlogSamples = nil, 0
	for _, e := range backlog {
		m.event(e)
	}
}

func (m *machine) shutdown() {
	switch m.phase {
	case phaseBeginning, phaseActive, phaseEnding:
		m.im.Abort()
	}
	m.gen++
	m.phase = phaseIdle
	m.backlog = nil
}

// fadeOut returns pcm ramped linearly down to silence.
func fadeOut(pcm []int16) []int16 {
	out := make([]int16, len(pcm))
	n := len(pcm)
	for i, v := range pcm {
		out[i] = int16(int(v) * (n - 1 - i) / n)
	}
	return out
}
