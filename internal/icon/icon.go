// SPDX-License-Identifier: GPL-3.0-only

// Package icon draws Yaozhi's mark, as docs/design/index.html has it: a
// remote held at a slant, which is also a finger pointing; the red dot on
// it is the voice key, and the arcs before its tip are the "遥" (far). The
// app icon puts it in paper on an ink plate; the menu bar glyph is the mark
// alone, with a hole for the voice key. Both are drawn from signed
// distances, so every size is sharp and anti-aliased without supersampling.
package icon

import (
	"image"
	"image/color"
	"math"
)

// The colors of the app icon, and of the badge that marks a menu bar state
// needing attention.
var (
	Ink       = color.NRGBA{R: 0x1c, G: 0x19, B: 0x16, A: 0xff}
	Paper     = color.NRGBA{R: 0xf4, G: 0xef, B: 0xe4, A: 0xff}
	Zhu       = color.NRGBA{R: 0xe2, G: 0x47, B: 0x2e, A: 0xff}
	BadgeWarn = color.NRGBA{R: 0xe0, G: 0x8a, B: 0x00, A: 0xff}

	plateTop, plateBottom = rgba{0x2e / 255., 0x2a / 255., 0x26 / 255., 1}, rgba{0x10 / 255., 0x0e / 255., 0x0d / 255., 1}
	bodyFrom, bodyTo      = rgba{0xfc / 255., 0xf8 / 255., 0xef / 255., 1}, rgba{0xd9 / 255., 0xd1 / 255., 0xbf / 255., 1}
)

// tilt is how far the remote leans to the right, in degrees.
const tilt = 40

// The app icon in the 1024-point macOS grid: an 824-point plate, and the
// remote in its own frame, which stands upright with its origin at appAt.
var (
	plateMin, plateMax, plateRadius = 100.0, 924.0, 186.0
	appAt                           = frame{470, 566, tilt}
	appBody                         = capsule{0, 187, 99} // 198 × 572
	appRing                         = disc{0, -34, 60}    // stroked 15 wide
	appDots                         = []disc{{0, 96, 13}, {0, 152, 13}}
	appVoice                        = disc{0, -186, 48}
	appNear                         = arc{-255.8, 140, 30, 15} // the arcs before the tip
	appFar                          = arc{-256.5, 220, 30, 15}
)

// The menu bar glyph in an 18-point box, which it nearly fills, as the
// system's own icons do.
var (
	glyphAt      = frame{7, 11.75, tilt}
	glyphBody    = capsule{-.6, 3.3, 3.7}
	glyphOutline = capsule{-.6, 3.3, 3.05} // paused: the outline alone, 1.3 wide
	glyphVoice   = disc{0, -3.6, 1.7}      // a hole
	glyphNear    = arc{-6.28, 4.4, 34.6, .65}
	glyphFar     = arc{-6.2, 6, 30, .65} // while listening
	glyphSlash   = [4]float64{3.5, 3.5, 14.5, 14.5}
	glyphBadge   = disc{13.6, 14, 2.5}
	badgeGap     = 1.0 // clear space around the badge
)

// GlyphState is what the menu bar glyph shows.
type GlyphState int

const (
	GlyphIdle   GlyphState = iota
	GlyphLive              // listening: a second arc
	GlyphPaused            // voice input is off: an outline, struck through
)

// App draws the app icon, size pixels square (1024 for resources/icon.png).
func App(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	unit := 1024 / float64(size) // grid points per pixel
	ink, paper, zhu := fromNRGBA(Ink), fromNRGBA(Paper), fromNRGBA(Zhu)
	for py := range size {
		for px := range size {
			x, y := (float64(px)+.5)*unit, (float64(py)+.5)*unit
			var c rgba

			// The drop shadow the macOS grid leaves room for below the plate.
			c = c.layer(rgba{a: .26 * blurred(plateDist(x, y-18)+6, 16)})

			pd := plateDist(x, y)
			if cov := coverage(pd, unit); cov > 0 {
				p := mix(plateTop, plateBottom, clamp((y-plateMin)/(plateMax-plateMin)))
				// A thin light edge, 4 points wide, just inside the outline.
				p = p.layer(rgba{1, 1, 1, .14 * coverage(math.Abs(pd+4)-2, unit)})

				lx, ly := appAt.local(x, y)
				// The remote's shadow on the plate, down and to the left.
				p = p.layer(rgba{a: .3 * blurred(appBody.dist(lx+10, ly-22), 16)})
				if cov := coverage(appBody.dist(lx, ly), unit); cov > 0 {
					t := ((lx+appBody.r)/(2*appBody.r) + (ly+appBody.half+appBody.r)/(2*(appBody.half+appBody.r))) / 2
					b := mix(bodyFrom, bodyTo, clamp(t))
					b = b.layer(ink.fade(.92 * coverage(math.Abs(appRing.dist(lx, ly))-7.5, unit)))
					for _, d := range appDots {
						b = b.layer(ink.fade(.85 * coverage(d.dist(lx, ly), unit)))
					}
					b = b.layer(zhu.fade(.5 * blurred(appVoice.dist(lx, ly)-8, 26))) // its glow
					b = b.layer(zhu.fade(coverage(appVoice.dist(lx, ly), unit)))
					p = p.layer(b.fade(cov))
				}
				p = p.layer(paper.fade(.45 * coverage(appFar.dist(lx, ly), unit)))
				p = p.layer(paper.fade(coverage(appNear.dist(lx, ly), unit)))
				c = c.layer(p.fade(cov))
			}
			img.SetNRGBA(px, py, c.nrgba())
		}
	}
	return img
}

// Glyph draws the menu bar glyph, size pixels square (36 for 18 points at
// 2×), in ink. A badge color other than the zero value adds the attention
// badge at the lower right, with clear space cut out of the mark around it.
func Glyph(size int, ink color.NRGBA, state GlyphState, badge color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	unit := 18 / float64(size)
	hasBadge := badge.A != 0
	for py := range size {
		for px := range size {
			x, y := (float64(px)+.5)*unit, (float64(py)+.5)*unit
			lx, ly := glyphAt.local(x, y)
			// The mark's parts, each a distance and how opaque it is.
			type part struct{ d, a float64 }
			var parts []part
			if state == GlyphPaused {
				parts = []part{
					{math.Abs(glyphOutline.dist(lx, ly)) - .65, .55},
					{segmentDist(x, y, glyphSlash) - .65, 1},
				}
			} else {
				parts = []part{
					{math.Max(glyphBody.dist(lx, ly), -glyphVoice.dist(lx, ly)), 1},
					{glyphNear.dist(lx, ly), 1},
				}
				if state == GlyphLive {
					parts = append(parts, part{glyphFar.dist(lx, ly), .6})
				}
			}
			var c rgba
			for _, p := range parts {
				if hasBadge {
					p.d = math.Max(p.d, -(glyphBadge.dist(x, y) - badgeGap))
				}
				if cov := coverage(p.d, unit) * p.a; cov > c.a {
					c = fromNRGBA(ink).fade(cov)
				}
			}
			if hasBadge {
				c = c.layer(fromNRGBA(badge).fade(coverage(glyphBadge.dist(x, y), unit)))
			}
			img.SetNRGBA(px, py, c.nrgba())
		}
	}
	return img
}

// plateDist is the signed distance to the plate: negative inside.
func plateDist(x, y float64) float64 {
	c := (plateMin + plateMax) / 2
	h := (plateMax-plateMin)/2 - plateRadius
	qx, qy := math.Abs(x-c)-h, math.Abs(y-c)-h
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - plateRadius
}

// coverage turns a signed distance, in design units, into the share of a
// pixel unit units wide that the shape covers.
func coverage(d, unit float64) float64 { return clamp(.5 - d/unit) }

// blurred is how much of a shape shows at signed distance d from its edge
// after a Gaussian blur of deviation sigma: the edge, spread out.
func blurred(d, sigma float64) float64 { return .5 * math.Erfc(d/(sigma*math.Sqrt2)) }

// frame is the remote's own coordinates: upright, its origin at (x, y) of
// the image, turned clockwise by deg degrees.
type frame struct{ x, y, deg float64 }

// local takes a point of the image into the frame.
func (f frame) local(x, y float64) (float64, float64) {
	s, c := math.Sincos(f.deg * math.Pi / 180)
	dx, dy := x-f.x, y-f.y
	return dx*c + dy*s, -dx*s + dy*c
}

type disc struct{ x, y, r float64 }

func (c disc) dist(x, y float64) float64 { return math.Hypot(x-c.x, y-c.y) - c.r }

// capsule is an upright pill around (0, y): a line half long each way from
// there, and everything within r of it.
type capsule struct{ y, half, r float64 }

func (k capsule) dist(x, y float64) float64 {
	return math.Hypot(x, math.Max(math.Abs(y-k.y)-k.half, 0)) - k.r
}

// arc is a stroke along a circle of radius r around (0, y), from span
// degrees left of straight up to span degrees right of it, half wide each
// side, with round ends.
type arc struct{ y, r, span, half float64 }

func (a arc) dist(x, y float64) float64 {
	qx, qy := x, y-a.y
	span := a.span * math.Pi / 180
	if math.Abs(math.Atan2(qx, -qy)) <= span {
		return math.Abs(math.Hypot(qx, qy)-a.r) - a.half
	}
	s, c := math.Sincos(span)
	return math.Hypot(math.Abs(qx)-a.r*s, qy+a.r*c) - a.half
}

// segmentDist is the distance from (x, y) to the segment s (x1, y1, x2, y2).
func segmentDist(x, y float64, s [4]float64) float64 {
	bx, by := s[2]-s[0], s[3]-s[1]
	t := clamp(((x-s[0])*bx + (y-s[1])*by) / (bx*bx + by*by))
	return math.Hypot(x-s[0]-t*bx, y-s[1]-t*by)
}

// rgba is a non-premultiplied color with components in [0, 1].
type rgba struct{ r, g, b, a float64 }

func fromNRGBA(c color.NRGBA) rgba {
	return rgba{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, float64(c.A) / 255}
}

func (c rgba) fade(t float64) rgba { c.a *= t; return c }

// layer composites src over c.
func (c rgba) layer(src rgba) rgba {
	a := src.a + c.a*(1-src.a)
	if a == 0 {
		return rgba{}
	}
	f := func(s, d float64) float64 { return (s*src.a + d*c.a*(1-src.a)) / a }
	return rgba{f(src.r, c.r), f(src.g, c.g), f(src.b, c.b), a}
}

func (c rgba) nrgba() color.NRGBA {
	b := func(v float64) uint8 { return uint8(math.Round(clamp(v) * 255)) }
	return color.NRGBA{b(c.r), b(c.g), b(c.b), b(c.a)}
}

func mix(a, b rgba, t float64) rgba {
	l := func(x, y float64) float64 { return x + (y-x)*t }
	return rgba{l(a.r, b.r), l(a.g, b.g), l(a.b, b.b), l(a.a, b.a)}
}

func clamp(v float64) float64 { return math.Max(0, math.Min(1, v)) }
