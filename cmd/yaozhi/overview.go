// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/zJiaJun/yaozhi/internal/app"
	"github.com/zJiaJun/yaozhi/internal/device"
)

// The overview: the heading is the state, in large type; under it the
// voice's level and today's numbers; on the right the remote in line work,
// its voice key named. The readiness chip in the corner opens the list of
// what voice input needs.

func (s *shell) overview(c *ui.Context, p pal, x status, pw, ph float32) {
	st := s.st
	calm := still(c)
	listening := st.Voice == app.VoiceListening

	// The remote, as tall as the page allows, with its callout on the left.
	rh := max(min(ph-74-28, 540), 300)
	rw := rh * (remoteW + calloutW) / remoteH
	look := remoteLook{p: p, listening: listening, since: st.VoiceSince, still: calm, callout: true,
		muted: !listening && (!x.allSet || st.Paused || st.Conn != device.StateReady)} // the key would do nothing
	ui.Box(c).Absolute().Right(56).Top(74).Size(rw, rh).Draw(func(pt *ui.Painter, r ui.Rect) { drawRemote(pt, r, look) })

	// The left column keeps clear of the remote's body; the callout may
	// reach over the room beside the heading.
	colW := min(404, pw-padX-56-rh*remoteW/remoteH-28)
	size := float32(76)
	if colW < 360 {
		size = 60
	}
	head := p.ink
	if listening {
		head = p.zhu
	}
	if x.setup || x.done { // the first launch, or something went missing since
		s.setupColumn(c, p, x, colW, head, calm)
		return
	}
	ui.Column(c).Absolute().Left(padX).Top(40).Width(colW).Children(func() {
		eyebrow(c, p, "概览")
		lh := float32(math.Round(float64(size) * 1.12))
		ui.Box(c).MinHeight(2*lh).Margin(30, 0, 18, 0).Children(func() {
			ui.Text(c, x.head).Font(serif).FontSize(size).FontWeight(900).FixedLineHeight(lh).LetterSpacing(.5).TextColor(head)
		})
		ui.Box(c).MinHeight(44).MaxWidth(360).Children(func() {
			ui.Text(c, x.sub).FontSize(14).LineHeight(1.5).TextColor(p.ink2)
		})
		ui.Box(c).Height(64).Margin(22, 0, 0, 0).Draw(func(pt *ui.Painter, r ui.Rect) { s.drawWave(pt, r, p, p.ink3, listening, calm) })
	})

	// Today's numbers, on the page's bottom.
	t := st.Stats
	ui.Column(c).Absolute().Left(padX).Bottom(24).Width(colW).Children(func() {
		hair(c, p.ink)
		ui.Row(c).AlignItems(ui.Start).Padding(10, 0, 0, 0).Children(func() {
			number := func(value, unit, label string) {
				ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
					ui.Row(c).AlignItems(ui.Baseline).Gap(3).Children(func() {
						ui.Text(c, value).Font(serif).FontSize(30).FontWeight(900).FixedLineHeight(32).TextColor(p.ink).NoWrap()
						if unit != "" {
							ui.Text(c, unit).FontSize(12).FontWeight(500).TextColor(p.ink2)
						}
					})
					ui.Text(c, label).FontSize(11.5).TextColor(p.ink2).NoWrap()
				})
			}
			number(fmt.Sprint(t.Today), "", "今天说话")
			if t.Today > 0 {
				number(fmt.Sprintf("%.1f", t.AvgAudio().Seconds()), "秒", "平均时长")
				number(fmt.Sprint(t.AvgLatency().Milliseconds()), "ms", "松开后出字")
			} else {
				number("—", "", "平均时长")
				number("—", "", "松开后出字")
			}
		})
	})

	s.readyChip(c, p, x)
}

// drawWave draws the voice's level as bars spreading from the middle, the
// newest level there; at rest it is a dotted line.
func (s *shell) drawWave(pt *ui.Painter, r ui.Rect, p pal, idle ui.Color, listening, calm bool) {
	const n = 58
	if listening {
		pt.AnimationFrame()
		// The level is sampled 30 times a second.
		if now := pt.Now(); now.Sub(s.waveAt) >= 33*time.Millisecond {
			s.waveAt = now
			s.level += (s.eng.Level() - s.level) * .5
			copy(s.wave[1:], s.wave[:])
			s.wave[0] = s.level
		}
	} else {
		s.wave = [len(s.wave)]float32{}
		s.level = 0
	}
	col := idle
	if listening {
		col = p.zhu
	}
	step := r.W / n
	for i := range n {
		h := float32(2)
		if listening && !calm {
			// How far from the middle, and so how long ago.
			d := math.Abs(float64(i) - (n-1)/2.0)
			v := s.wave[min(int(d), len(s.wave)-1)]
			env := float32(math.Cos(d / (n / 2.0) * math.Pi / 2)) // lower towards the ends
			h = 3 + v*(r.H-3)*env
		}
		x := r.X + float32(i)*step + (step-3)/2
		pt.Fill(ui.Rect{X: x, Y: r.Y + (r.H-h)/2, W: 3, H: h}, col, 1.5)
	}
}

// readyChip is the chip in the page's corner once everything is set up,
// and the list it opens: the five steps, all done.
func (s *shell) readyChip(c *ui.Context, p pal, x status) {
	chip := ui.Row(c.Key("ready-chip")).Absolute().Right(28).Top(22).Gap(7).Padding(5, 11).Radius(999).
		Border(1, p.line).Background(p.paper).Cursor(ui.CursorPointer)
	if chip.Hovered() || s.readyOpen {
		chip.Background(p.card).BorderColor(p.ink3)
	}
	chip.Children(func() {
		dot(c, p.ok, 6)
		ui.Text(c, "一切就绪").FontSize(12).TextColor(p.ink).NoWrap()
		ui.Text(c, fmt.Sprintf("%d/%d", len(x.steps)-x.left, len(x.steps))).Font(mono).FontSize(11).TextColor(p.ink3)
	})
	if chip.Clicked() {
		s.readyOpen = !s.readyOpen
	}
	if !s.readyOpen {
		return
	}
	ui.Column(c.Key("ready-list")).Absolute().Right(28).Top(58).Width(308).Padding(4, 14).Radius(12).
		Background(p.card).Border(1, p.line).Shadow(0, 18, 40, 0, ui.RGBA(20, 16, 10, .16)).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -6}, Exit: &ui.Motion{Y: -6}}).
		Children(func() {
			for i, w := range x.steps {
				if i > 0 {
					hair(c, p.line2)
				}
				ui.Row(c.Key("row-"+w.key)).Gap(9).Padding(9, 0).Children(func() {
					dot(c, p.of(w.tone), 6)
					ui.Text(c, w.title).FontSize(12.5).TextColor(p.ink).NoWrap()
					ui.Spacer(c)
					ui.Text(c, w.state).FontSize(11.5).TextColor(p.ink2).NoWrap().Shrink(1).MinWidth(0).Ellipsis("…")
				})
			}
		})
}

// setupColumn is the overview's left side while something is still to do:
// how many steps are left, in large type, and the five of them in order.
// Those done are ticked; the first that is not is open, saying what to do
// and offering it.
func (s *shell) setupColumn(c *ui.Context, p pal, x status, colW float32, head ui.Color, calm bool) {
	size := float32(64)
	if colW < 360 {
		size = 52
	}
	ui.Column(c).Absolute().Left(padX).Top(40).Width(colW).Children(func() {
		eyebrow(c, p, "概览")
		ui.Text(c, strings.ReplaceAll(x.head, "\n", "")).Font(serif).FontSize(size).FontWeight(900).
			FixedLineHeight(size*1.12).LetterSpacing(.5).TextColor(head).NoWrap().Margin(20, 0, 14, 0)
		hair(c, p.ink)
		for i, w := range x.steps {
			cur := i == x.cur
			ui.Column(c.Key("step-"+w.key)).Padding(8, 0).Children(func() {
				ui.Row(c).Gap(10).Children(func() {
					ui.Box(c).Size(18, 18).Shrink(0).Draw(func(pt *ui.Painter, r ui.Rect) {
						ring := (&ui.Path{}).Circle(r.X+9, r.Y+9, 8.5)
						switch {
						case w.done:
							pt.FillPath(ring, p.ok)
							pt.Transform(ui.Scaling(.75, .75).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
								pt.StrokePath(checkPath, 2.6, ui.RGB(255, 255, 255))
							})
							return
						case cur:
							pt.FillPath(ring, p.ink)
						default:
							pt.StrokePath(ring, 1, p.ink3)
						}
						col := p.ink3
						if cur {
							col = p.paper
						}
						n := ui.Span{Text: fmt.Sprint(i + 1), Font: mono, Size: 10.5, Weight: 600, Color: col}
						nw, nh := pt.MeasureText(0, n)
						pt.RichText(r.X+9-nw/2, r.Y+9-nh/2, 0, n)
					})
					title := ui.Text(c, w.title).FontSize(13).FontWeight(600).NoWrap()
					if w.done || cur {
						title.TextColor(p.ink)
					} else {
						title.TextColor(p.ink3)
					}
					ui.Spacer(c)
					if cur && w.tone == toneBusy {
						ui.Spinner(c).Size(11, 11)
					}
					if w.done || cur {
						ui.Text(c, w.state).FontSize(11.5).TextColor(p.ink2).NoWrap().Shrink(1).MinWidth(0).Ellipsis("…")
					}
				})
				if x.done && w.key == stepTry { // what was just said stays in sight
					ui.Box(c).Margin(0, 0, 2, 28).Children(func() { s.try.view(c, s, p, x, calm) })
				}
				if !cur {
					return
				}
				ui.Column(c).Margin(6, 0, 4, 28).Children(func() {
					ui.Text(c, w.body).FontSize(12.5).LineHeight(1.55).TextColor(p.ink2)
					if w.code != "" {
						ui.Box(c).AlignSelf(ui.Start).Margin(7, 0, 0, 0).Padding(2, 8).Radius(6).Background(p.paper2).Border(1, p.line).Children(func() {
							ui.Text(c, w.code).Font(mono).FontSize(12).TextColor(p.ink).Selectable()
						})
					}
					if len(w.acts) > 0 {
						ui.Row(c).Gap(8).Margin(9, 0, 0, 0).Children(func() {
							for j, a := range w.acts {
								if button(c, p, fmt.Sprint("step-act-", j), a.label, j == 0) {
									a.do()
								}
							}
						})
					}
					if w.key == stepTry && len(w.acts) == 0 {
						s.try.view(c, s, p, x, calm)
					}
				})
			})
			hair(c, p.line)
		}
		if x.done { // all ticked: what comes next, and the way on
			ui.Text(c, doneNote).FontSize(13).LineHeight(1.55).TextColor(p.ink2).Margin(14, 0, 0, 0)
			ui.Row(c).Margin(12, 0, 0, 0).Children(func() {
				if button(c, p, "setup-done", doneAction, true) {
					s.start()
				}
			})
		}
	})
}
