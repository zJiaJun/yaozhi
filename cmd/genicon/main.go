// SPDX-License-Identifier: GPL-3.0-only

// Command genicon draws the app icon into resources/icon.png, which MyGo
// turns into the .app's icon. Run it with make icon after changing
// internal/icon.
package main

import (
	"flag"
	"image/png"
	"log"
	"os"

	"github.com/zJiaJun/yaozhi/internal/icon"
)

func main() {
	out := flag.String("o", "resources/icon.png", "output PNG")
	size := flag.Int("size", 1024, "width and height in pixels")
	flag.Parse()
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, icon.App(*size)); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}
