// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"bytes"
	"image/png"
	"math"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/zJiaJun/yaozhi/internal/icon"
)

// The look is "墨与朱", docs/design/index.html: paper, ink, and a little
// cinnabar. Everything is black line work on paper, and only the voice is
// red: the voice key, and what shows that Yaozhi is listening. Fine is
// pine green, and what needs the user is gamboge yellow.

// Headings and numbers are set in Songti, which macOS has; the rest is the
// system's font.
const (
	serif = "Songti SC"
	mono  = "SF Mono, Menlo, monospace"
)

type pal struct {
	dark bool

	paper, paper2, card ui.Color // the window, its sidebar, what lies on it
	line, line2         ui.Color // hairlines, and fills as faint
	ink, ink2, ink3     ui.Color // text: full, secondary, faint
	zhu, zhuSoft        ui.Color // the voice
	ok, warn            ui.Color
}

func palette(dark bool) pal {
	p := pal{dark: dark}
	if dark {
		p.paper, p.paper2, p.card = ui.Hex("#161513"), ui.Hex("#100F0E"), ui.Hex("#1E1C1A")
		p.ink = ui.Hex("#EEE8DB")
		p.line, p.line2 = p.ink.Alpha(.14), p.ink.Alpha(.06)
		p.zhu, p.ok, p.warn = ui.Hex("#EE5A40"), ui.Hex("#74B892"), ui.Hex("#DDA63A")
		p.zhuSoft = p.zhu.Alpha(.14)
	} else {
		p.paper, p.paper2, p.card = ui.Hex("#F4F0E6"), ui.Hex("#EBE6D9"), ui.Hex("#FAF7F0")
		p.ink = ui.Hex("#1C1916")
		p.line, p.line2 = p.ink.Alpha(.13), p.ink.Alpha(.06)
		p.zhu, p.ok, p.warn = ui.Hex("#D5402B"), ui.Hex("#3C7A5A"), ui.Hex("#B57A12")
		p.zhuSoft = p.zhu.Alpha(.10)
	}
	p.ink2, p.ink3 = p.ink.Alpha(.64), p.ink.Alpha(.4)
	return p
}

// tone is how something reads at a glance.
type tone int

const (
	toneOK tone = iota
	toneWarn
	toneBad   // needs the user now; yellow like toneWarn, red is the voice's
	toneBusy  // waiting or working: a spinner
	toneVoice // listening
	toneMuted // nothing to do, nothing done
)

func (p pal) of(k tone) ui.Color {
	switch k {
	case toneWarn, toneBad:
		return p.warn
	case toneBusy:
		return p.ink2
	case toneVoice:
		return p.zhu
	case toneMuted:
		return p.ink3
	}
	return p.ok
}

// dot is a small status dot.
func dot(c *ui.Context, col ui.Color, size float32) ui.Element {
	return ui.Box(c).Size(size, size).Radius(size / 2).Background(col).Shrink(0)
}

// hair is a hairline across a column.
func hair(c *ui.Context, col ui.Color) { ui.Box(c).Height(1).Background(col).Shrink(0) }

// eyebrow is the small spaced-out label over a page's heading.
func eyebrow(c *ui.Context, p pal, s string) ui.Element {
	return ui.Text(c, s).FontSize(11).FontWeight(600).LetterSpacing(2).TextColor(p.ink3)
}

// button is a pill: filled with ink, or an outline that fills while the
// pointer is over it.
func button(c *ui.Context, p pal, key, label string, filled bool) bool {
	b := ui.Row(c.Key(key)).Padding(5, 14).Radius(999).Border(1, p.ink).Cursor(ui.CursorPointer).Shrink(0)
	on := filled || b.Hovered()
	col := p.ink
	if on {
		b.Background(p.ink)
		col = p.paper
	}
	if b.Pressed() {
		b.Opacity(.8)
	}
	b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(col).NoWrap() })
	return b.Clicked()
}

// toggle is a switch in the voice's red; it reports a click, and the
// caller turns it over.
func toggle(c *ui.Context, p pal, key string, on bool) bool {
	b := ui.Box(c.Key(key)).Size(36, 21).Radius(11).Cursor(ui.CursorPointer).Shrink(0).Label("开关")
	at := float32(0)
	if on {
		at = 1
	}
	at = b.AnimateTo("at", at, ui.Snappy)
	b.Draw(func(pt *ui.Painter, r ui.Rect) {
		pt.Fill(r, p.ink3.MixOklab(p.zhu, at), r.H/2)
		x := r.X + 2 + (r.W-21)*at
		pt.Shadow(ui.Rect{X: x, Y: r.Y + 2, W: 17, H: 17}, 8.5, 0, 1, 2, 0, ui.RGBA(0, 0, 0, .25))
		pt.Fill(ui.Rect{X: x, Y: r.Y + 2, W: 17, H: 17}, ui.RGB(255, 255, 255), 8.5)
	})
	return b.Clicked()
}

// still reports whether motion should stop: the user asked for less of it,
// or the window is in the background.
func still(c *ui.Context) bool {
	return c.Preferences().ReduceMotion || !c.WindowActive()
}

// phase is how far through a period of secs seconds the painter's clock
// is, from 0 to 1.
func phase(p *ui.Painter, secs float64) float32 {
	return float32(math.Mod(float64(p.Now().UnixMilli())/1000/secs, 1))
}

// checkPath is a check mark in a 24-unit box.
var checkPath = ui.MustParsePath("M5.5 12.5 L10 17 L18.5 7.5")

// Icons of the sidebar, in the color of the text.
var (
	iconOverview = strokeIcon(`<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="2.2" fill="currentColor" stroke="none"/>`)
	iconVoice    = strokeIcon(`<path d="M4 12h1M8 8v8M12 4.5v15M16 9v6M20 12h-1"/>`)
	iconKeys     = strokeIcon(`<rect x="8" y="2.5" width="8" height="19" rx="4"/><circle cx="12" cy="8" r="1.8"/><path d="M12 13.5v.01M12 17v.01"/>`)
	iconDevice   = strokeIcon(`<path d="M7 7l10 10-5 5V2l5 5L7 17"/>`)
)

func strokeIcon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// brandMark is the app icon, for the sidebar, drawn when first shown.
var brandMark = sync.OnceValue(func() *ui.Bitmap {
	var b bytes.Buffer
	if err := png.Encode(&b, icon.App(136)); err != nil {
		panic(err)
	}
	bm, err := ui.DecodeBitmap(b.Bytes())
	if err != nil {
		panic(err)
	}
	return bm
})
