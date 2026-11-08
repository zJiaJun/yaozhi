// SPDX-License-Identifier: GPL-3.0-only

package macinput

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/ebitengine/purego"

	"github.com/zJiaJun/yaozhi/internal/cf"
)

// EventType is a CGEventType this package watches.
type EventType int

const (
	EventKeyDown      EventType = 10
	EventKeyUp        EventType = 11
	EventFlagsChanged EventType = 12

	eventTapDisabledByTimeout   = 0xFFFFFFFE
	eventTapDisabledByUserInput = 0xFFFFFFFF
	fieldKeycode                = 9 // kCGKeyboardEventKeycode
	sessionEventTap             = 1 // kCGSessionEventTap
	headInsert                  = 0 // kCGHeadInsertEventTap
	listenOnly                  = 1 // kCGEventTapOptionListenOnly

	// KeyCodeF5 is kVK_F5.
	KeyCodeF5 = 96
	// KeyCodeFunction is kVK_Function.
	KeyCodeFunction = keyCodeFunction
	// FlagSecondaryFn is kCGEventFlagMaskSecondaryFn.
	FlagSecondaryFn = flagSecondaryFn
)

func (t EventType) String() string {
	switch t {
	case EventKeyDown:
		return "key_down"
	case EventKeyUp:
		return "key_up"
	case EventFlagsChanged:
		return "flags_changed"
	}
	return fmt.Sprintf("EventType(%d)", int(t))
}

// KeyEvent is one keyboard event seen by WatchKeys.
type KeyEvent struct {
	Type      EventType
	KeyCode   int
	Flags     uint64
	Synthetic bool // posted by this package (SyntheticMarker)
}

// ErrCannotListen reports that the process may not watch keyboard events:
// grant it in System Settings → Privacy & Security → Input Monitoring.
var ErrCannotListen = errors.New("macinput: not allowed to watch events (Input Monitoring permission)")

var (
	watchOnce sync.Once
	watchErr  error

	cgTapCreate       func(tap, place, options uint32, mask uint64, callback, user uintptr) cf.Ref
	cgTapEnable       func(port cf.Ref, enable bool)
	cgGetIntField     func(ev cf.Ref, field uint32) int64
	cgGetFlags        func(ev cf.Ref) uint64
	cgPreflightListen func() bool
	cgRequestListen   func() bool
	machPortSource    func(alloc uintptr, port cf.Ref, order int) cf.Ref
	runLoopCurrent    func() cf.Ref
	runLoopAddSource  func(rl, src, mode cf.Ref)
	runLoopRunInMode  func(mode cf.Ref, seconds float64, returnAfterSource bool) int32
	runLoopCommon     cf.Ref
	runLoopDefault    cf.Ref
	tapCallback       uintptr

	watchMu      sync.Mutex
	watchHandler func(KeyEvent)
	watchPort    cf.Ref
)

func loadWatch() error {
	watchOnce.Do(func() {
		if watchErr = loadCG(); watchErr != nil {
			return
		}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			watchErr = err
			return
		}
		purego.RegisterLibFunc(&cgTapCreate, cg, "CGEventTapCreate")
		purego.RegisterLibFunc(&cgTapEnable, cg, "CGEventTapEnable")
		purego.RegisterLibFunc(&cgGetIntField, cg, "CGEventGetIntegerValueField")
		purego.RegisterLibFunc(&cgGetFlags, cg, "CGEventGetFlags")
		purego.RegisterLibFunc(&cgPreflightListen, cg, "CGPreflightListenEventAccess")
		purego.RegisterLibFunc(&cgRequestListen, cg, "CGRequestListenEventAccess")
		purego.RegisterLibFunc(&machPortSource, cf.Lib, "CFMachPortCreateRunLoopSource")
		purego.RegisterLibFunc(&runLoopCurrent, cf.Lib, "CFRunLoopGetCurrent")
		purego.RegisterLibFunc(&runLoopAddSource, cf.Lib, "CFRunLoopAddSource")
		purego.RegisterLibFunc(&runLoopRunInMode, cf.Lib, "CFRunLoopRunInMode")
		for name, dst := range map[string]*cf.Ref{
			"kCFRunLoopCommonModes": &runLoopCommon,
			"kCFRunLoopDefaultMode": &runLoopDefault,
		} {
			sym, err := purego.Dlsym(cf.Lib, name)
			if err != nil {
				watchErr = err
				return
			}
			*dst = cf.GlobalString(sym)
		}
		// Runs on the thread running the run loop, inside WatchKeys.
		tapCallback = purego.NewCallback(func(proxy uintptr, typ uint32, ev cf.Ref, user uintptr) cf.Ref {
			watchMu.Lock()
			h, port := watchHandler, watchPort
			watchMu.Unlock()
			switch {
			case typ == eventTapDisabledByTimeout || typ == eventTapDisabledByUserInput:
				if port != 0 {
					cgTapEnable(port, true)
				}
			case h != nil:
				h(KeyEvent{
					Type:      EventType(typ),
					KeyCode:   int(cgGetIntField(ev, fieldKeycode)),
					Flags:     cgGetFlags(ev),
					Synthetic: cgGetIntField(ev, fieldSourceUserID) == SyntheticMarker,
				})
			}
			return ev
		})
	})
	return watchErr
}

// RequestListenEvents asks for the Input Monitoring permission and reports
// whether it is granted.
func RequestListenEvents() (bool, error) {
	if err := loadWatch(); err != nil {
		return false, err
	}
	if cgPreflightListen() {
		return true, nil
	}
	return cgRequestListen(), nil
}

// WatchKeys calls fn for every key down, key up and modifier change in the
// login session for d, without changing them. It blocks, running a run loop
// on a locked OS thread, and fn runs on that thread. One watch at a time.
func WatchKeys(d time.Duration, fn func(KeyEvent)) error {
	if err := loadWatch(); err != nil {
		return err
	}
	if !cgPreflightListen() {
		return ErrCannotListen
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	mask := uint64(1)<<EventKeyDown | uint64(1)<<EventKeyUp | uint64(1)<<EventFlagsChanged
	port := cgTapCreate(sessionEventTap, headInsert, listenOnly, mask, tapCallback, 0)
	if port == 0 {
		return ErrCannotListen
	}
	defer cf.Release(port)
	watchMu.Lock()
	if watchHandler != nil {
		watchMu.Unlock()
		return errors.New("macinput: already watching")
	}
	watchHandler, watchPort = fn, port
	watchMu.Unlock()
	defer func() {
		watchMu.Lock()
		watchHandler, watchPort = nil, 0
		watchMu.Unlock()
	}()

	src := machPortSource(0, port, 0)
	defer cf.Release(src)
	runLoopAddSource(runLoopCurrent(), src, runLoopCommon)
	cgTapEnable(port, true)
	defer cgTapEnable(port, false)

	deadline := time.Now().Add(d)
	for left := time.Until(deadline); left > 0; left = time.Until(deadline) {
		runLoopRunInMode(runLoopDefault, min(left, time.Second).Seconds(), false)
	}
	return nil
}
