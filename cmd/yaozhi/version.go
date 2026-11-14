// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package main

import (
	"runtime/debug"

	"github.com/egoist/mygo"
)

// appVersion is the version of the packaged app (mygo.json's "version"),
// "" when not running from a package, as in tests.
func appVersion() string { return mygo.App.Version() }

// appBuild tells which source the program was built from: the first seven
// digits of the Git commit, with a "+" when files were changed since. Go
// records both in the program; "" when it did not (built outside the
// repository).
func appBuild() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, changed string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				changed = "+"
			}
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev == "" {
		return ""
	}
	return rev + changed
}

// versionLine is the version and the build in a line, for the log and the
// diagnostics: "0.0.1（a1b2c3d）".
func versionLine() string {
	v, b := appVersion(), appBuild()
	if v == "" {
		v = "开发版"
	}
	if b != "" {
		v += "（" + b + "）"
	}
	return v
}

// showAbout shows the system's about panel: the icon, the name, the
// version and the build, and the license.
func showAbout() {
	mygo.App.ShowAboutPanel(mygo.AboutPanelOptions{
		ApplicationName:    "遥指",
		ApplicationVersion: appVersion(),
		Version:            appBuild(),
		Copyright:          "© 2026 zhujiajun · GPL-3.0-only",
		Credits:            "用遥控器控制 Mac",
	})
}
