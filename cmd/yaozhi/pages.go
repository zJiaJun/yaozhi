// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"fmt"
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/zJiaJun/yaozhi/internal/app"
	"github.com/zJiaJun/yaozhi/internal/device"
	"github.com/zJiaJun/yaozhi/internal/voice"
)

// The pages beside the overview: "语音" (the voice input's settings and the
// way the voice takes), "按键" (what each key does; a preview of M2) and
// "设备" (the remote and the permissions).

// pageHead is a page's heading: the small label, the title, a line under it.
func pageHead(c *ui.Context, p pal, label, title, desc string) {
	eyebrow(c, p, label)
	ui.Text(c, title).Font(serif).FontSize(30).FontWeight(900).LineHeight(1.2).TextColor(p.ink).Margin(14, 0, 6, 0)
	if desc != "" {
		ui.Text(c, desc).FontSize(13).LineHeight(1.5).TextColor(p.ink2).MaxWidth(440)
	}
}

// setting is a line of a page's list: what it is on the left, its control
// or value on the right, and a hairline under it.
func setting(c *ui.Context, p pal, title, sub string, right func()) {
	ui.Row(c).Gap(12).Padding(13, 0).MinHeight(58).Children(func() {
		ui.Column(c).Gap(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.ink)
			if sub != "" {
				ui.Text(c, sub).FontSize(11.5).TextColor(p.ink2)
			}
		})
		ui.Spacer(c)
		ui.Row(c).Gap(8).Shrink(0).Children(right)
	})
	hair(c, p.line)
}

func (s *shell) voicePage(c *ui.Context, p pal) {
	st := s.st
	name := s.preset.Name
	ui.Column(c).Padding(40, padX, 0, padX).Children(func() {
		how := "按住语音键说话，松开后遥指等声音全部送完，再通知 " + name + " 结束。"
		pageHead(c, p, "语音", "语音输入", how+"遥指不识别、不保存语音。")
		ui.Row(c).AlignItems(ui.Start).Gap(44).Margin(26, 0, 0, 0).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
				hair(c, p.ink)
				setting(c, p, "启用语音输入", "关掉后语音键交还给系统", func() {
					if toggle(c, p, "enabled", !st.Paused) {
						s.eng.SetPaused(!st.Paused)
					}
				})
				trigger := "点一下开始，再点一下结束"
				if s.preset.Trigger.Mode == voice.ModeHold {
					trigger = "按住期间听写"
				}
				setting(c, p, "输入法", trigger, func() {
					ui.Text(c, name).FontSize(12.5).TextColor(p.ink2)
				})
				setting(c, p, "麦克风", name+" 里也选它", func() {
					col := p.ok
					if !(st.Ready.BlackHole && st.Ready.Mic) {
						col = p.warn
					}
					dot(c, col, 6)
					ui.Text(c, app.MicName).FontSize(12.5).TextColor(p.ink2)
				})
				setting(c, p, "音量增益", "声音太小、识别不准时加大", func() {
					gain := st.GainDB
					sl := ui.SliderBase(c.Key("gain"), &gain, 0, voice.MaxGainDB).Step(1).Size(164, 22).PaddingX(7).
						Cursor(ui.CursorPointer).Label("音量增益")
					at := float32(gain / voice.MaxGainDB)
					sl.Draw(func(pt *ui.Painter, r ui.Rect) {
						x0, w, y := r.X+7, r.W-14, r.Y+r.H/2
						pt.Fill(ui.Rect{X: x0, Y: y - 1, W: w, H: 2}, p.line, 1)
						pt.Fill(ui.Rect{X: x0, Y: y - 1, W: w * at, H: 2}, p.ink, 1)
						knob := (&ui.Path{}).Circle(x0+w*at, y, 6.2)
						pt.FillPath(knob, p.paper)
						pt.StrokePath(knob, 1.6, p.ink)
					})
					if sl.Changed() {
						s.eng.SetGain(math.Round(gain))
					}
					ui.Box(c).Width(44).AlignItems(ui.End).Children(func() {
						ui.Text(c, fmt.Sprintf("+%.0f dB", st.GainDB)).Font(mono).FontSize(12).TextColor(p.ink2).NoWrap()
					})
				})
			})

			// The way the voice takes, from the key to the text.
			ui.Column(c).Width(230).Shrink(0).Children(func() {
				eyebrow(c, p, "声音的去向").Margin(0, 0, 12, 0)
				stops := [][2]string{
					{"语音键", "遥控器 · 16 kHz"},
					{"遥指", "解码，补上开头"},
					{app.MicName, "BlackHole 2ch"},
					{name, "识别并上屏"},
				}
				for i, stop := range stops {
					last := i == len(stops)-1
					ui.Row(c).AlignItems(ui.Stretch).Gap(12).Children(func() {
						ui.Column(c).Width(9).AlignItems(ui.Center).Padding(5, 0, 0, 0).Children(func() {
							col := p.ok
							if i == 0 {
								col = p.zhu
							}
							dot(c, col, 9)
							if !last {
								ui.Box(c).Width(1).Grow(1).Background(p.line).Margin(4, 0, 0, 0)
							}
						})
						ui.Column(c).Gap(1).Padding(0, 0, 20, 0).Children(func() {
							ui.Text(c, stop[0]).FontSize(12.5).FontWeight(600).TextColor(p.ink)
							ui.Text(c, stop[1]).FontSize(11.5).TextColor(p.ink2)
						})
					})
				}
			})
		})
	})
}

// keyActions is what each key does in the first version: only the voice
// key is Yaozhi's, the others stay the system's.
var keyActions = []struct{ id, name, action string }{
	{"voice", "语音键", "语音输入"},
	{"power", "电源", "交给系统"},
	{"ring", "方向环", "方向键"},
	{"ok", "确认", "回车"},
	{"back", "返回", "交给系统"},
	{"home", "主页", "交给系统"},
	{"menu", "菜单", "交给系统"},
	{"vol", "音量 + −", "系统音量"},
	{"tv", "电视", "交给系统"},
}

func (s *shell) keysPage(c *ui.Context, p pal, ph float32) {
	// The remote on the left; the pointer over a key picks its line.
	rh := max(min(ph-36-24, 540), 300)
	k := rh / remoteH
	look := remoteLook{p: p, sel: s.selKey, still: true}
	ui.Box(c).Absolute().Left(64).Top(36).Size(remoteW*k, rh).
		Draw(func(pt *ui.Painter, r ui.Rect) { drawRemote(pt, r, look) }).
		Children(func() {
			for _, key := range remoteKeys {
				if ui.Box(c.Key("key-"+key.id)).Absolute().Left(key.x*k).Top(key.y*k).Size(key.w*k, key.h*k).Radius(key.w * k / 2).Hovered() {
					s.selKey = key.id
				}
			}
		})

	ui.Column(c).Absolute().Left(64 + remoteW*k + 56).Right(padX).Top(40).Children(func() {
		eyebrow(c, p, "按键")
		ui.Text(c, "每个键做什么").Font(serif).FontSize(30).FontWeight(900).LineHeight(1.2).TextColor(p.ink).Margin(14, 0, 10, 0)
		ui.Row(c).AlignSelf(ui.Start).Padding(6, 12).Radius(8).Border(1, p.line).Margin(0, 0, 16, 0).Children(func() {
			ui.Text(c, "现在只接管语音键，其余按键交给系统").FontSize(12).TextColor(p.ink2)
		})
		hair(c, p.ink)
		for _, a := range keyActions {
			r := ui.Row(c.Key("line-"+a.id)).Padding(8, 10).Margin(0, -10).Gap(8)
			if r.Hovered() {
				s.selKey = a.id
			}
			if s.selKey == a.id {
				r.Background(p.zhuSoft)
			}
			r.Children(func() {
				ui.Text(c, a.name).FontSize(13).FontWeight(600).TextColor(p.ink).Width(92)
				ui.Spacer(c)
				if a.id == "voice" {
					dot(c, p.zhu, 6)
				}
				ui.Text(c, a.action).FontSize(12).TextColor(p.ink2)
			})
			hair(c, p.line)
		}
	})
}

func (s *shell) devicePage(c *ui.Context, p pal, x status) {
	st := s.st
	ui.Column(c).Padding(40, padX, 0, padX).Children(func() {
		name := st.Device
		if name == "" {
			name = "还没连接遥控器"
		}
		desc := "语音通道 ATVV · 16 kHz"
		if st.Model != "" {
			desc = "型号 " + st.Model + " · " + desc
		}
		if st.Conn != device.StateReady && st.Conn != device.StateConnecting {
			desc = "先在“系统设置 → 蓝牙”里配对遥控器；配对过的，按任意键唤醒。"
		}
		pageHead(c, p, "设备", name, desc)

		ui.Column(c).Margin(26, 0, 0, 0).Children(func() {
			hair(c, p.ink)
			ui.Row(c).AlignItems(ui.Start).Padding(14, 0, 18, 0).Children(func() {
				fact := func(label, value, unit string) {
					ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
						ui.Text(c, label).FontSize(11.5).TextColor(p.ink2)
						ui.Row(c).AlignItems(ui.Baseline).Gap(3).Children(func() {
							ui.Text(c, value).Font(serif).FontSize(22).FontWeight(900).TextColor(p.ink).NoWrap()
							if unit != "" {
								ui.Text(c, unit).FontSize(12).FontWeight(500).TextColor(p.ink2)
							}
						})
					})
				}
				conn := "未连接"
				switch st.Conn {
				case device.StateReady:
					conn = "已连接"
				case device.StateConnecting:
					conn = "连接中"
				case device.StateUnavailable:
					conn = "蓝牙已关闭"
				}
				fact("连接", conn, "")
				if st.BatteryKnown {
					fact("电量", fmt.Sprint(st.Battery), "%")
				} else {
					fact("电量", "—", "")
				}
				fact("断线后", "自动重连", "")
			})
			hair(c, p.line)
			// The two permissions, from the steps they belong to.
			perm := func(key, title, sub string, w step, granted bool) {
				ui.Row(c.Key("perm-"+key)).Gap(12).Padding(13, 0).MinHeight(58).Children(func() {
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(p.ink)
						ui.Text(c, sub).FontSize(11.5).TextColor(p.ink2)
					})
					ui.Spacer(c)
					if granted {
						dot(c, p.ok, 6)
						ui.Text(c, "已允许").FontSize(12.5).TextColor(p.ink2)
						return
					}
					if w.tone == toneBusy {
						ui.Spinner(c).Size(10, 10)
					} else {
						dot(c, p.of(w.tone), 6)
					}
					ui.Text(c, w.state).FontSize(12.5).TextColor(p.ink2)
					if len(w.acts) > 0 && button(c, p, "perm-act-"+key, w.acts[0].label, true) {
						w.acts[0].do()
					}
				})
				hair(c, p.line)
			}
			perm("bt", "蓝牙", "连接遥控器并接收语音", x.steps[0], st.Ready.Bluetooth == app.PermGranted)
			perm("ax", "辅助功能", "替你点一下 "+s.preset.Name+" 的快捷键", x.steps[3], st.Ready.Accessibility)
		})
		ui.Row(c).Gap(10).Margin(22, 0, 0, 0).Children(func() {
			if button(c, p, "pair", "配对新遥控器", true) {
				if err := mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.BluetoothSettings"); err != nil {
					logf("打开蓝牙设置失败：%v", err)
				}
			}
			if button(c, p, "diag", "导出诊断", false) {
				s.exportDiagnostics()
			}
		})
	})
}
