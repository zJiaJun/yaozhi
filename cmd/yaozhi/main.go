// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

// Command yaozhi is the Yaozhi app: a menu bar icon with a panel, and a main
// window, in MyGo's native UI. The engine (internal/app) does the work; this
// file only shows its state. The look follows docs/design/main-window.html,
// which later steps bring the window closer to.
//
// Build and run it as an app bundle (make yaozhi, then scripts/yaozhi.sh):
// macOS kills a process that touches Bluetooth without the usage string the
// bundle's Info.plist carries.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/egoist/mygo/ui"

	"github.com/zJiaJun/yaozhi/internal/app"
	"github.com/zJiaJun/yaozhi/internal/device"
	"github.com/zJiaJun/yaozhi/internal/voice"
)

var start = time.Now()

func logf(format string, args ...any) {
	fmt.Printf("[%7.3f] %s\n", time.Since(start).Seconds(), fmt.Sprintf(format, args...))
}

// supportDir is where user files live.
func supportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Yaozhi")
}

func main() {
	presetID := flag.String("preset", "typeless", "输入法预设")
	anyRemote := flag.Bool("any", false, "也接受没有设备描述的 ATVV 遥控器（试用新设备）")
	flag.Parse()

	descs, err := device.Catalog(filepath.Join(supportDir(), "devices"))
	if err != nil {
		logf("跳过了有问题的设备描述：%v", err)
	}
	presets, err := voice.Presets(filepath.Join(supportDir(), "presets"))
	if err != nil {
		logf("跳过了有问题的输入法预设：%v", err)
	}
	i := slices.IndexFunc(presets, func(p voice.Preset) bool { return p.ID == *presetID })
	if i < 0 {
		log.Fatalf("没有输入法预设 %q", *presetID)
	}

	eng := app.New(app.Options{Devices: descs, Preset: presets[i], AcceptAny: *anyRemote, Logf: logf})
	s := &shell{eng: eng}
	eng.OnChange(s.changed)

	mygo.App.SetName("Yaozhi")
	mygo.App.OnWindowAllClosed(func() {}) // lives on in the menu bar
	mygo.App.WhenReady(s.ready)
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { s.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { s.stop() })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// shell is the UI. Its fields are used on the main thread only, except
// changed, which the engine calls from its goroutines.
type shell struct {
	eng      *app.Engine
	st       app.State
	win      *mygo.Window
	panel    *mygo.Window // the open menu bar panel, or nil
	tray     *mygo.Tray
	trayTone string
	quitting bool
	guiding  bool

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *shell) ready() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		if err := s.eng.Run(s.ctx); err != nil {
			logf("引擎退出：%v", err)
		}
	}()

	var err error
	s.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: trayIcon("", false), IconIsTemplate: true, ToolTip: "遥指"})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	s.tray.OnClick(s.togglePanel)
	s.tray.OnRightClick(func() {
		if s.panel != nil {
			s.panel.Close()
		}
		s.tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "打开遥指", Click: func(*mygo.MenuItem, *mygo.Window) { s.showWindow() }},
			mygo.Separator(),
			{Label: "移除 yaozhi-mic 并退出", Click: func(*mygo.MenuItem, *mygo.Window) {
				if err := s.eng.RemoveMic(); err != nil {
					logf("移除 yaozhi-mic 失败：%v", err)
				}
				mygo.App.Quit()
			}},
			{Label: "退出遥指", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		}))
		defer s.tray.SetMenu(nil)
		s.tray.PopUpMenu()
	})
	s.tray.OnAppearanceChange(func() { s.trayTone = "?"; s.updateTray() })

	s.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "遥指",
		Width:         920,
		Height:        600,
		MinWidth:      780,
		MinHeight:     520,
		TitleBarStyle: mygo.TitleBarHiddenInset,
		Hidden:        true,
		Content:       ui.View(s.mainView),
	})
	s.win.OnClose(func(e *mygo.CloseEvent) {
		// Closing the window keeps Yaozhi in the menu bar; Quit closes it.
		if !s.quitting {
			e.PreventDefault()
			s.hideWindow()
		}
	})
	s.st = s.eng.State()
	s.showWindow()
}

// changed is called by the engine from any goroutine.
func (s *shell) changed() {
	for _, w := range []*mygo.Window{s.win, s.panel} {
		if w != nil {
			w.Update(func() { s.st = s.eng.State(); s.updateTray() })
		}
	}
}

func (s *shell) stop() {
	s.cancel()
	select {
	case <-s.done: // the engine restored the voice key and released Fn
	case <-time.After(2 * time.Second):
		logf("退出时引擎没有及时停下")
	}
	if s.tray != nil {
		s.tray.Destroy()
	}
}

func (s *shell) showWindow() {
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	if s.panel != nil {
		s.panel.Close()
	}
	s.win.Show()
	s.win.Focus()
}

func (s *shell) hideWindow() {
	s.win.Hide()
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
}

func (s *shell) togglePanel() {
	if s.panel != nil {
		s.panel.Close()
		return
	}
	s.st = s.eng.State()
	s.panel = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      s.tray,
		Placement: mygo.PlacementBottom,
		Gap:       4,
		Width:     320,
		Height:    250,
		Focusable: true,
		Shadow:    true,
		Popover:   true,
		Content:   ui.View(s.panelView),
	})
	s.panel.OnClosed(func() { s.panel = nil })
}

// guide opens System Settings at the permission, with MyGo's panel to drag
// Yaozhi into the list, and checks again once the user is done.
func (s *shell) guide(p permissionflow.Permission) {
	if s.guiding {
		return
	}
	s.guiding = true
	go func() {
		st, err := permissionflow.Guide(s.ctx, p, permissionflow.GuideOptions{})
		logf("授权引导（%s）结束：%v %v", p.Title(), st, err)
		s.eng.Recheck()
		s.win.Update(func() { s.guiding = false })
	}()
}

// ---------------------------------------------------------------- status

// tone is how a status reads at a glance.
type tone int

const (
	toneOK tone = iota
	toneWarn
	toneBad
	toneBusy
	toneVoice
	toneMuted
)

func (k tone) color(t *ui.Theme) ui.Color {
	switch k {
	case toneWarn:
		return t.Warning
	case toneBad:
		return t.Danger
	case toneBusy:
		return t.Accent
	case toneVoice:
		return ui.Hex("#ff6b6b")
	case toneMuted:
		return t.TextMuted
	}
	return t.Success
}

// status is the one line the island and the panel show.
type status struct {
	tone   tone
	title  string
	detail string
	action string // a button, if the user can do something about it
	do     func()
}

func (s *shell) status() status {
	st := s.st
	r := st.Ready
	switch st.Voice {
	case app.VoiceListening:
		return status{tone: toneVoice, title: "正在听", detail: "松开语音键结束"}
	case app.VoiceEnding:
		return status{tone: toneBusy, title: "收尾中…"}
	case app.VoiceDone:
		return status{tone: toneOK, title: "已交给 Typeless", detail: fmt.Sprintf("%.1f 秒", st.LastAudio.Seconds())}
	case app.VoiceFailed:
		return status{tone: toneBad, title: "这次没有成功", detail: st.Problem}
	}
	switch {
	case r.Bluetooth == app.PermDenied:
		return status{tone: toneBad, title: "没有蓝牙权限", detail: "连不上遥控器", action: "去系统设置", do: func() { s.guide(permissionflow.Bluetooth) }}
	case !r.Accessibility:
		return status{tone: toneWarn, title: fmt.Sprintf("还差 %d 步", app.TotalSteps-r.Steps()), detail: "下一步：允许辅助功能", action: "去授权", do: func() { s.guide(permissionflow.Accessibility) }}
	case !r.BlackHole:
		return status{tone: toneWarn, title: fmt.Sprintf("还差 %d 步", app.TotalSteps-r.Steps()), detail: "下一步：安装 BlackHole 2ch"}
	case !r.Mic && r.MicErr != "":
		return status{tone: toneWarn, title: "虚拟麦克风没准备好", detail: r.MicErr}
	case !r.Mic:
		return status{tone: toneBusy, title: "正在准备虚拟麦克风…"}
	}
	switch st.Conn {
	case device.StateUnavailable:
		return status{tone: toneBad, title: "蓝牙已关闭"}
	case device.StateSearching:
		return status{tone: toneMuted, title: "等待遥控器连接", detail: "按遥控器任意键唤醒"}
	case device.StateConnecting:
		return status{tone: toneBusy, title: "正在连接遥控器…"}
	}
	return status{tone: toneOK, title: "就绪", detail: "按住语音键说话，松开出字"}
}

func (s *shell) updateTray() {
	if s.tray == nil {
		return
	}
	tn := ""
	switch k := s.status().tone; {
	case k == toneBad:
		tn = "bad"
	case k == toneWarn:
		tn = "warn"
	}
	if tn == s.trayTone {
		return
	}
	s.trayTone = tn
	if tn == "" {
		_ = s.tray.SetIcon(trayIcon("", false), true)
	} else {
		_ = s.tray.SetIcon(trayIcon(tn, s.tray.Dark()), false)
	}
}

// ---------------------------------------------------------------- views

func dot(c *ui.Context, col ui.Color, size float32) {
	ui.Column(c).Width(size).Height(size).Radius(size / 2).Background(col)
}

func (s *shell) mainView(c *ui.Context) {
	t := c.Theme()
	st := s.st
	ui.Column(c).Fill().Padding(56, 36, 28).Gap(24).Children(func() {
		ui.Row(c).Grow(1).Gap(32).Children(func() {
			// left: what Yaozhi does, and today's numbers
			ui.Column(c).Grow(1).Gap(14).Justify(ui.Center).Children(func() {
				s.deviceChip(c)
				ui.Text(c, "按住，说话。").FontSize(34).Bold()
				ui.Text(c, "按住遥控器上的语音键说一句，松开，文字就出现在你正在输入的地方。").
					FontSize(14).TextColor(t.TextMuted).MaxWidth(360)
				ui.Row(c).Gap(28).Children(func() {
					stat(c, fmt.Sprint(st.Count), "这次打开后说话")
					last := "—"
					if st.LastAudio > 0 {
						last = fmt.Sprintf("%.1f 秒", st.LastAudio.Seconds())
					}
					stat(c, last, "上一段")
				})
			})
			// right: what Yaozhi still needs
			s.readiness(c)
		})
		ui.Row(c).Justify(ui.Center).Children(func() { s.island(c) })
	})
}

func stat(c *ui.Context, value, label string) {
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, value).FontSize(20).Bold()
		ui.Text(c, label).FontSize(11).TextColor(c.Theme().TextMuted)
	})
}

func (s *shell) deviceChip(c *ui.Context) {
	t := c.Theme()
	st := s.st
	col, text := t.TextMuted, "未连接"
	switch st.Conn {
	case device.StateReady:
		col, text = t.Success, "已连接"
	case device.StateConnecting:
		col, text = t.Accent, "连接中"
	}
	name := st.Device
	if name == "" {
		name = "遥控器"
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(6, 12, 6, 10).Radius(999).Background(t.Surface).Border(1, t.Border).
		AlignSelf(ui.Start).Children(func() {
		dot(c, col, 8)
		ui.Text(c, name).Bold()
		ui.Text(c, text).TextColor(t.TextMuted)
	})
}

// readiness is the checklist of docs/design/permissions.html.
func (s *shell) readiness(c *ui.Context) {
	t := c.Theme()
	r := s.st.Ready
	ui.Column(c).Width(400).Gap(4).Padding(16).Radius(18).Background(t.Surface).Border(1, t.Border).
		AlignSelf(ui.Center).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(0, 4, 8).Children(func() {
			ui.Text(c, "准备情况").FontSize(17).Bold()
			ui.Spacer(c)
			ui.Text(c, fmt.Sprintf("%d/%d", r.Steps(), app.TotalSteps)).TextColor(t.TextMuted)
		})
		section(c, "权限")
		switch r.Bluetooth {
		case app.PermGranted:
			check(c, "bt", "蓝牙", "连接遥控器、接收语音", toneOK, "已允许", "", nil)
		case app.PermDenied:
			check(c, "bt", "蓝牙", "只能到系统设置里打开", toneBad, "已拒绝", "去系统设置", func() { s.guide(permissionflow.Bluetooth) })
		default:
			check(c, "bt", "蓝牙", "等待系统询问", toneMuted, "等待中", "", nil)
		}
		if r.Accessibility {
			check(c, "ax", "辅助功能", "替你点 Fn，开始和结束 Typeless", toneOK, "已允许", "", nil)
		} else {
			label := "去授权"
			if s.guiding {
				label = "等待中…"
			}
			check(c, "ax", "辅助功能", "没有它，语音输入用不了", toneWarn, "未允许", label, func() { s.guide(permissionflow.Accessibility) })
		}
		section(c, "语音链路")
		// BlackHole is the user's to install; yaozhi-mic on top of it is Yaozhi's to make.
		switch {
		case !r.BlackHole:
			check(c, "vm", "虚拟麦克风", "先安装 BlackHole 2ch：brew install blackhole-2ch", toneWarn, "未安装", "复制命令", func() {
				mygo.Clipboard.WriteText("brew install blackhole-2ch")
			})
		case !r.Mic && r.MicErr != "":
			check(c, "vm", "虚拟麦克风", "yaozhi-mic："+r.MicErr, toneWarn, "未就绪", "", nil)
		case !r.Mic:
			check(c, "vm", "虚拟麦克风", "正在创建 yaozhi-mic…", toneBusy, "准备中", "", nil)
		default:
			check(c, "vm", "虚拟麦克风", "BlackHole 2ch，Typeless 从 yaozhi-mic 收音", toneOK, "已就绪", "", nil)
		}
		check(c, "tl", "Typeless", "在它的设置里把麦克风选为 yaozhi-mic", toneMuted, "需要你确认", "", nil)
	})
}

func section(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(11).Bold().TextColor(c.Theme().TextMuted).Padding(8, 6, 2)
}

func check(c *ui.Context, key, title, sub string, k tone, label, action string, do func()) {
	t := c.Theme()
	bg := ui.Transparent
	if k == toneWarn || k == toneBad {
		bg = t.SurfaceHover
	}
	ui.Row(c.Key(key)).Gap(12).AlignItems(ui.Center).Padding(9, 10).Radius(12).Background(bg).Children(func() {
		dot(c, k.color(t), 9)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).Bold()
				ui.Text(c, label).FontSize(11).TextColor(k.color(t))
			})
			ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted)
		})
		if action != "" && do != nil {
			if ui.PrimaryButton(c.Key(key+"-act"), action).Clicked() {
				do()
			}
		}
	})
}

// island is the status line at the bottom of the window.
func (s *shell) island(c *ui.Context) {
	t := c.Theme()
	st := s.status()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(10, 18).Radius(999).Background(t.Surface).Border(1, t.Border).
		MinHeight(48).Children(func() {
		if st.tone == toneBusy {
			ui.Spinner(c)
		} else {
			dot(c, st.tone.color(t), 9)
		}
		ui.Text(c, st.title).Bold()
		if st.detail != "" {
			ui.Text(c, st.detail).TextColor(t.TextMuted)
		}
		if st.action != "" {
			if ui.PrimaryButton(c.Key("island-act"), st.action).Clicked() {
				st.do()
			}
		}
	})
}

func (s *shell) panelView(c *ui.Context) {
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	st := s.status()
	box := ui.Column(c).Fill()
	if !c.Vibrancy() {
		box.Background(t.Surface).Radius(10).Border(1, t.Border)
	}
	box.Padding(16).Gap(12).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			dot(c, st.tone.color(t), 10)
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, "遥指").FontSize(15).Bold()
				name := s.st.Device
				if name == "" {
					name = "还没连接遥控器"
				}
				ui.Text(c, name).FontSize(12).TextColor(t.TextMuted)
			})
		})
		ui.Column(c).Gap(4).Padding(10, 12).Radius(10).Background(t.SurfaceHover).Children(func() {
			ui.Text(c, st.title).Bold()
			if st.detail != "" {
				ui.Text(c, st.detail).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if st.action != "" {
			if ui.Button(c.Key("panel-act"), st.action).Clicked() {
				st.do()
			}
		}
		ui.Spacer(c)
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c.Key("open"), "打开遥指").Clicked() {
				s.showWindow()
			}
			ui.Spacer(c)
			ui.Button(c.Key("quit"), "退出").OnClick(mygo.App.Quit)
		})
	})
}
