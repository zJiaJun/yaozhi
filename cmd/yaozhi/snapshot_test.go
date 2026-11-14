// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/zJiaJun/yaozhi/internal/app"
	"github.com/zJiaJun/yaozhi/internal/device"
	"github.com/zJiaJun/yaozhi/internal/voice"
)

// testShell is a shell showing st, without an engine running or a window.
func testShell(st app.State) *shell {
	p := voice.BuiltinPresets()[0]
	return &shell{eng: app.New(app.Options{Preset: p}), preset: p, st: st, page: pageOverview, selKey: "voice"}
}

// readyState is everything in place and the remote connected.
func readyState() app.State {
	return app.State{
		Conn: device.StateReady, Device: "小米蓝牙遥控器 2 Pro", Model: "RC003", Battery: 82, BatteryKnown: true,
		Ready:      app.Readiness{Bluetooth: app.PermGranted, Accessibility: true, BlackHole: true, Mic: true, InputInstalled: true, InputRunning: true},
		TextSeen:   true,
		RemoteSeen: true,
		Stats:      app.Stats{Today: 12, Audio: 12 * 3400 * time.Millisecond, Latency: 12 * 430 * time.Millisecond, Total: 40},
	}
}

// TestSnapshots draws the window's pages in their main states into the directory $YAOZHI_SNAP, to look at. Without it, it
// only checks that every one of them draws.
func TestSnapshots(t *testing.T) {
	out := os.Getenv("YAOZHI_SNAP")
	listening := readyState()
	listening.Voice, listening.VoiceSince = app.VoiceListening, time.Now().Add(-2300*time.Millisecond)
	done := readyState()
	done.Voice, done.LastAudio = app.VoiceDone, 2300*time.Millisecond
	blocked := readyState()
	blocked.Ready.Accessibility = false
	blocked.Stats = app.Stats{}
	paused := readyState()
	paused.Paused = true
	asleep := readyState()
	asleep.Conn, asleep.BatteryKnown = device.StateSearching, false
	gain := readyState()
	gain.GainDB = 6
	// The first launch on a Mac with nothing set up, step by step.
	setup := func(pretend string) app.State {
		sh := testShell(readyState())
		sh.pretend = parsePretend(pretend)
		return sh.pretended(sh.st)
	}
	step1, step2 := setup("all"), setup("blackhole,typeless,ax,text")
	step3, step3off := setup("typeless,ax,text"), setup("typeless-off,ax,text")
	step4, step5 := setup("ax,text"), setup("text")

	for _, tc := range []struct {
		name string
		st   app.State
		page string
		dark bool
		open bool
		try  tryBox

		finished bool
	}{
		{name: "overview-ready", st: readyState()},
		{name: "overview-listening", st: listening},
		{name: "overview-done-dark", st: done, dark: true},
		{name: "overview-blocked-dark", st: blocked, dark: true, open: true},
		{name: "overview-paused", st: paused},
		{name: "overview-asleep", st: asleep},
		{name: "setup-1-remote", st: step1},
		{name: "setup-2-blackhole", st: step2},
		{name: "setup-3-typeless-dark", st: step3, dark: true},
		{name: "setup-3-typeless-off", st: step3off},
		{name: "setup-4-access", st: step4},
		{name: "setup-5-try", st: step5},
		{name: "setup-5-miss-dark", st: step5, dark: true, try: tryBox{armed: true, result: tryMiss, resultAt: time.Now()}},
		{name: "setup-done", st: readyState(), finished: true, try: tryBox{text: "今天天气不错。", armed: true, result: tryOK, audio: 2300 * time.Millisecond}},
		{name: "voice", st: gain, page: pageVoice},
		{name: "voice-dark", st: gain, page: pageVoice, dark: true},
		{name: "keys", st: readyState(), page: pageKeys},
		{name: "device", st: readyState(), page: pageDevice},
		{name: "device-blocked-dark", st: blocked, page: pageDevice, dark: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testShell(tc.st)
			if tc.page != "" {
				s.page = tc.page
			}
			s.readyOpen = tc.open
			s.try, s.finished = tc.try, tc.finished
			tt := ui.NewTesterWith(s.mainView, 1040, 660, ui.TesterOptions{Dark: tc.dark})
			tt.SetScale(2)
			tt.Advance(700 * time.Millisecond)
			img := tt.Image()
			if img == nil || img.Bounds().Empty() {
				t.Fatal("nothing drawn")
			}
			if out == "" {
				return
			}
			f, err := os.Create(filepath.Join(out, tc.name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestMenu checks what the menu bar menu offers as things stand.
func TestMenu(t *testing.T) {
	labels := func(s *shell) string {
		var b strings.Builder
		for _, it := range s.menu() {
			b.WriteString(it.Label + "|")
		}
		return b.String()
	}
	ready := labels(testShell(readyState()))
	for _, want := range []string{"小米蓝牙遥控器 2 Pro · 电量 82%|", "|暂停语音输入|", "|打开遥指…|", "|关于遥指|", "|退出遥指|"} {
		if !strings.Contains(ready, want) {
			t.Errorf("menu %q lacks %q", ready, want)
		}
	}
	paused := readyState()
	paused.Paused = true
	if got := labels(testShell(paused)); strings.Count(got, "启用") != 1 || !strings.Contains(got, "|启用语音输入|") || strings.Contains(got, "暂停语音输入|") {
		t.Errorf("paused menu: %q", got)
	}
	blocked := readyState()
	blocked.Ready.Accessibility = false
	if got := labels(testShell(blocked)); !strings.Contains(got, "|去授权…|") {
		t.Errorf("menu without Accessibility offers no way to grant it: %q", got)
	}
}

// TestSteps checks which step the first launch is at as things get done.
func TestSteps(t *testing.T) {
	for _, tc := range []struct {
		pretend string
		left    int
		cur     string
		action  string
		allSet  bool
	}{
		{"all", 5, stepRemote, "继续", false},
		{"remote,blackhole,typeless,ax,text", 5, stepRemote, "打开蓝牙设置", false},
		{"blackhole,typeless,ax,text", 4, stepBlackHole, "复制命令", false},
		{"typeless,ax,text", 3, stepInput, "复制命令", false},
		{"typeless-off,ax,text", 3, stepInput, "打开 Typeless", false},
		{"ax,text", 2, stepAccess, "去授权", false},
		{"text", 1, stepTry, "", true},
		// Done out of order: what is done is ticked, and the first thing
		// missing is the one to take.
		{"blackhole", 1, stepBlackHole, "复制命令", false},
		{"", 0, "", "", true},
	} {
		s := testShell(readyState())
		s.pretend = parsePretend(tc.pretend)
		s.st = s.pretended(s.st)
		x := s.status()
		cur := ""
		if x.cur < len(x.steps) {
			cur = x.steps[x.cur].key
		}
		if x.left != tc.left || cur != tc.cur || x.action != tc.action || x.allSet != tc.allSet || x.setup != (tc.left > 0) {
			t.Errorf("pretending %q: %d left, at %q, action %q, allSet %v, setup %v; want %d, %q, %q, %v",
				tc.pretend, x.left, cur, x.action, x.allSet, x.setup, tc.left, tc.cur, tc.action, tc.allSet)
		}
	}
	// The last step done in this run: the steps stay, all ticked, until
	// the user moves on, also while the next sentence is being said.
	s := testShell(readyState())
	s.finished = true
	if x := s.status(); !x.done || x.setup || x.head != "准备好了。" || x.action != doneAction {
		t.Errorf("just finished: %+v", x)
	}
	s.st.Voice = app.VoiceListening
	if x := s.status(); !x.done || x.head != "正在听" {
		t.Errorf("speaking after finishing: done %v, head %q", x.done, x.head)
	}
	s.st.Voice = app.VoiceIdle
	s.status().do()
	if x := s.status(); x.done || x.head != "按住，\n说话。" {
		t.Errorf("after moving on: done %v, head %q", x.done, x.head)
	}

	// A remote that was connected before and sleeps now is not a step to
	// take again.
	asleep := readyState()
	asleep.Conn = device.StateSearching
	if x := testShell(asleep).status(); x.setup {
		t.Errorf("a sleeping remote brings the setup back: %+v", x.steps[0])
	}
}
