// SPDX-License-Identifier: GPL-3.0-only

// Package icon draws Yaozhi's mark, "A1 墨指" in docs/design/icons.html: a
// fingertip at the lower left that narrows towards the upper right, and the
// target dot it points at. The app icon puts it on a macOS rounded-square
// plate; the menu bar glyph is the mark alone. Both are drawn from signed
// distances, so every size is sharp and anti-aliased without supersampling.
package icon

import (
	"image"
	"image/color"
	"math"
)

// Ink is the mark's color on the light plate, and Badge colors mark a menu
// bar state that needs attention.
var (
	Ink        = color.NRGBA{R: 0x11, G: 0x11, B: 0x14, A: 0xff}
	BadgeWarn  = color.NRGBA{R: 0xff, G: 0x9f, B: 0x0a, A: 0xff}
	BadgeError = color.NRGBA{R: 0xff, G: 0x45, B: 0x3a, A: 0xff}
)

// The app icon in the 1024-point macOS grid: an 824-point plate and the mark,
// as in docs/design/icons.html.
var (
	plateMin, plateMax, plateRadius = 100.0, 924.0, 185.0
	appDrop                         = roundCone{362, 662, 132, 654, 370, 26}
	appDot                          = disc{728, 296, 58}
)

// The menu bar glyph, with the attention badge at the lower right. The mark
// spans 2.5–14 by 2–13.5 units; the image shows a glyphView-unit square
// around glyphCenter, so the mark is about 15 of the menu bar's 18 points,
// like the system's own icons.
var (
	glyphDrop   = roundCone{5.4, 10.6, 2.9, 10.4, 5.6, .6}
	glyphDot    = disc{12.4, 3.6, 1.6}
	glyphBadge  = disc{12.7, 12.1, 2.2}
	badgeGap    = 1.0 // clear space around the badge, in units
	glyphView   = 13.8
	glyphCenter = [2]float64{8.4, 8.1}
)

// App draws the app icon, size pixels square (1024 for resources/icon.png).
func App(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	unit := 1024 / float64(size) // grid points per pixel
	for py := range size {
		for px := range size {
			x, y := (float64(px)+.5)*unit, (float64(py)+.5)*unit
			var c rgba

			// The drop shadow the macOS grid leaves room for below the plate.
			// It fades from 20 points inside the offset plate to 30 outside.
			if t := clamp((30 - plateDist(x, y-12)) / 50); t > 0 {
				c = c.layer(rgba{a: .24 * t * t})
			}

			pd := plateDist(x, y)
			if cov := coverage(pd, unit); cov > 0 {
				// A soft vertical gradient, white to cool grey, with a sheen
				// over the top 45%.
				t := clamp((y - plateMin) / (plateMax - plateMin))
				p := mix(rgba{1, 1, 1, 1}, rgba{0xe9 / 255., 0xeb / 255., 0xf2 / 255., 1}, t)
				if s := 1 - t/.45; s > 0 {
					p = p.layer(rgba{1, 1, 1, .9 * s})
				}
				// A thin edge, 4 points wide, straddling the outline.
				if e := coverage(math.Abs(pd)-2, unit); e > 0 {
					p = p.layer(rgba{a: .06 * e})
				}
				c = c.layer(p.fade(cov))
			}

			mark := math.Min(appDrop.dist(x, y), appDot.dist(x, y))
			if cov := coverage(mark, unit); cov > 0 {
				c = c.layer(fromNRGBA(Ink).fade(cov))
			}
			img.SetNRGBA(px, py, c.nrgba())
		}
	}
	return img
}

// Glyph draws the menu bar glyph, size pixels square (36 for 18 points at
// 2×), in ink. A badge color other
// than the zero value adds the attention badge at the lower right, with
// clear space cut out of the mark around it.
func Glyph(size int, ink, badge color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	unit := glyphView / float64(size)
	x0, y0 := glyphCenter[0]-glyphView/2, glyphCenter[1]-glyphView/2
	hasBadge := badge.A != 0
	for py := range size {
		for px := range size {
			x, y := x0+(float64(px)+.5)*unit, y0+(float64(py)+.5)*unit
			var c rgba
			mark := math.Min(glyphDrop.dist(x, y), glyphDot.dist(x, y))
			if hasBadge {
				mark = math.Max(mark, -(glyphBadge.dist(x, y) - badgeGap))
			}
			if cov := coverage(mark, unit); cov > 0 {
				c = c.layer(fromNRGBA(ink).fade(cov))
			}
			if hasBadge {
				if cov := coverage(glyphBadge.dist(x, y), unit); cov > 0 {
					c = c.layer(fromNRGBA(badge).fade(cov))
				}
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

type disc struct{ x, y, r float64 }

func (c disc) dist(x, y float64) float64 { return math.Hypot(x-c.x, y-c.y) - c.r }

// roundCone is two discs and the hull tangent to both: the teardrop, from a
// head (x1, y1, r1) to a tip (x2, y2, r2). The distance is Inigo Quilez's
// round cone.
type roundCone struct{ x1, y1, r1, x2, y2, r2 float64 }

func (k roundCone) dist(x, y float64) float64 {
	bax, bay := k.x2-k.x1, k.y2-k.y1
	l2 := bax*bax + bay*bay
	rr := k.r1 - k.r2
	a2 := l2 - rr*rr
	il2 := 1 / l2
	pax, pay := x-k.x1, y-k.y1
	yy := pax*bax + pay*bay
	z := yy - l2
	xvx, xvy := pax*l2-bax*yy, pay*l2-bay*yy
	x2 := xvx*xvx + xvy*xvy
	y2 := yy * yy * l2
	z2 := z * z * l2
	kk := math.Copysign(rr*rr*x2, rr)
	switch {
	case math.Copysign(a2*z2, z) > kk:
		return math.Sqrt(x2+z2)*il2 - k.r2
	case math.Copysign(a2*y2, yy) < kk:
		return math.Sqrt(x2+y2)*il2 - k.r1
	}
	return (math.Sqrt(x2*a2*il2)+yy*rr)*il2 - k.r1
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
