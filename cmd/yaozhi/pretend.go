// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"strings"

	"github.com/zJiaJun/yaozhi/internal/app"
	"github.com/zJiaJun/yaozhi/internal/device"
)

// Pretending is for looking at the first launch on a Mac that has
// everything: with -pretend, the window shows chosen things as missing,
// whatever the engine found. Only what is shown changes: the engine works
// on underneath, and so does voice input once nothing is pretended away.
//
// A step's button does what it always does, and also ends the pretending
// of that step, so the whole of the first launch can be clicked through.

// pretendAll is what "-pretend all" stands for: a Mac with nothing set up.
const pretendAll = "bluetooth,remote,blackhole,typeless,ax,text"

// parsePretend reads the flag: names separated by commas, or "all".
//
//	bluetooth     the user was never asked about Bluetooth
//	remote        no remote was ever connected
//	blackhole     BlackHole 2ch is not installed
//	typeless      the input method's app is not installed
//	typeless-off  it is installed, and not running
//	ax            no Accessibility permission
//	text          no text was ever seen to arrive
func parsePretend(flag string) map[string]bool {
	if flag == "all" {
		flag = pretendAll
	}
	m := map[string]bool{}
	for _, name := range strings.Split(flag, ",") {
		if name = strings.TrimSpace(name); name != "" {
			m[name] = true
		}
	}
	return m
}

// pretended returns st with what is pretended away taken out of it.
func (s *shell) pretended(st app.State) app.State {
	for name := range s.pretend {
		switch name {
		case "bluetooth":
			st.NeedsStart, st.Ready.Bluetooth = true, app.PermUnknown
		case "remote":
			st.Conn, st.RemoteSeen, st.Device, st.BatteryKnown = device.StateSearching, false, "", false
		case "blackhole":
			st.Ready.BlackHole, st.Ready.Mic = false, false
		case "typeless":
			st.Ready.InputInstalled, st.Ready.InputRunning = false, false
		case "typeless-off":
			st.Ready.InputRunning = false
		case "ax":
			st.Ready.Accessibility, st.Ready.AXRevoked = false, false
		case "text":
			st.TextSeen = false
		}
	}
	return st
}

// unpretend ends the pretending of one thing, and shows the state without
// it. It does nothing when that was not pretended.
func (s *shell) unpretend(name string) {
	if !s.pretend[name] {
		return
	}
	delete(s.pretend, name)
	logf("不再假装缺少 %s", name)
	s.setState(s.eng.State())
}
