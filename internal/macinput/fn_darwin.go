// SPDX-License-Identifier: GPL-3.0-only

package macinput

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ebitengine/purego"

	"github.com/zJiaJun/yaozhi/internal/cf"
)

const (
	keyCodeFunction   = 63       // kVK_Function
	flagSecondaryFn   = 0x800000 // kCGEventFlagMaskSecondaryFn
	hidSystemState    = 1        // kCGEventSourceStateHIDSystemState
	hidEventTap       = 0        // kCGHIDEventTap
	fieldSourceUserID = 42       // kCGEventSourceUserData

	// SyntheticMarker is stamped on every event this package posts, so an
	// event tap can tell them from real key presses.
	SyntheticMarker = 0x59415A48 // "YAZH"

	// DefaultTapDuration is how long a simulated tap holds Fn down, as
	// remote-mic-app's VoiceFnTapSessionController does.
	DefaultTapDuration = 120 * time.Millisecond
)

// ErrNotTrusted reports that the process may not post keyboard events:
// grant it in System Settings → Privacy & Security → Accessibility.
var ErrNotTrusted = errors.New("macinput: not allowed to post events (Accessibility permission)")

var (
	cgOnce sync.Once
	cgErr  error

	cgSourceCreate    func(state int32) cf.Ref
	cgKeyboardEvent   func(source cf.Ref, key uint16, down bool) cf.Ref
	cgSetFlags        func(ev cf.Ref, flags uint64)
	cgSetIntField     func(ev cf.Ref, field uint32, v int64)
	cgPost            func(tap uint32, ev cf.Ref)
	cgPreflightPost   func() bool
	cgRequestPost     func() bool
	axIsTrusted       func() bool
	fnStateMu         sync.Mutex
	fnPressedByUs     bool
	errEventCreateNil = errors.New("macinput: CGEventCreateKeyboardEvent failed")
)

func loadCG() error {
	cgOnce.Do(func() {
		if cgErr = cf.Load(); cgErr != nil {
			return
		}
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			cgErr = fmt.Errorf("macinput: load CoreGraphics: %w", err)
			return
		}
		purego.RegisterLibFunc(&cgSourceCreate, lib, "CGEventSourceCreate")
		purego.RegisterLibFunc(&cgKeyboardEvent, lib, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgSetFlags, lib, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgSetIntField, lib, "CGEventSetIntegerValueField")
		purego.RegisterLibFunc(&cgPost, lib, "CGEventPost")
		purego.RegisterLibFunc(&cgPreflightPost, lib, "CGPreflightPostEventAccess")
		purego.RegisterLibFunc(&cgRequestPost, lib, "CGRequestPostEventAccess")

		as, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			cgErr = fmt.Errorf("macinput: load ApplicationServices: %w", err)
			return
		}
		purego.RegisterLibFunc(&axIsTrusted, as, "AXIsProcessTrusted")
	})
	return cgErr
}

// CanPostEvents reports whether this process may post keyboard events.
//
// CGPreflightPostEventAccess keeps answering false for the rest of the
// process after the user grants Accessibility; AXIsProcessTrusted sees the
// grant at once, so polling this function works without a restart.
func CanPostEvents() (bool, error) {
	if err := loadCG(); err != nil {
		return false, err
	}
	return cgPreflightPost() || axIsTrusted(), nil
}

// RequestPostEvents asks macOS for the permission to post events. The first
// time it shows the system prompt that leads to the Accessibility list; it
// returns whether the permission is already granted.
func RequestPostEvents() (bool, error) {
	if err := loadCG(); err != nil {
		return false, err
	}
	return cgRequestPost(), nil
}

// SetFn posts Fn going down or up, as the built-in keyboard's Fn/Globe key
// does. Without the permission macOS drops the event silently, so it checks
// first.
func SetFn(down bool) error {
	if err := loadCG(); err != nil {
		return err
	}
	if !cgPreflightPost() && !axIsTrusted() {
		return ErrNotTrusted
	}
	src := cgSourceCreate(hidSystemState)
	defer cf.Release(src)
	ev := cgKeyboardEvent(src, keyCodeFunction, down)
	if ev == 0 {
		return errEventCreateNil
	}
	defer cf.Release(ev)
	var flags uint64
	if down {
		flags = flagSecondaryFn
	}
	cgSetFlags(ev, flags)
	cgSetIntField(ev, fieldSourceUserID, SyntheticMarker)
	cgPost(hidEventTap, ev)
	fnStateMu.Lock()
	fnPressedByUs = down
	fnStateMu.Unlock()
	return nil
}

// TapFn presses and releases Fn, holding it for hold. It blocks for hold.
func TapFn(hold time.Duration) error {
	if err := SetFn(true); err != nil {
		return err
	}
	time.Sleep(hold)
	return SetFn(false)
}

// ReleaseFnIfPressed lifts Fn if this package left it down, as after a crash
// between the two halves of a tap. Call it on shutdown.
func ReleaseFnIfPressed() error {
	fnStateMu.Lock()
	pressed := fnPressedByUs
	fnStateMu.Unlock()
	if !pressed {
		return nil
	}
	return SetFn(false)
}
