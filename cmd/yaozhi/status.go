// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"

	"github.com/zJiaJun/yaozhi/internal/app"
	"github.com/zJiaJun/yaozhi/internal/device"
)

// act is something the user can do about a step: a button.
type act struct {
	label string
	do    func()
}

// step is one of the five things voice input needs, in the order a new
// user does them (docs/design/index.html, "冷启动"): the remote, the
// virtual sound card, the input method's app, the permission to press its
// shortcut, and a first sentence that comes out as text.
type step struct {
	key, title string
	done       bool
	state      string // how it stands, in a word or two
	tone       tone   // toneOK when done; else toneWarn, toneBad or toneBusy (waiting, nothing to do)
	body       string // what to do, shown while it is the step to take
	code       string // a command to run, if any
	acts       []act  // the first is the one to take
}

// The steps' keys.
const (
	stepRemote    = "remote"
	stepBlackHole = "blackhole"
	stepInput     = "input"
	stepAccess    = "ax"
	stepTry       = "try"
)

// status is what Yaozhi is doing, said once for every place that shows it:
// the overview's heading and steps, the status bar, the menu bar icon and
// its menu.
type status struct {
	tone tone
	busy bool // waiting: a spinner in place of the dot

	head, sub   string // the overview's heading and the line under it
	bar, detail string // the status bar: in bold, and what follows it
	action      string // a button, if the user can do something about it
	do          func()
	resume      bool // the action turns voice input back on

	steps  []step
	done   bool // the last of them was done just now: say so before moving on
	left   int  // how many are still to do
	cur    int  // the first of them, the one to take now; len(steps) when none
	setup  bool // some are: the overview shows the steps
	allSet bool // all but the first sentence are done: speaking can work
}

// What the overview says under the steps once all are done, and its button.
const (
	doneNote   = "以后随时按住语音键说话，文字会出现在光标所在的地方。关掉窗口后，遥指留在菜单栏里。"
	doneAction = "开始使用"
)

const (
	blackHoleCommand = "brew install blackhole-2ch"
	blackHoleURL     = "https://existential.audio/blackhole/"
	bluetoothPane    = "x-apple.systempreferences:com.apple.BluetoothSettings"
)

// open opens a URL in what the system has for it.
func open(url string) {
	if err := mygo.Shell.OpenExternal(url); err != nil {
		logf("打开 %s 失败：%v", url, err)
	}
}

// steps tells how each of the five things stands.
func (s *shell) steps() []step {
	st := s.st
	r := st.Ready
	name := s.preset.Name
	mic := app.MicName

	remote := step{key: stepRemote, title: "连接遥控器"}
	switch {
	case st.NeedsStart:
		remote.tone, remote.state = toneWarn, "等你点继续"
		remote.body = "遥指用蓝牙连接遥控器并接收语音。点“继续”后，macOS 会问是否允许遥指使用蓝牙。"
		remote.acts = []act{{"继续", func() { s.unpretend("bluetooth"); s.eng.Begin() }}}
	case r.Bluetooth == app.PermDenied:
		remote.tone, remote.state = toneBad, "蓝牙被拒绝"
		remote.body = "遥指没有蓝牙权限，连不上遥控器。到系统设置里允许后就能连接。"
		remote.acts = []act{{"去系统设置", func() { s.guide(permissionflow.Bluetooth) }}}
	case r.Bluetooth != app.PermGranted:
		remote.tone, remote.state = toneBusy, "等你允许"
		remote.body = "在系统弹出的询问里点“允许”。"
	case st.Conn == device.StateReady:
		remote.done, remote.state = true, "已连接"
		if st.BatteryKnown {
			remote.state = fmt.Sprintf("已连接 · 电量 %d%%", st.Battery)
		}
	case st.RemoteSeen: // paired, and asleep or away: not something to set up
		remote.done, remote.state = true, "已配对，按任意键唤醒"
	case st.Conn == device.StateUnavailable:
		remote.tone, remote.state = toneBad, "蓝牙已关闭"
		remote.body = "打开这台 Mac 的蓝牙，遥指会自动连上遥控器。"
		remote.acts = []act{{"打开蓝牙设置", func() { open(bluetoothPane) }}}
	case st.Conn == device.StateConnecting:
		remote.tone, remote.state = toneBusy, "正在连接…"
		remote.body = "找到遥控器了，正在接通语音通道。"
	default:
		remote.tone, remote.state = toneWarn, "正在找遥控器…"
		remote.body = "没配对过：打开“系统设置 → 蓝牙”，同时长按遥控器的主页键和菜单键，在列表里点连接。配对过：按遥控器任意键唤醒。"
		remote.acts = []act{{"打开蓝牙设置", func() { s.unpretend("remote"); open(bluetoothPane) }}}
	}

	bh := step{key: stepBlackHole, title: "安装 BlackHole 2ch"}
	switch {
	case !r.BlackHole:
		bh.tone, bh.state = toneWarn, "未安装"
		bh.body = "开源的虚拟声卡，遥指通过它把声音送给输入法。装原版就行，遥指不改它，也不改系统默认麦克风。装好后遥指会自动发现。"
		bh.code = blackHoleCommand
		bh.acts = []act{
			{"复制命令", func() {
				mygo.Clipboard.WriteText(blackHoleCommand)
				s.say("已复制，到终端里粘贴运行")
				s.unpretend("blackhole")
			}},
			{"打开下载页", func() { open(blackHoleURL) }},
		}
	case !r.Mic && r.MicErr != "":
		bh.tone, bh.state = toneWarn, mic+" 没建好"
		bh.body = mic + "：" + r.MicErr
	case !r.Mic:
		bh.tone, bh.state = toneBusy, "正在创建 "+mic+"…"
		bh.body = "BlackHole 已经装好，遥指正在它上面创建 " + mic + "。"
	default:
		bh.done, bh.state = true, "已安装 · "+mic+" 已就绪"
	}

	input := step{key: stepInput, title: "安装 " + name}
	switch {
	case !r.InputInstalled:
		input.tone, input.state = toneWarn, "未安装"
		input.body = "遥指不识别语音，文字由 " + name + " 写出来。装好并打开它之后，这一步会自己打勾。"
		if cmd := s.preset.App.Brew; cmd != "" {
			input.code = cmd
			input.acts = append(input.acts, act{"复制命令", func() {
				mygo.Clipboard.WriteText(cmd)
				s.say("已复制，到终端里粘贴运行")
				s.unpretend("typeless")
			}})
		}
		if url := s.preset.App.URL; url != "" {
			input.acts = append(input.acts, act{"打开 " + name + " 官网", func() { s.unpretend("typeless"); open(url) }})
		}
	case !r.InputRunning:
		input.title = "打开 " + name
		input.tone, input.state = toneWarn, "未运行"
		input.body = name + " 已经装好，还没打开。"
		input.acts = []act{{"打开 " + name, func() {
			s.unpretend("typeless-off")
			if err := exec.Command("open", "-b", s.preset.App.BundleID).Run(); err != nil {
				logf("打开 %s 失败：%v", name, err)
			}
		}}}
	default:
		input.done, input.state = true, "运行中"
	}

	ax := step{key: stepAccess, title: "允许辅助功能"}
	grant := func() { s.unpretend("ax"); s.guide(permissionflow.Accessibility) }
	switch {
	case r.Accessibility:
		ax.done, ax.state = true, "已允许"
	case s.guiding:
		ax.tone, ax.state = toneBusy, "等你允许"
		ax.body = "在系统设置里把 Yaozhi 拖进列表，再打开开关。"
	case r.AXRevoked:
		ax.tone, ax.state = toneBad, "已被关闭"
		ax.body = "“辅助功能”被关闭了，语音输入已停下。重新允许后马上恢复。"
		ax.acts = []act{{"重新授权", grant}}
	default:
		ax.tone, ax.state = toneWarn, "未允许"
		ax.body = name + " 用 Fn 开始和结束听写。遥指要在你按住、松开语音键时替你点这一下 Fn，需要“辅助功能”权限。遥指只发 Fn，不读取你的按键。"
		ax.acts = []act{{"去授权", grant}}
	}

	try := step{key: stepTry, title: "选麦克风，说一句"}
	switch {
	case st.TextSeen:
		try.done, try.state = true, "已出字"
	case st.Paused:
		try.tone, try.state = toneWarn, "语音输入已暂停"
		try.body = "语音输入暂停着，语音键已交还给系统。启用后在这里说一句。"
		try.acts = []act{{"启用", func() { s.eng.SetPaused(false) }}}
	case s.try.result == tryMiss:
		try.tone, try.state = toneWarn, "没有收到文字"
		try.body = "声音送到了 " + name + "，这里却没收到文字。看看 " + name + " 是否在运行、它的“选择麦克风”里选的是不是 " + mic + "，然后再说一句。"
	default:
		try.tone, try.state = toneWarn, "等你说话"
		try.body = "在 " + name + " 的菜单栏图标 →“选择麦克风”里选 " + mic + "，然后按住语音键说一句。"
		if st.Conn != device.StateReady {
			try.body = "先按遥控器任意键唤醒它。" + try.body
		}
	}

	all := []step{remote, bh, input, ax, try}
	for i := range all {
		if all[i].done {
			all[i].tone = toneOK
		}
	}
	return all
}

func (s *shell) status() status {
	st := s.st
	name := s.preset.Name
	x := status{steps: s.steps()}
	x.cur = len(x.steps)
	for i := len(x.steps) - 1; i >= 0; i-- {
		if !x.steps[i].done {
			x.left++
			x.cur = i
		}
	}
	x.setup = x.left > 0
	x.allSet = x.cur >= len(x.steps)-1 // only the first sentence may be missing
	x.done = s.finished && !x.setup

	// What is going on, the most pressing first.
	switch {
	case st.Voice == app.VoiceListening:
		x.tone, x.head, x.sub = toneVoice, "正在听", "说完松开就行。声音正送往 "+name+"。"
		x.bar = "正在听"
	case st.Voice == app.VoiceEnding:
		x.tone, x.busy, x.head, x.sub = toneBusy, true, "收尾中", "等最后一点声音送完，再通知 "+name+" 结束。"
		x.bar = "收尾中…"
	case st.Voice == app.VoiceDone:
		x.tone, x.head, x.sub = toneOK, "写好了。", "已交给 "+name+"，文字在你的光标处。"
		x.bar, x.detail = "已交给 "+name, fmt.Sprintf("%.1f 秒", st.LastAudio.Seconds())
		if s.try.armed && s.try.result == tryOK {
			x.bar = "出字成功"
		}
	case st.Voice == app.VoiceFailed:
		x.tone, x.head, x.sub = toneBad, "这次\n没成。", st.Problem
		x.bar, x.detail = "这次没有成功", st.Problem

	case x.done:
		// The first sentence just came out as text: everything is set up.
		// It is said until the user moves on, rather than the steps
		// vanishing the moment the last one is done.
		x.tone, x.head, x.sub = toneOK, "准备好了。", doneNote
		x.bar, x.detail, x.action = "全部准备好了", "五步都完成了", doneAction
		x.do = s.start

	case x.setup:
		// The step to take is said in the status bar, with its button; the
		// overview lists them all.
		c := x.steps[x.cur]
		x.tone, x.busy = c.tone, c.tone == toneBusy
		x.head, x.sub = fmt.Sprintf("还差 %d 步。", x.left), c.body
		x.bar, x.detail = fmt.Sprintf("还差 %d 步", x.left), c.title
		if len(c.acts) > 0 {
			x.action, x.do = c.acts[0].label, c.acts[0].do
		}

	case st.Paused:
		x.tone, x.head, x.sub = toneMuted, "已暂停。", "语音键已交还给系统。想用的时候再启用语音输入。"
		x.bar, x.detail, x.action, x.resume = "语音输入已暂停", "语音键交还给系统", "启用", true
		x.do = func() { s.eng.SetPaused(false) }

	case st.Conn == device.StateUnavailable:
		x.tone, x.head, x.sub = toneBad, "蓝牙\n没开。", "打开蓝牙后，遥指会自动连上遥控器。"
		x.bar, x.detail = "蓝牙已关闭", "打开后自动连接"
	case st.Conn == device.StateConnecting:
		x.tone, x.busy, x.head, x.sub = toneBusy, true, "正在\n连接。", "找到遥控器了，正在接通语音通道。"
		x.bar = "正在连接遥控器…"
	case st.Conn != device.StateReady:
		x.tone, x.head, x.sub = toneMuted, "等遥控器\n醒来。", "按一下遥控器上的任意键，它会自己连上。"
		x.bar, x.detail = "等待遥控器连接", "按遥控器任意键唤醒"
	default:
		x.tone, x.head, x.sub = toneOK, "按住，\n说话。", "按住遥控器上的语音键说一句，松开，文字就出现在你正在输入的地方。"
		x.bar, x.detail = "就绪", "按住语音键说话"
	}

	// A moment's news takes the status bar, unless the voice is on.
	if st.Voice == app.VoiceIdle && time.Now().Before(s.flashUntil) {
		x.bar, x.detail, x.action, x.do, x.busy, x.resume = s.flash, "", "", nil, false, false
		if x.tone != toneWarn && x.tone != toneBad {
			x.tone = toneOK
		}
	}
	return x
}

// start leaves the first launch behind, once all its steps are done: the
// overview goes on as it is from then on, and what was said into the try
// box is dropped.
func (s *shell) start() {
	s.finished = false
	s.try.clear()
}

// say shows msg on the status bar for a moment.
func (s *shell) say(msg string) {
	s.flash, s.flashUntil = msg, time.Now().Add(2200*time.Millisecond)
	if s.win != nil {
		time.AfterFunc(2300*time.Millisecond, func() { s.win.Update(func() {}) })
	}
}
