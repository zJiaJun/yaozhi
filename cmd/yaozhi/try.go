// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/zJiaJun/yaozhi/internal/app"
)

// tryBox is the "试一试" of the last step of the first launch: a text box
// of Yaozhi's own that has the keyboard while the window is in front, so
// that what is said lands in it and the user sees voice input work without
// going to another app. It is also the one place where Yaozhi can tell
// whether text really came: it knows what the box held when the voice
// started, and sees it change. Once that happened the box has done its
// work, and the overview goes on without it.
//
// The text stays in memory, in this box: it is not saved, not logged, and
// gone when Yaozhi quits.
type tryBox struct {
	text    string
	focused bool // the box had the keyboard in the latest frame

	// The latest voice stream, if it began with the keyboard in the box.
	armed    bool
	base     string        // what the box held then
	doneAt   time.Time     // when the stream was handed to the input method
	audio    time.Duration // how much was said
	result   tryResult
	resultAt time.Time
}

type tryResult int

const (
	tryNone tryResult = iota
	tryOK             // the text in the box changed
	tryMiss           // it did not, tryWait after the stream was handed over
)

const (
	// tryWait is how long the input method gets to write its text. Typeless
	// takes about a second; a slow network takes more, and text that comes
	// later still counts.
	tryWait = 3 * time.Second
)

// follow takes a change of the engine's state: a stream starting with the
// keyboard in the box is watched, and one handed over starts the wait.
func (t *tryBox) follow(s *shell, old, st app.State) {
	switch {
	case st.Voice == app.VoiceListening && old.Voice != app.VoiceListening:
		*t = tryBox{text: t.text, focused: t.focused, armed: t.focused, base: t.text}
	case st.Voice == app.VoiceDone && old.Voice != app.VoiceDone && t.armed:
		t.doneAt, t.audio = time.Now(), st.LastAudio
		if s.win != nil { // look again once the wait is over
			time.AfterFunc(tryWait+100*time.Millisecond, func() { s.win.Update(func() {}) })
		}
	case st.Voice == app.VoiceFailed || (st.Voice == app.VoiceIdle && old.Voice == app.VoiceListening):
		t.armed = false // nothing was handed over
	}
}

// check looks at the box, each frame: text that changed since the stream
// began came from it, and none after the wait is a miss.
func (t *tryBox) check(s *shell) {
	if !t.armed {
		return
	}
	switch {
	case t.text != t.base:
		if t.result != tryOK {
			t.result, t.resultAt = tryOK, time.Now()
			s.eng.NoteText()
			s.unpretend("text")
		}
	case t.result == tryNone && !t.doneAt.IsZero() && time.Since(t.doneAt) >= tryWait:
		t.result, t.resultAt = tryMiss, time.Now()
	}
}

func (t *tryBox) clear() { *t = tryBox{focused: t.focused} }

// view draws the box: its label and what the latest try came to, the text,
// and the voice's level along its bottom.
func (t *tryBox) view(c *ui.Context, s *shell, p pal, x status, calm bool) {
	st := s.st
	listening := st.Voice == app.VoiceListening
	border := p.line
	switch {
	case listening:
		border = p.zhu
	case t.result == tryMiss:
		border = p.warn
	case t.focused:
		border = p.ink
	}
	hint := "按住语音键，说一句“今天天气不错”。"
	ui.Column(c).Margin(10, 0, 0, 0).Padding(9, 14, 8, 14).Radius(12).Background(p.card).Border(1, border).
		Transition(ui.ElementTransition{Colors: true}).Children(func() {
		ui.Row(c).Gap(8).Height(18).Children(func() {
			ui.Text(c, "试一试").FontSize(11).FontWeight(600).LetterSpacing(1.8).TextColor(p.ink3)
			ui.Spacer(c)
			note := func(col ui.Color, text string) {
				if col.A > 0 {
					dot(c, col, 6)
				}
				ui.Text(c, text).FontSize(11.5).TextColor(p.ink2).NoWrap()
			}
			switch {
			case listening:
				note(p.zhu, "正在听")
			case st.Voice == app.VoiceEnding:
				note(ui.Color{}, "收尾中…")
			case t.result == tryOK:
				note(p.ok, fmt.Sprintf("出字成功 · %.1f 秒", t.audio.Seconds()))
			case t.result == tryMiss:
				note(p.warn, "没有收到文字")
			case t.armed && !t.doneAt.IsZero():
				note(ui.Color{}, "等文字出现…")
			}
			if t.text != "" {
				b := ui.Box(c.Key("try-clear")).Padding(0, 7).Radius(5).Border(1, p.line).Cursor(ui.CursorPointer)
				if b.Hovered() {
					b.Background(p.line2)
				}
				b.Children(func() { ui.Text(c, "清空").FontSize(11.5).TextColor(p.ink2) })
				if b.Clicked() {
					t.clear()
				}
			}
		})
		caret := p.ink
		if listening {
			caret = p.zhu
		}
		in := ui.TextAreaBase(c.Key("try-text"), &t.text).Lines(1, 3).Margin(6, 0, 4, 0).
			FontSize(15).LineHeight(1.6).TextColor(p.ink).CaretColor(caret).
			Placeholder(hint).PlaceholderColor(p.ink3).Label("试一试")
		// The box keeps the keyboard while the window is in front: what is
		// said then should land here without a click first. Nothing else
		// on the page takes typing.
		if c.WindowActive() {
			in.Focus()
		}
		t.focused = in.Focused()
		ui.Box(c).Height(22).Draw(func(pt *ui.Painter, r ui.Rect) { s.drawWave(pt, r, p, p.line, listening, calm) })
	})
}
