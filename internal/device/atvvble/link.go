// SPDX-License-Identifier: GPL-3.0-only

// Package atvvble is the device adapter for remotes that send voice over
// ATVV (Android TV Voice over BLE), such as the Xiaomi Bluetooth Remote 2 Pro.
// Which remotes it accepts comes from device descriptors whose voice protocol
// is "atvv".
//
// The connection flow follows open-voice-bridge's XiaomiBluetoothBridge.swift
// (GPL-3.0-only): look up a remote the system already keeps connected (a
// paired remote stops advertising), subscribe to Audio and Control, request
// capabilities, then translate the protocol into device events.
package atvvble

import (
	"errors"

	"github.com/zJiaJun/yaozhi/internal/atvv"
	"github.com/zJiaJun/yaozhi/internal/device"
)

// link turns the ATVV notifications of one connection into device events.
// It holds no Bluetooth state, so it can be tested without a remote. A new
// link is made for every connection.
type link struct {
	sess  *atvv.Session
	emit  func(device.Event)
	write func(cmd []byte) // to the Transmit characteristic
	logf  func(format string, args ...any)
}

func newLink(emit func(device.Event), write func([]byte), logf func(string, ...any)) *link {
	return &link{sess: atvv.NewSession(nil), emit: emit, write: write, logf: logf}
}

// control handles a Control notification. ready is true when the
// capabilities were just accepted. A non-nil error means the remote cannot be
// used (it does not offer 16 kHz audio) and the connection should be dropped.
func (l *link) control(b []byte) (ready bool, err error) {
	wasStreaming := l.sess.Streaming()
	ev, err := l.sess.HandleControl(b)
	switch {
	case errors.Is(err, atvv.ErrUnsupportedRate):
		// The session has already stopped the stream; every VoiceStarted
		// still gets its VoiceEnded.
		if wasStreaming && !l.sess.Streaming() {
			l.emit(device.VoiceEnded{Interrupted: true})
		}
		return false, err
	case err != nil:
		l.logf("忽略控制消息 % x：%v", b, err)
		return false, nil
	}
	switch ev.Kind {
	case atvv.EventCapabilities:
		c := ev.Capabilities
		l.logf("ATVV 就绪：版本 %04x，%d Hz，帧长 %d，交互模式 %02x", c.Version, c.SampleRate, c.FrameSize, c.Interaction)
		return true, nil
	case atvv.EventMicButton:
		// Remotes in on-request mode only stream after the host opens the
		// mic. RC003 skips this and sends AUDIO_START straight away.
		l.write(l.sess.MicOpenCommand())
	case atvv.EventAudioStart:
		l.emit(device.VoiceStarted{})
	case atvv.EventAudioStop:
		l.emit(device.VoiceEnded{})
	}
	return false, nil
}

// audio handles an Audio notification.
func (l *link) audio(b []byte) {
	pcm, started, err := l.sess.HandleAudio(b)
	if err != nil {
		l.logf("忽略 %d 字节音频：%v", len(b), err)
		return
	}
	if started {
		// Audio raced ahead of AUDIO_START (or AUDIO_START was lost).
		l.emit(device.VoiceStarted{})
	}
	if len(pcm) > 0 {
		l.emit(device.VoiceAudio{PCM: pcm})
	}
}

// ready reports whether the capabilities have been accepted.
func (l *link) ready() bool { return l.sess.Ready() }

// end finishes a voice stream that is still open, as interrupted. closeMic
// also asks the remote to stop streaming, for when the host gives up on a
// connection that is still up.
func (l *link) end(closeMic bool) {
	if !l.sess.Streaming() {
		return
	}
	if closeMic {
		l.write(l.sess.MicCloseCommand())
	}
	// Feed a synthetic AUDIO_STOP so the session state matches.
	_, _ = l.sess.HandleControl([]byte{atvv.OpAudioStop})
	l.emit(device.VoiceEnded{Interrupted: true})
}
