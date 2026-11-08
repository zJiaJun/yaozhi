// SPDX-License-Identifier: GPL-3.0-only

package coreaudio

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

// Watching the device list, so that callers need not list the devices over
// and over to notice one coming or going.

var (
	addListener    func(obj uint32, addr *propertyAddress, fn uintptr, client uintptr) int32
	removeListener func(obj uint32, addr *propertyAddress, fn uintptr, client uintptr) int32

	listenerOnce sync.Once
	listenerFn   uintptr
	watchers     sync.Map // uintptr id -> func()
	nextWatcher  atomic.Uintptr
)

func loadListener() {
	listenerOnce.Do(func() {
		ca, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&addListener, ca, "AudioObjectAddPropertyListener")
		purego.RegisterLibFunc(&removeListener, ca, "AudioObjectRemovePropertyListener")
		// AudioObjectPropertyListenerProc, called on a Core Audio thread.
		listenerFn = purego.NewCallback(func(obj, n uint32, addrs, client uintptr) int32 {
			if fn, ok := watchers.Load(client); ok {
				fn.(func())()
			}
			return 0
		})
	})
}

// WatchDevices calls fn, on a thread of Core Audio's, whenever devices are
// added or removed (including aggregates being made or destroyed). fn must
// not block. The returned function stops watching.
func WatchDevices(fn func()) (stop func(), err error) {
	if err := load(); err != nil {
		return nil, err
	}
	loadListener()
	if listenerFn == 0 {
		return nil, fmt.Errorf("coreaudio: property listeners unavailable")
	}
	id := nextWatcher.Add(1)
	watchers.Store(id, fn)
	addr := propertyAddress{selDevices, scopeGlobal, elementMain}
	if st := addListener(systemObject, &addr, listenerFn, id); st != 0 {
		watchers.Delete(id)
		return nil, fmt.Errorf("coreaudio: watch devices: OSStatus %d", st)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			removeListener(systemObject, &addr, listenerFn, id)
			watchers.Delete(id)
		})
	}, nil
}
