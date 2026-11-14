// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The front of the Xiaomi Bluetooth Remote 2 Pro (RC003) as line work: its
// outline and its keys, nothing of its materials. The geometry is that of
// docs/design/rc003.js (drawn from scratch from the product's layout; no
// photo is used and no logo or wordmark is drawn), in a 200 × 840 box.
// Another remote is another set of lines.

const (
	remoteW, remoteH = 200, 840
	voiceX, voiceY   = 142, 80
	voiceR           = 22
)

// remoteKey is a key's place on the drawing, a circle or a pill.
type remoteKey struct {
	id         string
	x, y, w, h float32 // its box
}

func circleKey(id string, cx, cy, r float32) remoteKey {
	return remoteKey{id, cx - r, cy - r, 2 * r, 2 * r}
}

// remoteKeys are the keys as the "按键" page lists them. The ring is under
// its middle, the OK key, so that one comes after it.
var remoteKeys = []remoteKey{
	circleKey("power", 58, 80, 22), circleKey("voice", voiceX, voiceY, voiceR),
	circleKey("ring", 100, 208, 83), circleKey("ok", 100, 208, 44),
	circleKey("back", 62, 348, 31), circleKey("home", 62, 425, 31), circleKey("menu", 62, 502, 31),
	{"vol", 110, 317, 62, 139}, circleKey("tv", 141, 502, 31),
}

var (
	remoteGlyphs = ui.MustParsePath(
		"M58 70.5 V79 M50.6 75.5 A10.2 10.2 0 1 0 65.4 75.5 " + // power
			"M94 148 L100 142 L106 148 M94 268 L100 274 L106 268 M40 202 L34 208 L40 214 M160 202 L166 208 L160 214 " + // arrows
			"M71 348 H53 M60 341 L53 348 L60 355 " + // back
			"M51 426 L62 416 L73 426 M55 424 V434 H69 V424 " + // home
			"M53 495 H71 M53 502 H71 M53 509 H71 " + // menu
			"M141 341 V355 M134 348 H148 M134 425 H148 " + // volume
			"M130 493 H152 V508 H130 Z M136 513 H146") // tv
	voiceCapsule = ui.MustParsePath("M137.5 72.5 A4.5 4.5 0 0 1 146.5 72.5 V78.5 A4.5 4.5 0 0 1 137.5 78.5 Z")
	voiceStand   = ui.MustParsePath("M133 80 A9 9 0 0 0 151 80 M142 89 V93")
)

// keyPath is a key's outline.
func keyPath(k remoteKey) *ui.Path {
	if k.w == k.h {
		return (&ui.Path{}).Circle(k.x+k.w/2, k.y+k.h/2, k.w/2)
	}
	r := k.w / 2 // a pill, standing
	return (&ui.Path{}).MoveTo(k.x, k.y+r).
		CubeTo(k.x, k.y+r*.45, k.x+r*.45, k.y, k.x+r, k.y).
		CubeTo(k.x+k.w-r*.45, k.y, k.x+k.w, k.y+r*.45, k.x+k.w, k.y+r).
		LineTo(k.x+k.w, k.y+k.h-r).
		CubeTo(k.x+k.w, k.y+k.h-r*.45, k.x+k.w-r*.45, k.y+k.h, k.x+r, k.y+k.h).
		CubeTo(k.x+r*.45, k.y+k.h, k.x, k.y+k.h-r*.45, k.x, k.y+k.h-r).Close()
}

// remoteLook is what changes on the drawn remote.
type remoteLook struct {
	p         pal
	listening bool      // the voice key is held: filled red, with rings
	since     time.Time // since when
	muted     bool      // voice input cannot work: the voice key is not red
	still     bool      // reduce motion: no rings
	callout   bool      // name the voice key, on the left of the remote
	sel       string    // the key picked out on the "按键" page
}

// calloutW is the room the callout takes on the left of the remote, in the
// remote's units.
const calloutW = 180

// drawRemote draws the remote into r, 200 × 840 in its own units (with
// calloutW more on its left for the callout). It animates from the
// painter, so the view does not run again for it.
func drawRemote(pt *ui.Painter, r ui.Rect, look remoteLook) {
	p := look.p
	left := float32(0)
	if look.callout {
		left = calloutW
	}
	k := r.H / remoteH
	ox, oy := r.X+left*k, r.Y
	const lw = 2.2 // strokes, in the remote's units

	voice := p.zhu
	if look.muted {
		voice = p.ink3
	}
	pt.Transform(ui.Scaling(k, k).Then(ui.Translation(ox, oy)), 0, 0, func() {
		body := ui.Rect{X: 4, Y: 4, W: remoteW - 8, H: remoteH - 8}
		pt.Fill(body, p.card, 30)
		pt.Stroke(body, p.ink, 30, lw)
		if look.callout { // under the keys, which it passes behind
			pt.StrokePath((&ui.Path{}).MoveTo(voiceX-voiceR-4, voiceY).LineTo(-20, voiceY).Dash(0, 1, 7), 1.6, voice)
		}

		// Rings spreading from the voice key, a new one every 0.6 s.
		if look.listening && !look.still {
			pt.AnimationFrame()
			t := pt.Now().Sub(look.since).Seconds()
			for i := range 3 {
				d := t - float64(i)*.6
				if d < 0 {
					continue
				}
				ph := math.Mod(d, 1.8) / 1.8
				e := 1 - math.Pow(1-ph, 2.2) // fast, then slowing
				pt.StrokePath((&ui.Path{}).Circle(voiceX, voiceY, voiceR*float32(1+3*e)), 1.6, p.zhu.Alpha(float32(.8*(1-ph))))
			}
		}

		for _, key := range remoteKeys {
			path := keyPath(key)
			fill, stroke := p.paper, p.ink
			switch {
			case key.id == "voice" && look.listening:
				fill, stroke = p.zhu, p.zhu
			case key.id == "voice":
				stroke = voice
			}
			pt.FillPath(path, fill)
			if key.id == look.sel && !(key.id == "voice" && look.listening) {
				pt.FillPath(path, p.zhuSoft)
			}
			pt.StrokePath(path, lw, stroke)
		}
		pt.StrokePath(remoteGlyphs, lw, p.ink)

		mic := voice
		if look.listening {
			mic = p.paper
		}
		pt.FillPath(voiceCapsule, mic)
		pt.StrokePath(voiceStand, lw, mic)
	})
	if look.callout {
		name := ui.Span{Text: "语音键", Size: 13, Weight: 600, Color: voice}
		hint := ui.Span{Text: "按住说话", Size: 11.5, Color: p.ink2}
		nw, nh := pt.MeasureText(0, name)
		hw, _ := pt.MeasureText(0, hint)
		right, mid := ox-28*k, oy+voiceY*k
		pt.RichText(right-nw, mid-nh+1, 0, name)
		pt.RichText(right-hw, mid+3, 0, hint)
	}
}

// drawRemoteMark is the remote at a glance, for the sidebar's device line:
// its outline, the ring, and the voice key as a red dot.
func drawRemoteMark(pt *ui.Painter, r ui.Rect, ink, voice ui.Color) {
	pt.Stroke(ui.Rect{X: r.X + .6, Y: r.Y + .6, W: r.W - 1.2, H: r.H - 1.2}, ink, 3, 1.2)
	cx := r.X + r.W/2
	pt.StrokePath((&ui.Path{}).Circle(cx, r.Y+r.H*.27, r.W*.25), 1.2, ink)
	pt.FillPath((&ui.Path{}).Circle(cx+r.W*.2, r.Y+r.H*.1, 1), voice)
}
