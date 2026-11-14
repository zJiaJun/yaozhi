// SPDX-License-Identifier: GPL-3.0-only

package macapp

import (
	"sync"
	"testing"
)

func TestKnownApps(t *testing.T) {
	// The Finder is on every Mac, and running in every login session; the
	// tests may run without one, so only its being installed is certain.
	if !Installed("com.apple.finder") {
		t.Error("the Finder is not installed?")
	}
	const nobody = "com.github.zjiajun.yaozhi.no-such-app"
	if Installed(nobody) || Running(nobody) {
		t.Error("an app that does not exist is installed or running")
	}
	if Installed("") || Running("") {
		t.Error("no identifier is no app")
	}
}

// TestManyCalls asks again and again from many goroutines at once, as the
// engine does every two seconds for as long as Yaozhi runs: each call has
// an autorelease pool of its own, which must be drained on the thread
// that made it.
func TestManyCalls(t *testing.T) {
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 2000 {
				if !Installed("com.apple.finder") {
					t.Error("the Finder is not installed?")
					return
				}
				Running("com.apple.finder")
			}
		})
	}
	wg.Wait()
}
