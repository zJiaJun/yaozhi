// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"bytes"
	"image/color"
	"image/png"

	"github.com/zJiaJun/yaozhi/internal/icon"
)

// trayIcon draws the menu bar icon, the app's mark at 18 pt and 2×. With tone
// "" it is a template (black, for macOS to tint); "warn" and "bad" add an
// orange or red badge, which a template would lose, so the mark is drawn in
// the menu bar's own color instead (white when dark).
func trayIcon(tone string, dark bool) []byte {
	ink := color.NRGBA{A: 255}
	if tone != "" && dark {
		ink = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	badge := map[string]color.NRGBA{"warn": icon.BadgeWarn, "bad": icon.BadgeError}[tone]
	var b bytes.Buffer
	_ = png.Encode(&b, icon.Glyph(36, ink, badge))
	return b.Bytes()
}
