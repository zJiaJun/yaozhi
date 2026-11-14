// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/zJiaJun/yaozhi/internal/app"
	"github.com/zJiaJun/yaozhi/internal/device"
)

// The main window, as docs/design/index.html lays it out: a sidebar
// with the pages and the remote, the page, and under it a status bar that
// is there on every page.

const (
	pageOverview = "overview"
	pageVoice    = "voice"
	pageKeys     = "keys"
	pageDevice   = "device"

	sideW = 204
	barH  = 50
	padX  = 48 // the pages' left and right margin
)

func (s *shell) mainView(c *ui.Context) {
	p := palette(c.Theme().Dark)
	W, H := c.Size()
	// The try box says in this frame whether it has the keyboard; it is
	// only there in the last step of the first launch.
	s.try.check(s)
	s.try.focused = false
	x := s.status()

	c.Root().Background(p.paper)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		s.sidebar(c, p)
		ui.Box(c).Width(1).Background(p.line).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Box(c).Grow(1).MinHeight(0).Clip().Children(func() {
				pw, ph := W-sideW-1, H-barH-1
				ui.Box(c.Key(s.page)).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() {
					switch s.page {
					case pageVoice:
						s.voicePage(c, p)
					case pageKeys:
						s.keysPage(c, p, ph)
					case pageDevice:
						s.devicePage(c, p, x)
					default:
						s.overview(c, p, x, pw, ph)
					}
				})
				// The top strip drags the window, as a title bar would. It
				// stops short of the right, where the overview has its chip.
				ui.Box(c).Absolute().Top(0).Left(0).Right(190).Height(40).DragWindow()
			})
			hair(c, p.line)
			s.statusBar(c, p, x)
		})
	})
}

// sidebar holds the window's controls, the name, the pages, and the remote.
func (s *shell) sidebar(c *ui.Context, p pal) {
	ui.Column(c).Width(sideW).Shrink(0).Background(p.paper2).Padding(0, 12, 12, 12).Children(func() {
		ui.Box(c).Height(50).DragWindow() // the traffic lights are here
		ui.Row(c).Gap(10).Padding(0, 6, 20, 6).DragWindow().Children(func() {
			ui.Box(c).Size(34, 34).Shrink(0).Draw(func(pt *ui.Painter, r ui.Rect) {
				pt.Image(brandMark(), ui.Rect{X: r.X - 4, Y: r.Y - 4, W: r.W + 8, H: r.H + 8}, ui.FillBox)
			})
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, "遥指").Font(serif).FontSize(19).FontWeight(900).LetterSpacing(1.5).TextColor(p.ink).FixedLineHeight(20)
				sub := "YAOZHI"
				if v := appVersion(); v != "" {
					sub += " · " + v
				}
				ui.Text(c, sub).FontSize(10).FontWeight(500).LetterSpacing(2.2).TextColor(p.ink3).NoWrap()
			})
		})
		nav := func(page, label string, ic *ui.SVG) {
			on := s.page == page
			b := ui.Row(c.Key("nav-"+page)).Gap(10).Padding(8, 10).Radius(8).Cursor(ui.CursorPointer)
			col := p.ink2
			if on {
				col = p.ink
			}
			if on || b.Hovered() {
				b.Background(p.line2)
			}
			b.Children(func() {
				if on { // the red mark on the window's edge
					ui.Box(c).Absolute().Left(-12).Top(9).Bottom(9).Width(3).Background(p.zhu)
				}
				ui.Icon(c, ic).FontSize(16).TextColor(col)
				t := ui.Text(c, label).FontSize(13).TextColor(col)
				if on {
					t.FontWeight(600)
				}
			})
			if b.Clicked() {
				s.page = page
			}
		}
		ui.Column(c).Gap(2).Children(func() {
			nav(pageOverview, "概览", iconOverview)
			nav(pageVoice, "语音", iconVoice)
			nav(pageKeys, "按键", iconKeys)
			nav(pageDevice, "设备", iconDevice)
		})
		ui.Spacer(c)
		hair(c, p.line)
		b := ui.Row(c.Key("side-device")).Gap(10).Padding(12, 6, 2, 6).Cursor(ui.CursorPointer)
		b.Children(func() {
			st := s.st
			voice := p.zhu
			if st.Conn != device.StateReady {
				voice = p.ink3
			}
			ui.Box(c).Size(12, 38).Shrink(0).Draw(func(pt *ui.Painter, r ui.Rect) { drawRemoteMark(pt, r, p.ink2, voice) })
			ui.Column(c).Gap(2).Shrink(1).MinWidth(0).Children(func() {
				name := st.Device
				if name == "" {
					name = "还没连接遥控器"
				}
				ui.Text(c, name).FontSize(12).FontWeight(600).TextColor(p.ink).NoWrap().Ellipsis("…")
				ui.Row(c).Gap(5).Children(func() {
					col, state := p.ink3, "未连接"
					switch st.Conn {
					case device.StateReady:
						col, state = p.ok, "已连接"
						if st.BatteryKnown {
							state += fmt.Sprintf(" · 电量 %d%%", st.Battery)
						}
					case device.StateConnecting:
						state = "连接中"
					case device.StateUnavailable:
						col, state = p.warn, "蓝牙已关闭"
					}
					dot(c, col, 6)
					ui.Text(c, state).FontSize(11).TextColor(p.ink2).NoWrap()
				})
			})
		})
		if b.Clicked() {
			s.page = pageDevice
		}
	})
}

// statusBar says what Yaozhi is doing, on every page, and shows the way
// the voice takes, which lights up while it is on its way.
func (s *shell) statusBar(c *ui.Context, p pal, x status) {
	st := s.st
	calm := still(c)
	ui.Row(c).Height(barH).Shrink(0).Padding(0, 20, 0, padX).Gap(10).Children(func() {
		if x.busy {
			ui.Spinner(c).Size(13, 13)
		} else {
			dot(c, p.of(x.tone), 6)
		}
		ui.Text(c, x.bar).FontSize(12.5).FontWeight(600).TextColor(p.ink).NoWrap()
		switch {
		case st.Voice == app.VoiceListening:
			since := st.VoiceSince
			ui.Box(c).Size(34, 16).Draw(func(pt *ui.Painter, r ui.Rect) {
				pt.AnimationFrameRate(4)
				secs := int(pt.Now().Sub(since).Seconds())
				label := ui.Span{Text: fmt.Sprintf("%d:%02d", secs/60, secs%60), Font: mono, Size: 12, Color: p.ink2}
				_, h := pt.MeasureText(0, label)
				pt.RichText(r.X, r.Y+(r.H-h)/2, 0, label)
			})
		case x.detail != "":
			ui.Text(c, x.detail).FontSize(12.5).TextColor(p.ink2).NoWrap().Shrink(1).MinWidth(0).Ellipsis("…")
		}
		if x.action != "" && x.do != nil && button(c, p, "bar-act", x.action, true) {
			x.do()
		}
		ui.Spacer(c)

		// The way: voice key → Yaozhi → yaozhi-mic → the input method.
		flowing := st.Voice == app.VoiceListening || st.Voice == app.VoiceEnding
		stops := []string{"语音键", "遥指", app.MicName, s.preset.Name}
		reach := len(stops) // how far the voice can get as things stand
		switch r := st.Ready; {
		case st.Paused || st.Conn != device.StateReady:
			reach = 0
		case !r.BlackHole || !r.Mic:
			reach = 2
		case !r.Accessibility:
			reach = 3
		}
		ui.Row(c).Shrink(0).Children(func() {
			for i, name := range stops {
				if i > 0 {
					ui.Box(c).Size(26, 1).Margin(0, 8).Background(p.line).Draw(func(pt *ui.Painter, r ui.Rect) {
						if !flowing || calm {
							return
						}
						pt.AnimationFrame()
						at := phase(pt, .9)
						pt.Clip(r, 0, func() {
							pt.Fill(ui.Rect{X: r.X - 12 + (r.W+12)*at, Y: r.Y, W: 12, H: 1}, p.zhu, 0)
						})
					})
				}
				col := p.ink3
				if flowing {
					col = p.ink
				}
				ui.Row(c).Gap(6).Children(func() {
					d := ui.Box(c).Size(5, 5).Radius(3).Border(1, col)
					if flowing {
						d.Background(p.zhu).BorderColor(p.zhu)
					}
					t := ui.Text(c, name).FontSize(11.5).TextColor(col).NoWrap()
					if i >= reach && !flowing {
						t.Strikethrough()
					}
				})
			}
		})
	})
}
