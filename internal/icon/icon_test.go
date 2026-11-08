// SPDX-License-Identifier: GPL-3.0-only

package icon

import (
	"image/color"
	"math"
	"testing"
)

func TestRoundConeMatchesItsDiscs(t *testing.T) {
	k := appDrop
	for _, c := range []struct {
		x, y, want float64
	}{
		{k.x1, k.y1, -k.r1},            // head center
		{k.x2, k.y2, -k.r2},            // tip center
		{k.x1 - k.r1 - 10, k.y1, 10},   // left of the head
		{k.x2 + k.r2 + 5, k.y2 - 0, 5}, // right of the tip, past the hull
		{k.x1, k.y1 + k.r1 + 7, 7},     // below the head
	} {
		if got := k.dist(c.x, c.y); math.Abs(got-c.want) > .5 {
			t.Errorf("dist(%v, %v) = %.2f, want %.2f", c.x, c.y, got, c.want)
		}
	}
}

func TestApp(t *testing.T) {
	img := App(256)
	at := func(x, y float64) color.NRGBA { return img.NRGBAAt(int(x/4), int(y/4)) }
	if c := at(2, 2); c.A != 0 {
		t.Errorf("corner outside the plate is %v, want transparent", c)
	}
	if c := at(362, 662); c != Ink {
		t.Errorf("fingertip is %v, want ink", c)
	}
	if c := at(728, 296); c != Ink {
		t.Errorf("target dot is %v, want ink", c)
	}
	// The gap between the tip and the dot is the plate, nearly white.
	if c := at(680, 344); c.A != 255 || c.R < 0xe0 {
		t.Errorf("gap is %v, want the plate", c)
	}
}

func TestGlyph(t *testing.T) {
	const size = 36
	at := func(img interface{ NRGBAAt(int, int) color.NRGBA }, x, y float64) color.NRGBA {
		u := glyphView / size
		return img.NRGBAAt(int((x-glyphCenter[0]+glyphView/2)/u), int((y-glyphCenter[1]+glyphView/2)/u))
	}
	plain := Glyph(size, Ink, color.NRGBA{})
	if c := at(plain, glyphDrop.x1, glyphDrop.y1); c != Ink {
		t.Errorf("fingertip is %v", c)
	}
	if c := at(plain, glyphDot.x, glyphDot.y); c != Ink {
		t.Errorf("target dot is %v", c)
	}
	if c := at(plain, glyphBadge.x, glyphBadge.y); c.A != 0 {
		t.Errorf("badge spot without a badge is %v", c)
	}
	badged := Glyph(size, Ink, BadgeWarn)
	if c := at(badged, glyphBadge.x, glyphBadge.y); c != BadgeWarn {
		t.Errorf("badge is %v", c)
	}
	// Nothing is cut off at the edges.
	for _, img := range []interface{ NRGBAAt(int, int) color.NRGBA }{plain, badged} {
		for i := range size {
			for _, c := range []color.NRGBA{img.NRGBAAt(i, 0), img.NRGBAAt(i, size-1), img.NRGBAAt(0, i), img.NRGBAAt(size-1, i)} {
				if c.A > 40 {
					t.Fatalf("glyph touches the edge: %v", c)
				}
			}
		}
	}
}
