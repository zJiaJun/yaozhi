// SPDX-License-Identifier: GPL-3.0-only

// Package macapp tells whether another app is installed and running, by its
// bundle identifier, as Yaozhi needs to know of the input method it hands
// the voice to. It asks AppKit through purego, without cgo.
package macapp

import (
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/zJiaJun/yaozhi/internal/cf"
)

var (
	once    sync.Once
	loadErr error

	clsWorkspace, clsRunning, clsPool objc.Class

	selShared     = objc.RegisterName("sharedWorkspace")
	selURLForApp  = objc.RegisterName("URLForApplicationWithBundleIdentifier:")
	selRunningIDs = objc.RegisterName("runningApplicationsWithBundleIdentifier:")
	selCount      = objc.RegisterName("count")
	selNew        = objc.RegisterName("new")
	selDrain      = objc.RegisterName("drain")
)

func load() error {
	once.Do(func() {
		if loadErr = cf.Load(); loadErr != nil {
			return
		}
		if _, loadErr = purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); loadErr != nil {
			return
		}
		clsWorkspace = objc.GetClass("NSWorkspace")
		clsRunning = objc.GetClass("NSRunningApplication")
		clsPool = objc.GetClass("NSAutoreleasePool")
	})
	return loadErr
}

// ask runs f with the bundle identifier as an NSString, inside an
// autorelease pool, so that what AppKit hands back does not pile up when
// this is called again and again.
//
// The goroutine stays on one thread meanwhile: autorelease pools are a
// stack per thread, and Go may otherwise carry on a goroutine on another
// thread between two calls, where draining the pool crashes (it did, a
// minute into a run on 2026-11-14).
func ask(bundleID string, f func(id objc.ID) bool) bool {
	if bundleID == "" || load() != nil {
		return false
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(clsPool).Send(selNew)
	defer pool.Send(selDrain)
	s := cf.String(bundleID) // a CFString is an NSString
	defer cf.Release(s)
	return f(objc.ID(s))
}

// Installed reports whether an app with the bundle identifier is on this
// Mac, wherever it is.
func Installed(bundleID string) bool {
	return ask(bundleID, func(id objc.ID) bool {
		return objc.ID(clsWorkspace).Send(selShared).Send(selURLForApp, id) != 0
	})
}

// Running reports whether an app with the bundle identifier is running.
func Running(bundleID string) bool {
	return ask(bundleID, func(id objc.ID) bool {
		apps := objc.ID(clsRunning).Send(selRunningIDs, id)
		return apps != 0 && apps.Send(selCount) != 0
	})
}
