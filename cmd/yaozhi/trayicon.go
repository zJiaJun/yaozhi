// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"bytes"
	"image/color"
	"image/png"

	"github.com/zJiaJun/yaozhi/internal/icon"
)

// trayIcon draws the menu bar icon, the app's mark at 18 pt and 2×, as
// state has it. With badge "" it is a template (black, for macOS to tint);
// any other adds the orange badge, which a template would lose, so the
// mark is drawn in the menu bar's own color instead (white when dark).
func trayIcon(state icon.GlyphState, badge string, dark bool) []byte {
	ink := color.NRGBA{A: 255}
	var b color.NRGBA
	if badge != "" {
		b = icon.BadgeWarn
		if dark {
			ink = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, icon.Glyph(36, ink, state, b))
	return buf.Bytes()
}
