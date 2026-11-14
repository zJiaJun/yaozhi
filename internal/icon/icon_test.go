// SPDX-License-Identifier: GPL-3.0-only

package icon

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestShapes(t *testing.T) {
	k := capsule{-10, 100, 20}
	for _, c := range []struct{ x, y, want float64 }{
		{0, -10, -20},  // its middle
		{0, 90, -20},   // on the line
		{30, 0, 10},    // beside it
		{0, 125, 15},   // past its lower end
		{0, -130, 0},   // on its upper end
		{12, 106, 0},   // on the lower cap: 20 from its center
		{-20, -50, 0},  // on its side
		{20, 110, 8.3}, // off the corner of its box
	} {
		if got := k.dist(c.x, c.y); math.Abs(got-c.want) > .5 {
			t.Errorf("capsule.dist(%v, %v) = %.2f, want %.2f", c.x, c.y, got, c.want)
		}
	}

	a := arc{0, 100, 30, 5}
	for _, c := range []struct{ x, y, want float64 }{
		{0, -100, -5},   // on the arc, straight up
		{0, -110, 5},    // outside it
		{0, -92, 3},     // inside it
		{50, -86.6, -5}, // its right end
		{-50, -86.6, -5},
		{60, -86.6, 5}, // past the end: round
		{100, 0, 95},   // far round the circle, measured from the end
	} {
		if got := a.dist(c.x, c.y); math.Abs(got-c.want) > .5 {
			t.Errorf("arc.dist(%v, %v) = %.2f, want %.2f", c.x, c.y, got, c.want)
		}
	}

	// A frame turned a quarter to the right: its "up" is the image's right.
	if x, y := (frame{10, 10, 90}).local(15, 10); math.Abs(x) > 1e-9 || math.Abs(y+5) > 1e-9 {
		t.Errorf("frame.local = (%v, %v), want (0, -5)", x, y)
	}
}

// global is where a point of a frame is in the image.
func global(f frame, x, y float64) (float64, float64) {
	s, c := math.Sincos(f.deg * math.Pi / 180)
	return f.x + x*c - y*s, f.y + x*s + y*c
}

func TestApp(t *testing.T) {
	img := App(256)
	at := func(x, y float64) color.NRGBA { return img.NRGBAAt(int(x/4), int(y/4)) }
	in := func(x, y float64) color.NRGBA { return at(global(appAt, x, y)) }
	if c := at(2, 2); c.A != 0 {
		t.Errorf("corner outside the plate is %v, want transparent", c)
	}
	if c := at(512, 880); c.A != 255 || c.R > 0x30 {
		t.Errorf("plate is %v, want ink", c)
	}
	if c := in(appVoice.x, appVoice.y); c != Zhu {
		t.Errorf("voice key is %v, want %v", c, Zhu)
	}
	if c := in(60, 230); c.A != 255 || c.R < 0xd0 || c.G < 0xc8 {
		t.Errorf("remote is %v, want paper", c)
	}
	if c := in(0, appNear.y-appNear.r); c != Paper {
		t.Errorf("near arc is %v, want %v", c, Paper)
	}
	// Between the tip and the arc is the plate.
	if c := in(0, -330); c.A != 255 || c.R > 0x40 {
		t.Errorf("gap before the tip is %v, want the plate", c)
	}
}

func TestGlyph(t *testing.T) {
	const size = 36
	black := color.NRGBA{A: 255}
	at := func(img *image.NRGBA, x, y float64) color.NRGBA { return img.NRGBAAt(int(x*size/18), int(y*size/18)) }
	in := func(img *image.NRGBA, x, y float64) color.NRGBA {
		gx, gy := global(glyphAt, x, y)
		return at(img, gx, gy)
	}
	farTop := glyphFar.y - glyphFar.r

	idle := Glyph(size, black, GlyphIdle, color.NRGBA{})
	if c := in(idle, 0, 3); c != black {
		t.Errorf("remote is %v", c)
	}
	if c := in(idle, glyphVoice.x, glyphVoice.y); c.A != 0 {
		t.Errorf("voice key is %v, want a hole", c)
	}
	if c := in(idle, 0, glyphNear.y-glyphNear.r); c.A < 200 {
		t.Errorf("near arc is %v", c)
	}
	if c := in(idle, 0, farTop); c.A != 0 {
		t.Errorf("far arc shows while idle: %v", c)
	}
	if c := at(idle, glyphBadge.x, glyphBadge.y); c.A != 0 && c != black {
		t.Errorf("badge spot without a badge is %v", c)
	}

	live := Glyph(size, black, GlyphLive, color.NRGBA{})
	if c := in(live, 0, farTop); c.A < 100 {
		t.Errorf("far arc is %v while listening", c)
	}

	paused := Glyph(size, black, GlyphPaused, color.NRGBA{})
	if c := in(paused, 0, 3); c.A != 0 {
		t.Errorf("paused remote is filled: %v", c)
	}
	if c := at(paused, 13.5, 13.5); c != black {
		t.Errorf("slash is %v", c)
	}

	badged := Glyph(size, black, GlyphIdle, BadgeWarn)
	if c := at(badged, glyphBadge.x, glyphBadge.y); c != BadgeWarn {
		t.Errorf("badge is %v", c)
	}
	// Nothing is cut off at the edges.
	for n, img := range []*image.NRGBA{idle, live, paused, badged} {
		for i := range size {
			for _, c := range []color.NRGBA{img.NRGBAAt(i, 0), img.NRGBAAt(i, size-1), img.NRGBAAt(0, i), img.NRGBAAt(size-1, i)} {
				if c.A > 60 {
					t.Fatalf("glyph %d touches the edge at %d: %v", n, i, c)
				}
			}
		}
	}
}
