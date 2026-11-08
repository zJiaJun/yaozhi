// SPDX-License-Identifier: GPL-3.0-only

// Package cf is the small part of CoreFoundation the other native bindings
// share, through purego. A Ref follows CoreFoundation's ownership rules:
// values from Create/Copy functions belong to the caller, who calls Release.
package cf

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Ref is a CFTypeRef.
type Ref uintptr

const (
	encodingUTF8 = 0x08000100
	numberSInt64 = 4
)

var (
	loadOnce sync.Once
	loadErr  error

	// Lib is the CoreFoundation handle, for packages that look up their own
	// symbols in it.
	Lib uintptr

	cfRelease         func(Ref)
	cfRetain          func(Ref) Ref
	cfGetTypeID       func(Ref) uintptr
	stringCreate      func(alloc uintptr, s string, enc uint32) Ref
	stringGetCString  func(s Ref, buf *byte, size int, enc uint32) bool
	stringGetLength   func(s Ref) int
	stringTypeID      func() uintptr
	numberCreate      func(alloc uintptr, typ int, v *int64) Ref
	numberGetValue    func(n Ref, typ int, v *int64) bool
	numberTypeID      func() uintptr
	arrayCreate       func(alloc uintptr, values *Ref, n int, callbacks uintptr) Ref
	arrayGetCount     func(a Ref) int
	arrayGetValueAt   func(a Ref, i int) Ref
	arrayTypeID       func() uintptr
	dictCreate        func(alloc uintptr, keys, values *Ref, n int, keyCB, valueCB uintptr) Ref
	dictGetValue      func(d Ref, key Ref) Ref
	dictTypeID        func() uintptr
	arrayCallbacks    uintptr
	dictKeyCallbacks  uintptr
	dictValCallbacks  uintptr
	booleanTrueGlobal uintptr
)

// Load opens CoreFoundation. Every other function calls it implicitly except
// where noted; call it first to get the error.
func Load() error {
	loadOnce.Do(func() { loadErr = load() })
	return loadErr
}

func load() error {
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("cf: load CoreFoundation: %w", err)
	}
	Lib = lib
	purego.RegisterLibFunc(&cfRelease, lib, "CFRelease")
	purego.RegisterLibFunc(&cfRetain, lib, "CFRetain")
	purego.RegisterLibFunc(&cfGetTypeID, lib, "CFGetTypeID")
	purego.RegisterLibFunc(&stringCreate, lib, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&stringGetCString, lib, "CFStringGetCString")
	purego.RegisterLibFunc(&stringGetLength, lib, "CFStringGetLength")
	purego.RegisterLibFunc(&stringTypeID, lib, "CFStringGetTypeID")
	purego.RegisterLibFunc(&numberCreate, lib, "CFNumberCreate")
	purego.RegisterLibFunc(&numberGetValue, lib, "CFNumberGetValue")
	purego.RegisterLibFunc(&numberTypeID, lib, "CFNumberGetTypeID")
	purego.RegisterLibFunc(&arrayCreate, lib, "CFArrayCreate")
	purego.RegisterLibFunc(&arrayGetCount, lib, "CFArrayGetCount")
	purego.RegisterLibFunc(&arrayGetValueAt, lib, "CFArrayGetValueAtIndex")
	purego.RegisterLibFunc(&arrayTypeID, lib, "CFArrayGetTypeID")
	purego.RegisterLibFunc(&dictCreate, lib, "CFDictionaryCreate")
	purego.RegisterLibFunc(&dictGetValue, lib, "CFDictionaryGetValue")
	purego.RegisterLibFunc(&dictTypeID, lib, "CFDictionaryGetTypeID")
	for name, dst := range map[string]*uintptr{
		"kCFTypeArrayCallBacks":           &arrayCallbacks,
		"kCFTypeDictionaryKeyCallBacks":   &dictKeyCallbacks,
		"kCFTypeDictionaryValueCallBacks": &dictValCallbacks,
		"kCFBooleanTrue":                  &booleanTrueGlobal,
	} {
		if *dst, err = purego.Dlsym(lib, name); err != nil {
			return fmt.Errorf("cf: %w", err)
		}
	}
	return nil
}

func mustLoad() {
	if err := Load(); err != nil {
		panic(err)
	}
}

// Release releases r; 0 is ignored.
func Release(r Ref) {
	if r != 0 {
		cfRelease(r)
	}
}

// Retain retains r and returns it.
func Retain(r Ref) Ref {
	if r != 0 {
		cfRetain(r)
	}
	return r
}

// True returns kCFBooleanTrue (not owned).
func True() Ref {
	mustLoad()
	return GlobalString(booleanTrueGlobal)
}

// GlobalString reads a CF constant (such as a CFStringRef key) exported by
// a framework, given its symbol address from purego.Dlsym (not owned).
func GlobalString(symbol uintptr) Ref {
	// The symbol is the address of a C global, outside the Go heap.
	return *(*Ref)(unsafe.Add(unsafe.Pointer(nil), symbol))
}

// String creates a CFString (owned).
func String(s string) Ref {
	mustLoad()
	return stringCreate(0, s, encodingUTF8)
}

// GoString copies a CFString; 0 or another type gives "".
func GoString(s Ref) string {
	mustLoad()
	if s == 0 || cfGetTypeID(s) != stringTypeID() {
		return ""
	}
	// UTF-8 needs at most 4 bytes per UTF-16 unit, plus the terminator.
	buf := make([]byte, 4*stringGetLength(s)+1)
	if !stringGetCString(s, &buf[0], len(buf), encodingUTF8) {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// Number creates a CFNumber holding v (owned).
func Number(v int64) Ref {
	mustLoad()
	return numberCreate(0, numberSInt64, &v)
}

// Int64 reads a CFNumber; ok is false for 0 or another type.
func Int64(n Ref) (v int64, ok bool) {
	mustLoad()
	if n == 0 || cfGetTypeID(n) != numberTypeID() {
		return 0, false
	}
	return v, numberGetValue(n, numberSInt64, &v)
}

// Array creates a CFArray of values, which it retains (owned).
func Array(values []Ref) Ref {
	mustLoad()
	var p *Ref
	if len(values) > 0 {
		p = &values[0]
	}
	return arrayCreate(0, p, len(values), arrayCallbacks)
}

// Items lists a CFArray (not owned); 0 or another type gives nil.
func Items(a Ref) []Ref {
	mustLoad()
	if a == 0 || cfGetTypeID(a) != arrayTypeID() {
		return nil
	}
	n := arrayGetCount(a)
	out := make([]Ref, n)
	for i := range out {
		out[i] = arrayGetValueAt(a, i)
	}
	return out
}

// Dictionary creates a CFDictionary from parallel keys and values, which it
// retains (owned).
func Dictionary(keys, values []Ref) Ref {
	mustLoad()
	if len(keys) != len(values) {
		panic("cf: Dictionary: keys and values differ in length")
	}
	var k, v *Ref
	if len(keys) > 0 {
		k, v = &keys[0], &values[0]
	}
	return dictCreate(0, k, v, len(keys), dictKeyCallbacks, dictValCallbacks)
}

// Get looks up key (a Go string) in a CFDictionary (result not owned); 0 or
// another type gives 0.
func Get(d Ref, key string) Ref {
	mustLoad()
	if d == 0 || cfGetTypeID(d) != dictTypeID() {
		return 0
	}
	k := String(key)
	defer Release(k)
	return dictGetValue(d, k)
}
